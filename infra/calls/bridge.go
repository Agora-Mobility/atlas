package calls

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agora-Mobility/atlas/domain/services"
)

type Direct struct{}

func (Direct) Connect(_ context.Context, _, toPhone, _ string) (*services.Call, error) {
	if toPhone == "" {
		return nil, services.ErrCallsUnavailable
	}
	return &services.Call{Number: toPhone}, nil
}

// Bridge talks to a masked call provider through a small HTTP contract, so a
// provider is chosen by configuration: POST {base}/sessions with from, to and
// reference, answering proxy_number and expires_at.
type Bridge struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewBridge(baseURL, apiKey string, timeout time.Duration) *Bridge {
	return &Bridge{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: timeout}}
}

func (b *Bridge) Connect(ctx context.Context, fromPhone, toPhone, reference string) (*services.Call, error) {
	body, err := json.Marshal(map[string]string{"from": fromPhone, "to": toPhone, "reference": reference})
	if err != nil {
		return nil, fmt.Errorf("encode call: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build call: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	res, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", services.ErrCallsUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", services.ErrCallsUnavailable, res.StatusCode)
	}
	var out struct {
		ProxyNumber string     `json:"proxy_number"`
		ExpiresAt   *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", services.ErrCallsUnavailable, err)
	}
	if out.ProxyNumber == "" {
		return nil, fmt.Errorf("%w: no number", services.ErrCallsUnavailable)
	}
	return &services.Call{Number: out.ProxyNumber, Masked: true, ExpiresAt: out.ExpiresAt}, nil
}
