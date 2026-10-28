//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

func newTestUser(t *testing.T, email, phone string) *entities.User {
	t.Helper()
	user, err := entities.NewUser("Ana", email, phone, "$argon2id$hash", enums.RoleCustomer)
	require.NoError(t, err)
	return user
}

func TestUserRepository(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)

	user := newTestUser(t, "ana@agora.ao", "+244923000000")
	require.NoError(t, users.Create(ctx, user))

	t.Run("finds by phone, case-insensitive email and id", func(t *testing.T) {
		byPhone, err := users.FindByPhone(ctx, "+244923000000")
		require.NoError(t, err)
		byEmail, err := users.FindByEmail(ctx, "ANA@agora.ao")
		require.NoError(t, err)
		byID, err := users.FindByID(ctx, user.ID)
		require.NoError(t, err)

		assert.Equal(t, user.ID, byPhone.ID)
		assert.Equal(t, user.ID, byEmail.ID)
		assert.Equal(t, enums.UserStatusActive, byID.Status)
		assert.Equal(t, enums.RoleCustomer, byID.Role)
	})

	t.Run("duplicate phone or email is reported", func(t *testing.T) {
		err := users.Create(ctx, newTestUser(t, "", "+244923000000"))
		require.ErrorIs(t, err, domainrepository.ErrPhoneAlreadyExists)

		err = users.Create(ctx, newTestUser(t, "Ana@Agora.ao", "+244923999999"))
		require.ErrorIs(t, err, domainrepository.ErrEmailAlreadyExists)
	})

	t.Run("update and last login persist", func(t *testing.T) {
		user.MarkVerified()
		require.NoError(t, users.Update(ctx, user))
		loginAt := time.Now().UTC().Truncate(time.Microsecond)
		require.NoError(t, users.UpdateLastLogin(ctx, user.ID, loginAt))

		stored, err := users.FindByID(ctx, user.ID)
		require.NoError(t, err)
		assert.True(t, stored.IsVerified)
		require.NotNil(t, stored.LastLoginAt)
		assert.True(t, loginAt.Equal(*stored.LastLoginAt))
	})

	t.Run("missing user is nil", func(t *testing.T) {
		missing, err := users.FindByPhone(ctx, "+244900000000")
		require.NoError(t, err)
		assert.Nil(t, missing)
	})
}

func TestClientRepository(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	clients := repository.NewClientRepository(pool)

	seeded := map[string]enums.ClientType{
		"agora-customer-app":  enums.ClientTypeCustomerApp,
		"agora-provider-app":  enums.ClientTypeProviderApp,
		"agora-partner-panel": enums.ClientTypePartnerPanel,
		"agora-backoffice":    enums.ClientTypeBackoffice,
	}
	for clientID, clientType := range seeded {
		client, err := clients.FindActiveByClientID(ctx, clientID)
		require.NoError(t, err)
		require.NotNil(t, client, clientID)
		assert.Equal(t, clientType, client.Type)
	}

	_, err := pool.Exec(ctx, "UPDATE clients SET is_active = false WHERE client_id = 'agora-partner-panel'")
	require.NoError(t, err)
	inactive, err := clients.FindActiveByClientID(ctx, "agora-partner-panel")
	require.NoError(t, err)
	assert.Nil(t, inactive)

	unknown, err := clients.FindActiveByClientID(ctx, "someone-else")
	require.NoError(t, err)
	assert.Nil(t, unknown)
}

func TestRefreshTokenRepository(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	user := newTestUser(t, "", "+244923000000")
	require.NoError(t, repository.NewUserRepository(pool).Create(ctx, user))
	client, err := repository.NewClientRepository(pool).FindActiveByClientID(ctx, "agora-customer-app")
	require.NoError(t, err)
	tokens := repository.NewRefreshTokenRepository(pool)

	current, err := entities.StartRefreshTokenFamily(user.ID, client.ID, "hash-1", time.Hour, entities.SessionMetadata{UserAgent: "app", IPAddress: "10.0.0.1"})
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, current))

	next, err := current.Rotate("hash-2", time.Hour, entities.SessionMetadata{})
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, next))
	require.NoError(t, tokens.MarkRotated(ctx, current))

	rotated, err := tokens.FindByHashForUpdate(ctx, "hash-1")
	require.NoError(t, err)
	assert.True(t, rotated.IsRevoked())
	assert.Equal(t, next.ID, *rotated.ReplacedByID)
	assert.Equal(t, "app", rotated.UserAgent)
	assert.True(t, rotated.BelongsTo(client))

	require.NoError(t, tokens.RevokeFamily(ctx, current.FamilyID, time.Now().UTC()))
	revoked, err := tokens.FindByHashForUpdate(ctx, "hash-2")
	require.NoError(t, err)
	assert.True(t, revoked.IsRevoked())

	missing, err := tokens.FindByHashForUpdate(ctx, "missing")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestTransactionRollsBackUserAndOutboxTogether(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)
	outboxRepo := outbox.NewRepository(pool)
	txm := postgres.NewTxManager(pool)

	user := newTestUser(t, "", "+244923000000")
	env, err := events.NewEnvelope(events.UserRegisteredV1{ID: user.ID, Role: user.Role, CreatedAt: user.CreatedAt}, "corr")
	require.NoError(t, err)

	boom := errors.New("fail after writes")
	err = txm.WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, users.Create(ctx, user))
		require.NoError(t, outboxRepo.Save(ctx, env))
		return boom
	})
	require.ErrorIs(t, err, boom)

	missing, err := users.FindByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, missing)

	var outboxRows int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox").Scan(&outboxRows))
	assert.Zero(t, outboxRows)

	require.NoError(t, txm.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := users.Create(ctx, user); err != nil {
			return err
		}
		return outboxRepo.Save(ctx, env)
	}))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE published_at IS NULL").Scan(&outboxRows))
	assert.Equal(t, 1, outboxRows)
}
