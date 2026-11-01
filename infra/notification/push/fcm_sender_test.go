package push_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/notification/push"
)

func sender(t *testing.T, handler http.HandlerFunc) *push.FCMSender {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "access"})
	return push.NewFCMSenderWithTokens(tokens, "agora", server.URL, time.Second)
}

func TestSendPostsAHighPriorityDataMessage(t *testing.T) {
	var body map[string]any
	client := sender(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/projects/agora/messages:send", r.URL.Path)
		assert.Equal(t, "Bearer access", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "projects/agora/messages/1"})
	})

	id, err := client.Send(context.Background(), services.PushMessage{
		Token: "device", Title: "Oferta", Body: "Novo pedido", Data: map[string]string{"offer_id": "1"}, HighPriority: true, TTLSeconds: 18,
	})

	require.NoError(t, err)
	assert.Equal(t, "projects/agora/messages/1", id)
	message := body["message"].(map[string]any)
	assert.Equal(t, "device", message["token"])
	assert.Equal(t, "HIGH", message["android"].(map[string]any)["priority"])
	assert.Equal(t, "18s", message["android"].(map[string]any)["ttl"])
	assert.Equal(t, "10", message["apns"].(map[string]any)["headers"].(map[string]any)["apns-priority"])
	assert.Equal(t, "1", message["data"].(map[string]any)["offer_id"])
}

func TestUnregisteredTokenIsReported(t *testing.T) {
	client := sender(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"status": "UNREGISTERED"}})
	})
	_, err := client.Send(context.Background(), services.PushMessage{Token: "gone"})
	assert.ErrorIs(t, err, services.ErrPushTokenInvalid)
}

func TestServerErrorIsRetryable(t *testing.T) {
	client := sender(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, err := client.Send(context.Background(), services.PushMessage{Token: "device"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, services.ErrPushTokenInvalid)
}
