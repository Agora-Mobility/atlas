package appypay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
)

const (
	asyncAccept       = "application/vnd.appypay.asyncapi+json"
	jsonType          = "application/json"
	language          = "pt-BR"
	tokenRenewMargin  = time.Minute
	maxResponseBytes  = 1 << 20
	centsPerUnit      = 100
	statusRequested   = "Requested"
	statusPending     = "Pending"
	statusSuccess     = "Success"
	statusFailed      = "Failed"
	defaultTokenLife  = time.Hour
	lookupResultLimit = "1"
)

type Settings struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	TokenURL     string
	Resource     string
	GPOMethod    string
	REFMethod    string
	Timeout      time.Duration
}

type Client struct {
	settings Settings
	http     *http.Client
	now      func() time.Time

	mu        sync.Mutex
	token     string
	tokenEnds time.Time
}

var _ services.IPaymentGateway = (*Client)(nil)

func New(settings Settings) *Client {
	settings.BaseURL = strings.TrimRight(settings.BaseURL, "/")
	return &Client{settings: settings, http: tracing.HTTPClient(settings.Timeout), now: time.Now}
}

func (c *Client) Enabled() bool { return true }

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   string `json:"expires_in"`
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.tokenEnds) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"client_credentials"}, "client_id": {c.settings.ClientID},
		"client_secret": {c.settings.ClientSecret}, "resource": {c.settings.Resource},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.settings.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("appypay token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("appypay token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("appypay token: status %d", res.StatusCode)
	}
	var body tokenResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&body); err != nil {
		return "", fmt.Errorf("appypay token body: %w", err)
	}
	if body.AccessToken == "" {
		return "", errors.New("appypay token: empty token")
	}
	life := defaultTokenLife
	if seconds, err := strconv.Atoi(body.ExpiresIn); err == nil && seconds > 0 {
		life = time.Duration(seconds) * time.Second
	}
	c.token, c.tokenEnds = body.AccessToken, c.now().Add(life-tokenRenewMargin)
	return c.token, nil
}

func (c *Client) forgetToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
}

func (c *Client) send(ctx context.Context, method, path, accept string, payload any) (int, []byte, error) {
	status, body, err := c.sendOnce(ctx, method, path, accept, payload)
	if err == nil && status == http.StatusUnauthorized {
		c.forgetToken()
		return c.sendOnce(ctx, method, path, accept, payload)
	}
	return status, body, err
}

