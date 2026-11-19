package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
)

const (
	// Apple refuses provider tokens older than an hour and throttles new ones
	// made more often than every 20 minutes.
	apnsTokenLifetime  = 50 * time.Minute
	apnsSignatureBytes = 32
	liveActivitySuffix = ".push-type.liveactivity"
)

var (
	errAPNSKey = errors.New("the APNs key is not an ECDSA P-256 key in PKCS#8")

	apnsDeadTokenReasons = map[string]bool{"BadDeviceToken": true, "Unregistered": true, "DeviceTokenNotForTopic": true, "ExpiredToken": true}
	apnsAuthReasons      = map[string]bool{"ExpiredProviderToken": true, "InvalidProviderToken": true}
)

// APNSSender sends Live Activity pushes straight to APNs with a token-based
// (.p8) key. net/http speaks HTTP/2 to APNs on its own.
type APNSSender struct {
	baseURL string
	topic   string
	keyID   string
	teamID  string
	key     *ecdsa.PrivateKey
	http    *http.Client
	now     func() time.Time

	mu       sync.Mutex
	token    string
	issuedAt time.Time
}

var _ services.ILiveActivitySender = (*APNSSender)(nil)

// NewAPNSSender reads the .p8 key (PEM). bundleID is the bundle of the customer
// app; the Live Activity topic is derived from it.
func NewAPNSSender(keyPEM []byte, keyID, teamID, bundleID, baseURL string, timeout time.Duration) (*APNSSender, error) {
	key, err := parseAPNSKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return &APNSSender{
		baseURL: strings.TrimRight(baseURL, "/"), topic: bundleID + liveActivitySuffix, keyID: keyID, teamID: teamID,
		key: key, http: tracing.HTTPClient(timeout), now: time.Now,
	}, nil
}

func parseAPNSKey(keyPEM []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errAPNSKey
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAPNSKey, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errAPNSKey
	}
	return key, nil
}

func (s *APNSSender) providerToken(refresh bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !refresh && s.token != "" && now.Sub(s.issuedAt) < apnsTokenLifetime {
		return s.token, nil
	}
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": s.keyID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{"iss": s.teamID, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, sig, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign apns token: %w", err)
	}
	signature := append(fixed(r), fixed(sig)...)
	s.token, s.issuedAt = unsigned+"."+base64.RawURLEncoding.EncodeToString(signature), now
	return s.token, nil
}

func fixed(n *big.Int) []byte {
	out := make([]byte, apnsSignatureBytes)
	return n.FillBytes(out)
}

type apnsAPS struct {
	Timestamp    int64      `json:"timestamp"`
	Event        string     `json:"event"`
	ContentState any        `json:"content-state"`
	StaleDate    *int64     `json:"stale-date,omitempty"`
	DismissDate  *int64     `json:"dismissal-date,omitempty"`
	Alert        *apnsAlert `json:"alert,omitempty"`
}

type apnsAlert struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

func unix(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	value := t.Unix()
	return &value
}

func apnsPayload(message services.LiveActivityMessage) ([]byte, error) {
	aps := apnsAPS{
		Timestamp: message.Timestamp.Unix(), Event: message.Event, ContentState: message.ContentState,
		StaleDate: unix(message.StaleAt), DismissDate: unix(message.DismissAt),
	}
	if message.AlertTitle != "" || message.AlertBody != "" {
		aps.Alert = &apnsAlert{Title: message.AlertTitle, Body: message.AlertBody}
	}
	return json.Marshal(map[string]any{"aps": aps})
}

func (s *APNSSender) Send(ctx context.Context, message services.LiveActivityMessage) (string, error) {
	body, err := apnsPayload(message)
	if err != nil {
		return "", fmt.Errorf("encode apns payload: %w", err)
	}
	id, status, reason, err := s.post(ctx, message, body, false)
	if err == nil && status == http.StatusForbidden && apnsAuthReasons[reason] {
		id, status, reason, err = s.post(ctx, message, body, true)
	}
	if err != nil {
		return "", err
	}
	if status >= http.StatusBadRequest {
		return "", apnsFailure(status, reason)
	}
	return id, nil
}

func (s *APNSSender) post(ctx context.Context, message services.LiveActivityMessage, body []byte, refresh bool) (string, int, string, error) {
	token, err := s.providerToken(refresh)
	if err != nil {
		return "", 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/3/device/"+message.Token, bytes.NewReader(body))
	if err != nil {
		return "", 0, "", fmt.Errorf("build apns request: %w", err)
	}
	priority := "5"
	if message.HighPriority {
		priority = "10"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("apns-push-type", "liveactivity")
	req.Header.Set("apns-topic", s.topic)
	req.Header.Set("apns-priority", priority)
	if message.TTLSeconds > 0 {
		req.Header.Set("apns-expiration", strconv.FormatInt(s.now().Add(time.Duration(message.TTLSeconds)*time.Second).Unix(), 10))
	}
	res, err := s.http.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("call apns: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var parsed struct {
		Reason string `json:"reason"`
	}
	if res.StatusCode >= http.StatusBadRequest {
		if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
			return "", 0, "", fmt.Errorf("decode apns response: %w", err)
		}
	}
	return res.Header.Get("apns-id"), res.StatusCode, parsed.Reason, nil
}

func apnsFailure(status int, reason string) error {
	switch {
	case status == http.StatusGone || apnsDeadTokenReasons[reason]:
		return fmt.Errorf("%w: %s", services.ErrPushTokenInvalid, reason)
	case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		return fmt.Errorf("apns answered %d %s", status, reason)
	default:
		return fmt.Errorf("%w: apns answered %d %s", services.ErrPushRejected, status, reason)
	}
}
