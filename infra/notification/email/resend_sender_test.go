package email_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/notification/email"
)

type capturedRequest struct {
	body           map[string]any
	idempotencyKey string
}

func fakeResend(t *testing.T, status int, response string, received *capturedRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/emails", r.URL.Path)
		assert.Equal(t, "Bearer re_test", r.Header.Get("Authorization"))
		if received != nil {
			received.idempotencyKey = r.Header.Get("Idempotency-Key")
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&received.body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server
}

func newSender(t *testing.T, baseURL string, timeout time.Duration) *email.ResendSender {
	t.Helper()
	sender, err := email.NewResendSender("re_test", baseURL, "Agora <no-reply@agora.ao>", "suporte@agora.ao", timeout)
	require.NoError(t, err)
	return sender
}

func TestResendSenderSendsTheEmail(t *testing.T) {
	var received capturedRequest
	server := fakeResend(t, http.StatusOK, `{"id":"email-1"}`, &received)

	err := newSender(t, server.URL, time.Second).Send(context.Background(), services.Email{
		To: "ana@agora.ao", Subject: "Bem-vinda", HTML: "<p>Olá</p>", Text: "Olá", IdempotencyKey: "welcome/ana",
	})

	require.NoError(t, err)
	assert.Equal(t, "Agora <no-reply@agora.ao>", received.body["from"])
	assert.Equal(t, []any{"ana@agora.ao"}, received.body["to"])
	assert.Equal(t, "Bem-vinda", received.body["subject"])
	assert.Equal(t, "<p>Olá</p>", received.body["html"])
	assert.Equal(t, "Olá", received.body["text"])
	assert.Equal(t, "suporte@agora.ao", received.body["reply_to"])
	assert.Equal(t, "welcome/ana", received.idempotencyKey)
}

func TestResendSenderReportsProviderFailuresAsUnavailable(t *testing.T) {
	cases := map[string]struct {
		status   int
		response string
		reason   string
	}{
		"invalid key":       {http.StatusUnauthorized, `{"message":"API key is invalid"}`, "API key is invalid"},
		"unverified domain": {http.StatusForbidden, `{"message":"The agora.ao domain is not verified"}`, "domain is not verified"},
		"validation":        {http.StatusUnprocessableEntity, `{"statusCode":422,"name":"validation_error","message":"Invalid to field"}`, "Invalid to field"},
		"rate limit":        {http.StatusTooManyRequests, `{"message":"Too many requests"}`, "rate limit exceeded"},
		"provider down":     {http.StatusBadGateway, `{"message":"bad gateway"}`, "bad gateway"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := fakeResend(t, tc.status, tc.response, nil)

			err := newSender(t, server.URL, time.Second).Send(context.Background(), services.Email{To: "ana@agora.ao", Subject: "x", Text: "x"})

			require.ErrorIs(t, err, services.ErrEmailUnavailable)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestResendSenderTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	err := newSender(t, server.URL, 50*time.Millisecond).Send(context.Background(), services.Email{To: "ana@agora.ao", Subject: "x", Text: "x"})

	require.ErrorIs(t, err, services.ErrEmailUnavailable)
}