func (c *Client) sendOnce(ctx context.Context, method, path, accept string, payload any) (int, []byte, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("appypay encode: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.settings.BaseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("appypay request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", language)
	if payload != nil {
		req.Header.Set("Content-Type", jsonType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("appypay %s: %w", method, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return res.StatusCode, nil, fmt.Errorf("appypay read: %w", err)
	}
	return res.StatusCode, body, nil
}

func formatAmount(cents int64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", cents/centsPerUnit, cents%centsPerUnit))
}

func isSuccess(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func toCents(amount float64) int64 {
	return int64(math.Round(amount * centsPerUnit))
}

type referencePayload struct {
	ReferenceNumber string `json:"referenceNumber"`
	Entity          string `json:"entity"`
	DueDate         string `json:"dueDate"`
}

type responseStatus struct {
	Successful bool              `json:"successful"`
	Status     string            `json:"status"`
	Code       int               `json:"code"`
	Message    string            `json:"message"`
	Reference  *referencePayload `json:"reference"`
}

type chargeResponse struct {
	ID             string         `json:"id"`
	ResponseStatus responseStatus `json:"responseStatus"`
}

type paymentPayload struct {
	ID                    string            `json:"id"`
	MerchantTransactionID string            `json:"merchantTransactionId"`
	Amount                float64           `json:"amount"`
	Status                string            `json:"status"`
	Reference             *referencePayload `json:"reference"`
}

type chargeRequest struct {
	Amount                json.Number    `json:"amount"`
	Currency              string         `json:"currency"`
	Description           string         `json:"description"`
	MerchantTransactionID string         `json:"merchantTransactionId"`
	PaymentMethod         string         `json:"paymentMethod"`
	PaymentInfo           map[string]any `json:"paymentInfo,omitempty"`
}

func toStatus(raw string) services.ChargeStatus {
	switch raw {
	case statusSuccess:
		return services.ChargeSuccess
	case statusFailed:
		return services.ChargeFailed
	default:
		return services.ChargePending
	}
}

func parseDue(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

func applyReference(charge *services.Charge, reference *referencePayload) {
	if reference == nil {
		return
	}
	charge.ReferenceNumber, charge.ReferenceEntity, charge.DueAt = reference.ReferenceNumber, reference.Entity, parseDue(reference.DueDate)
}

func declined(status int, body []byte) error {
	var parsed chargeResponse
	message, code := "", strconv.Itoa(status)
	if err := json.Unmarshal(body, &parsed); err == nil {
		message = parsed.ResponseStatus.Message
		if parsed.ResponseStatus.Code != 0 {
			code = strconv.Itoa(parsed.ResponseStatus.Code)
		}
	}
	return &services.PaymentDeclinedError{Code: code, Message: message}
}

func (c *Client) methodKey(method entities.PaymentMethod) (string, map[string]any, error) {
	switch method {
	case entities.PaymentGPO:
		return c.settings.GPOMethod, nil, nil
	case entities.PaymentReference:
		return c.settings.REFMethod, nil, nil
	case entities.PaymentCash:
	}
	return "", nil, fmt.Errorf("appypay: method %q has no gateway key", method)
}

func (c *Client) CreateCharge(ctx context.Context, in services.ChargeInput) (*services.Charge, error) {
	key, info, err := c.methodKey(in.Method)
	if err != nil {
		return nil, err
	}
	if in.Method == entities.PaymentGPO {
		info = map[string]any{"phoneNumber": in.PhoneNumber}
	}
	status, body, err := c.send(ctx, http.MethodPost, "/charges", asyncAccept, chargeRequest{
		Amount: formatAmount(in.AmountCents), Currency: in.Currency, Description: in.Description, MerchantTransactionID: in.MerchantID, PaymentMethod: key, PaymentInfo: info,
	})
	if err != nil {
		return nil, err
	}
	switch {
	case isSuccess(status):
		var parsed chargeResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("appypay charge body: %w", err)
		}
		charge := &services.Charge{ID: strings.ToLower(parsed.ID), MerchantID: in.MerchantID, AmountCents: in.AmountCents, Status: toStatus(parsed.ResponseStatus.Status), Message: parsed.ResponseStatus.Message}
		applyReference(charge, parsed.ResponseStatus.Reference)
		return charge, nil
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError && status != http.StatusUnauthorized && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests:
		return nil, declined(status, body)
	default:
		return nil, fmt.Errorf("appypay charge: status %d", status)
	}
}

func toCharge(payment *paymentPayload) *services.Charge {
	charge := &services.Charge{
		ID: strings.ToLower(payment.ID), MerchantID: payment.MerchantTransactionID, Status: toStatus(payment.Status), AmountCents: toCents(payment.Amount),
	}
	applyReference(charge, payment.Reference)
	return charge
}

func (c *Client) GetCharge(ctx context.Context, chargeID string) (*services.Charge, error) {
	status, body, err := c.send(ctx, http.MethodGet, "/charges/"+url.PathEscape(chargeID), jsonType, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, services.ErrChargeNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("appypay get charge: status %d", status)
	}
	var parsed struct {
		Payment paymentPayload `json:"payment"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("appypay get charge body: %w", err)
	}
	if parsed.Payment.ID == "" {
		return nil, services.ErrChargeNotFound
	}
	return toCharge(&parsed.Payment), nil
}

func (c *Client) FindChargeByMerchantID(ctx context.Context, merchantID string) (*services.Charge, error) {
	query := url.Values{"merchantTransactionId": {merchantID}, "limit": {lookupResultLimit}}
	status, body, err := c.send(ctx, http.MethodGet, "/charges?"+query.Encode(), jsonType, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("appypay find charge: status %d", status)
	}
	var parsed struct {
		Payments []paymentPayload `json:"payments"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("appypay find charge body: %w", err)
	}
	for i := range parsed.Payments {
		if parsed.Payments[i].MerchantTransactionID == merchantID {
			return toCharge(&parsed.Payments[i]), nil
		}
	}
	return nil, services.ErrChargeNotFound
}

func (c *Client) Refund(ctx context.Context, chargeID string, cents int64, description string) (string, error) {
	status, body, err := c.send(ctx, http.MethodPost, "/refunds/"+url.PathEscape(chargeID), jsonType, map[string]any{"amount": formatAmount(cents), "description": description})
	if err != nil {
		return "", err
	}
	if !isSuccess(status) {
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
			return "", declined(status, body)
		}
		return "", fmt.Errorf("appypay refund: status %d", status)
	}
	var parsed chargeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("appypay refund body: %w", err)
	}
	return strings.ToLower(parsed.ID), nil
}
