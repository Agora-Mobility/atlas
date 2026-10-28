package sms_test

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
	"github.com/Agora-Mobility/atlas/infra/notification/sms"
)

type capturedRequest struct {
	body map[string]any
}

func fakeCastBrick(t *testing.T, status int, response string, received *capturedRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/sms/send", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		if received != nil {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&received.body))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCastBrickSenderSendsToTheRecipient(t *testing.T) {
	var received capturedRequest
	server := fakeCastBrick(t, http.StatusOK, `{"messageId":"msg-1","status":"queued","recipientCount":1}`, &received)
	sender := sms.NewCastBrickSender("test-key", server.URL, "AGORA", time.Second)

	err := sender.Send(context.Background(), "+244923000000", "Código de verificação: 123456.")

	require.NoError(t, err)
	assert.Equal(t, []any{"+244923000000"}, received.body["recipients"])
	assert.Equal(t, "Código de verificação: 123456.", received.body["content"])
	assert.Equal(t, "AGORA", received.body["senderId"])
}

func TestCastBrickSenderReportsProviderFailuresAsUnavailable(t *testing.T) {
	cases := map[string]struct {
		status   int
		response string
		reason   string
	}{
		"invalid key":          {http.StatusUnauthorized, `{"error":"unauthorized"}`, "invalid or revoked api key"},
		"insufficient credits": {http.StatusPaymentRequired, `{"error":"no credits"}`, "insufficient credits"},
		"validation":           {http.StatusUnprocessableEntity, `{"error":"bad phone"}`, "rejected by provider validation"},
		"provider down":        {http.StatusBadGateway, `bad gateway`, "status 502"},
		"error in success":     {http.StatusOK, `{"error":"sender id not approved"}`, "sender id not approved"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := fakeCastBrick(t, tc.status, tc.response, nil)
			sender := sms.NewCastBrickSender("test-key", server.URL, "", time.Second)

			err := sender.Send(context.Background(), "+244923000000", "teste")

			require.ErrorIs(t, err, services.ErrSMSUnavailable)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestCastBrickSenderTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(server.Close)
	sender := sms.NewCastBrickSender("test-key", server.URL, "", 50*time.Millisecond)

	err := sender.Send(context.Background(), "+244923000000", "teste")

	require.ErrorIs(t, err, services.ErrSMSUnavailable)
}
