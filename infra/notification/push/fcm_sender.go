package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	messagingScope   = "https://www.googleapis.com/auth/firebase.messaging"
	maxResponseBytes = 1 << 20
)

type FCMSender struct {
	baseURL   string
	projectID string
	tokens    oauth2.TokenSource
	http      *http.Client
}

var _ services.IPushSender = (*FCMSender)(nil)

func NewFCMSender(ctx context.Context, credentialsJSON []byte, projectID, baseURL string, timeout time.Duration) (*FCMSender, error) {
	credentials, err := google.CredentialsFromJSON(ctx, credentialsJSON, messagingScope) //nolint:staticcheck // the file comes from the deployment secret
	if err != nil {
		return nil, fmt.Errorf("read fcm credentials: %w", err)
	}
	return NewFCMSenderWithTokens(credentials.TokenSource, projectID, baseURL, timeout), nil
}

func NewFCMSenderWithTokens(tokens oauth2.TokenSource, projectID, baseURL string, timeout time.Duration) *FCMSender {
	return &FCMSender{baseURL: strings.TrimRight(baseURL, "/"), projectID: projectID, tokens: tokens, http: &http.Client{Timeout: timeout}}
}

type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      fcmAndroid        `json:"android"`
	APNS         fcmAPNS           `json:"apns"`
}

type fcmNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type fcmAndroid struct {
	Priority string `json:"priority"`
	TTL      string `json:"ttl,omitempty"`
}

type fcmAPNS struct {
	Headers map[string]string `json:"headers"`
}

type fcmResponse struct {
	Name  string `json:"name"`
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func (r fcmResponse) errorCode() string {
	for _, detail := range r.Error.Details {
		if detail.ErrorCode != "" {
			return detail.ErrorCode
		}
	}
	return r.Error.Status
}

var (
	deadTokenCodes = map[string]bool{"UNREGISTERED": true, "SENDER_ID_MISMATCH": true}
	rejectedCodes  = map[string]bool{"INVALID_ARGUMENT": true, "THIRD_PARTY_AUTH_ERROR": true, "PERMISSION_DENIED": true}
)

func failureOf(status int, parsed fcmResponse) error {
	code := parsed.errorCode()
	switch {
	case deadTokenCodes[code] || status == http.StatusNotFound:
		return fmt.Errorf("%w: %s", services.ErrPushTokenInvalid, code)
	case rejectedCodes[code]:
		return fmt.Errorf("%w: fcm answered %d %s: %s", services.ErrPushRejected, status, code, parsed.Error.Message)
	default:
		return fmt.Errorf("fcm answered %d %s: %s", status, code, parsed.Error.Message)
	}
}

func (s *FCMSender) Send(ctx context.Context, message services.PushMessage) (string, error) {
	token, err := s.tokens.Token()
	if err != nil {
		return "", fmt.Errorf("fcm token: %w", err)
	}
	body, err := json.Marshal(fcmRequest{Message: toFCM(message)})
	if err != nil {
		return "", fmt.Errorf("encode fcm message: %w", err)
	}
	url := s.baseURL + "/v1/projects/" + s.projectID + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build fcm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	res, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call fcm: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var parsed fcmResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("decode fcm response: %w", err)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", failureOf(res.StatusCode, parsed)
	}
	return parsed.Name, nil
}

func toFCM(message services.PushMessage) fcmMessage {
	priority, apnsPriority := "NORMAL", "5"
	if message.HighPriority {
		priority, apnsPriority = "HIGH", "10"
	}
	out := fcmMessage{
		Token: message.Token, Data: message.Data,
		Android: fcmAndroid{Priority: priority},
		APNS:    fcmAPNS{Headers: map[string]string{"apns-priority": apnsPriority}},
	}
	if message.Title != "" || message.Body != "" {
		out.Notification = &fcmNotification{Title: message.Title, Body: message.Body}
	}
	if message.TTLSeconds > 0 {
		out.Android.TTL = strconv.Itoa(message.TTLSeconds) + "s"
		out.APNS.Headers["apns-expiration"] = strconv.FormatInt(time.Now().Add(time.Duration(message.TTLSeconds)*time.Second).Unix(), 10)
	}
	return out
}
