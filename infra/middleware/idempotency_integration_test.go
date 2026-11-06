//go:build integration

package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/infra/middleware"
)

func redisForTest(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:8.8.4-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp")},
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := goredis.NewClient(&goredis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestIdempotentActionsRunOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idempotency := middleware.NewIdempotency(redisForTest(t), time.Hour)
	var calls atomic.Int64
	status := atomic.Int64{}
	status.Store(http.StatusCreated)
	engine := gin.New()
	engine.POST("/things", idempotency.Required(), func(ctx *gin.Context) {
		n := calls.Add(1)
		ctx.JSON(int(status.Load()), gin.H{"call": n})
	})
	send := func(key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/things", strings.NewReader(body))
		if key != "" {
			req.Header.Set(middleware.HeaderIdempotencyKey, key)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	t.Run("the key is required", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, send("", `{}`).Code)
	})

	t.Run("a resend replays the first answer", func(t *testing.T) {
		first := send("a", `{"x":1}`)
		second := send("a", `{"x":1}`)
		assert.Equal(t, http.StatusCreated, second.Code)
		assert.Equal(t, first.Body.String(), second.Body.String())
		assert.Equal(t, "true", second.Header().Get("Idempotent-Replayed"))
		assert.EqualValues(t, 1, calls.Load())
	})

	t.Run("the same key with another body is refused", func(t *testing.T) {
		rec := send("a", `{"x":2}`)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		var body map[string]map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "IDEMPOTENCY_KEY_REUSED", body["error"]["code"])
	})

	t.Run("a server failure can be retried", func(t *testing.T) {
		calls.Store(0)
		status.Store(http.StatusInternalServerError)
		assert.Equal(t, http.StatusInternalServerError, send("b", `{}`).Code)
		status.Store(http.StatusCreated)
		assert.Equal(t, http.StatusCreated, send("b", `{}`).Code)
		assert.EqualValues(t, 2, calls.Load())
	})
}
