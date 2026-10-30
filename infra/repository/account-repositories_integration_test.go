//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

func TestUserRepository_ListFiltersAndPages(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)

	ana := newTestUser(t, "ana@agora.ao", "+244923000001")
	bruno := newTestUser(t, "bruno@agora.ao", "+244923000002")
	bruno.Block()
	carla, err := entities.NewUser("Carla 100%_Real", "carla@agora.ao", "+244923000003", "$argon2id$hash", enums.RoleProvider)
	require.NoError(t, err)
	for _, u := range []*entities.User{ana, bruno, carla} {
		require.NoError(t, users.Create(ctx, u))
	}

	cases := []struct {
		name   string
		filter domainrepository.UserFilter
		want   []uuid.UUID
	}{
		{"no filter", domainrepository.UserFilter{}, []uuid.UUID{carla.ID, bruno.ID, ana.ID}},
		{"by role", domainrepository.UserFilter{Roles: []enums.Role{enums.RoleProvider}}, []uuid.UUID{carla.ID}},
		{"by several roles", domainrepository.UserFilter{Roles: []enums.Role{enums.RoleProvider, enums.RoleStaff}}, []uuid.UUID{carla.ID}},
		{"by status", domainrepository.UserFilter{Status: enums.UserStatusBlocked}, []uuid.UUID{bruno.ID}},
		{"by name, case-insensitive", domainrepository.UserFilter{Search: "BRUNO"}, []uuid.UUID{bruno.ID}},
		{"by phone fragment", domainrepository.UserFilter{Search: "000001"}, []uuid.UUID{ana.ID}},
		{"wildcards are literal", domainrepository.UserFilter{Search: "%_"}, []uuid.UUID{carla.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, total, err := users.List(ctx, tc.filter, pagination.Params{})
			require.NoError(t, err)
			assert.Equal(t, int64(len(tc.want)), total)
			var ids []uuid.UUID
			for _, u := range found {
				ids = append(ids, u.ID)
			}
			assert.Equal(t, tc.want, ids)
		})
	}

	t.Run("pages", func(t *testing.T) {
		found, total, err := users.List(ctx, domainrepository.UserFilter{}, pagination.Params{Page: 2, Size: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(3), total)
		require.Len(t, found, 1)
		assert.Equal(t, ana.ID, found[0].ID)
	})
}

func TestUserRepository_SoftDeleteFreesPhoneAndEmail(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)

	user := newTestUser(t, "ana@agora.ao", "+244923000000")
	require.NoError(t, users.Create(ctx, user))
	require.NoError(t, users.SoftDelete(ctx, user.ID, time.Now().UTC()))

	gone, err := users.FindByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, gone)
	require.ErrorIs(t, users.SoftDelete(ctx, user.ID, time.Now().UTC()), domainrepository.ErrNotFound)

	require.NoError(t, users.Create(ctx, newTestUser(t, "ana@agora.ao", "+244923000000")), "a new account can reuse the contacts")
}

func TestRefreshTokenRepository_ListSessionsIssuedSince(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	user := newTestUser(t, "", "+244923000000")
	require.NoError(t, repository.NewUserRepository(pool).Create(ctx, user))
	client, err := repository.NewClientRepository(pool).FindActiveByClientID(ctx, "agora-customer-app")
	require.NoError(t, err)
	tokens := repository.NewRefreshTokenRepository(pool)

	old, err := entities.StartRefreshTokenFamily(user.ID, client.ID, nil, "hash-old", time.Hour, entities.SessionMetadata{})
	require.NoError(t, err)
	old.CreatedAt = time.Now().UTC().Add(-time.Hour)
	require.NoError(t, tokens.Create(ctx, old))

	recent, err := entities.StartRefreshTokenFamily(user.ID, client.ID, nil, "hash-1", time.Hour, entities.SessionMetadata{})
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, recent))
	rotated, err := recent.Rotate("hash-2", time.Hour, entities.SessionMetadata{})
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, rotated))
	require.NoError(t, tokens.RevokeAllForUser(ctx, user.ID, time.Now().UTC()))

	sessions, err := tokens.ListSessionsIssuedSince(ctx, user.ID, time.Now().UTC().Add(-15*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{recent.FamilyID}, sessions, "revoked tokens still count, each session once")
}

func TestAuditLogRepository_IsAppendOnly(t *testing.T) {
	pool, owner := setupDBs(t)
	ctx := context.Background()
	audit := repository.NewAuditLogRepository(pool)

	actorID := uuid.New()
	orgID := uuid.New()
	actor := entities.Actor{ID: &actorID, Role: enums.RoleStaff, OrganizationID: &orgID, IPAddress: "10.0.0.1", UserAgent: "backoffice"}
	entry, err := entities.NewAuditEntry(actor, "user.block", "user", "abc", map[string]string{"status": "active"}, map[string]string{"status": "blocked"}, "Fraude", "corr-1")
	require.NoError(t, err)
	require.NoError(t, audit.Record(ctx, entry))

	system, err := entities.NewAuditEntry(entities.SystemActor(), "user.bootstrap-admin", "user", "def", nil, map[string]string{"role": "admin"}, "", "")
	require.NoError(t, err)
	require.NoError(t, audit.Record(ctx, system))

	var after, reason string
	var role *string
	var organization *uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, "SELECT after->>'status', reason, actor_role::text, organization_id FROM audit_log WHERE id = $1", entry.ID).Scan(&after, &reason, &role, &organization))
	assert.Equal(t, "blocked", after)
	assert.Equal(t, "Fraude", reason)
	assert.Equal(t, "staff", *role)
	assert.Equal(t, orgID, *organization)

	_, err = pool.Exec(ctx, "UPDATE audit_log SET reason = 'x'")
	require.ErrorContains(t, err, "permission denied", "the application role can only insert and read")
	_, err = pool.Exec(ctx, "DELETE FROM audit_log")
	require.ErrorContains(t, err, "permission denied")
	_, err = owner.Exec(ctx, "UPDATE audit_log SET reason = 'x'")
	require.ErrorContains(t, err, "append-only", "even the owner cannot change it")
	_, err = owner.Exec(ctx, "DELETE FROM audit_log")
	require.ErrorContains(t, err, "append-only")
}
