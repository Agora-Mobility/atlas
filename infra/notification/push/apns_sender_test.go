package push_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/notification/push"
)

func apnsKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func apnsSender(t *testing.T, handler http.HandlerFunc) (*push.APNSSender, *ecdsa.PrivateKey) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	key, keyPEM := apnsKey(t)
	sender, err := push.NewAPNSSender(keyPEM, "KEY123", "TEAM456", "ao.agora.cliente", server.URL, time.Second)
	require.NoError(t, err)
	return sender, key
}

func verifyProviderToken(t *testing.T, key *ecdsa.PrivateKey, authorization string) {
	t.Helper()
	token, found := strings.CutPrefix(authorization, "bearer ")
	require.True(t, found, "authorization is a bearer token")
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"alg":"ES256","kid":"KEY123"}`, string(header))
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(claims, &parsed))
	assert.Equal(t, "TEAM456", parsed["iss"])
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, signature, 64)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])
	assert.True(t, ecdsa.Verify(&key.PublicKey, digest[:], r, s), "the signature verifies with the key")
}

func TestLiveActivityUpdateGoesToAPNsWithTheLiveActivityHeaders(t *testing.T) {
	var body map[string]any
	var authorization string
	sender, key := apnsSender(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/3/device/activity-token", r.URL.Path)
		assert.Equal(t, "liveactivity", r.Header.Get("apns-push-type"))
		assert.Equal(t, "ao.agora.cliente.push-type.liveactivity", r.Header.Get("apns-topic"))
		assert.Equal(t, "10", r.Header.Get("apns-priority"))
		assert.NotEmpty(t, r.Header.Get("apns-expiration"))
		authorization = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("apns-id", "apns-1")
	})
	at := time.Unix(1_800_000_000, 0)
	stale := at.Add(2 * time.Minute)

	id, err := sender.Send(context.Background(), services.LiveActivityMessage{
		Token: "activity-token", Event: services.LiveActivityUpdate, ContentState: map[string]any{"status": "provider_en_route", "etaSeconds": 120},
		Timestamp: at, StaleAt: &stale, AlertTitle: "A caminho", HighPriority: true, TTLSeconds: 30,
	})

	require.NoError(t, err)
	assert.Equal(t, "apns-1", id)
	verifyProviderToken(t, key, authorization)
	aps := body["aps"].(map[string]any)
	assert.Equal(t, "update", aps["event"])
	assert.InDelta(t, 1_800_000_000, aps["timestamp"], 0)
	assert.InDelta(t, 1_800_000_120, aps["stale-date"], 0)
	assert.NotContains(t, aps, "dismissal-date")
	assert.Equal(t, "provider_en_route", aps["content-state"].(map[string]any)["status"])
	assert.Equal(t, "A caminho", aps["alert"].(map[string]any)["title"])
}

func TestLiveActivityEndCarriesTheDismissalDateAndLowPriority(t *testing.T) {
	var body map[string]any
	sender, _ := apnsSender(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.Header.Get("apns-priority"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	})
	at := time.Unix(1_800_000_000, 0)
	dismiss := at.Add(5 * time.Minute)

	_, err := sender.Send(context.Background(), services.LiveActivityMessage{
		Token: "t", Event: services.LiveActivityEnd, ContentState: map[string]any{"status": "completed"}, Timestamp: at, DismissAt: &dismiss,
	})

	require.NoError(t, err)
	aps := body["aps"].(map[string]any)
	assert.Equal(t, "end", aps["event"])
	assert.InDelta(t, 1_800_000_300, aps["dismissal-date"], 0)
	assert.NotContains(t, aps, "alert")
}

func TestLiveActivityTokenThatAPNsNoLongerKnowsIsInvalid(t *testing.T) {
	for name, answer := range map[string]struct {
		status int
		reason string
	}{
		"gone":         {http.StatusGone, "Unregistered"},
		"bad token":    {http.StatusBadRequest, "BadDeviceToken"},
		"wrong bundle": {http.StatusBadRequest, "DeviceTokenNotForTopic"},
	} {
		t.Run(name, func(t *testing.T) {
			sender, _ := apnsSender(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(answer.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"reason": answer.reason})
			})
			_, err := sender.Send(context.Background(), services.LiveActivityMessage{Token: "t", Event: services.LiveActivityUpdate, Timestamp: time.Now()})
			require.ErrorIs(t, err, services.ErrPushTokenInvalid)
		})
	}
}

func TestLiveActivityPayloadAPNsRefusesIsRejectedForGood(t *testing.T) {
	sender, _ := apnsSender(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": "PayloadTooLarge"})
	})
	_, err := sender.Send(context.Background(), services.LiveActivityMessage{Token: "t", Event: services.LiveActivityUpdate, Timestamp: time.Now()})
	require.ErrorIs(t, err, services.ErrPushRejected)
}

func TestLiveActivityRetriesOnceWithAFreshProviderToken(t *testing.T) {
	var tokens []string
	sender, _ := apnsSender(t, func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		if len(tokens) == 1 {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"reason": "ExpiredProviderToken"})
		}
	})
	_, err := sender.Send(context.Background(), services.LiveActivityMessage{Token: "t", Event: services.LiveActivityUpdate, Timestamp: time.Now()})
	require.NoError(t, err)
	require.Len(t, tokens, 2)
}

func TestLiveActivityServerErrorCanBeRetried(t *testing.T) {
	sender, _ := apnsSender(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := sender.Send(context.Background(), services.LiveActivityMessage{Token: "t", Event: services.LiveActivityUpdate, Timestamp: time.Now()})
	require.Error(t, err)
	assert.NotErrorIs(t, err, services.ErrPushRejected)
	assert.NotErrorIs(t, err, services.ErrPushTokenInvalid)
}

func TestAPNsKeyThatIsNotPEMIsRefused(t *testing.T) {
	_, err := push.NewAPNSSender([]byte("not a key"), "K", "T", "b", "https://api.push.apple.com", time.Second)
	require.Error(t, err)
}
