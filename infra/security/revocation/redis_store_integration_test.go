//go:build integration

package revocation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/infra/security/revocation"
)

const redisImage = "redis:8.8.4-alpine"

func setupRedis(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := goredis.NewClient(&goredis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisStore(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	store := revocation.NewRedisStore(client, "atlas:", time.Minute)
	first, second, live := uuid.New(), uuid.New(), uuid.New()

	require.NoError(t, store.RevokeSessions(ctx))
	require.NoError(t, store.RevokeSessions(ctx, first, second))

	for _, id := range []uuid.UUID{first, second} {
		revoked, err := store.IsSessionRevoked(ctx, id)
		require.NoError(t, err)
		assert.True(t, revoked)
	}
	revoked, err := store.IsSessionRevoked(ctx, live)
	require.NoError(t, err)
	assert.False(t, revoked)

	ttl, err := client.TTL(ctx, "atlas:revoked-session:"+first.String()).Result()
	require.NoError(t, err)
	assert.InDelta(t, time.Minute.Seconds(), ttl.Seconds(), 2, "entries live as long as an access token")
}
