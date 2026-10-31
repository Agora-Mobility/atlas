//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/migrations"
)

var appTables = []string{
	"audit_log", "block_reasons", "clients", "invitations", "membership_roles", "memberships", "mfa_recovery_codes", "organizations", "outbox",
	"permissions", "refresh_tokens", "role_permissions", "roles", "schema_migrations", "service_categories", "services", "user_block_reasons",
	"user_blocks", "user_mfa", "users",
}

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgis/postgis:18-3.6-alpine",
		tcpostgres.WithDatabase("atlas_test"),
		// A user name different from the schema, so "$user" in the default
		// search_path does not hide a misplaced object.
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("app"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return url
}

func connect(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.OwnerConnection(context.Background(), config.DatabaseConfig{
		URL: url, MaxConns: 2, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func tablesIn(t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind = 'r'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
		ORDER BY c.relname`, schema)
	require.NoError(t, err)
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func typeSchema(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var schema string
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT n.nspname FROM pg_type ty JOIN pg_namespace n ON n.oid = ty.typnamespace WHERE ty.typname = $1", name).Scan(&schema))
	return schema
}

func assertEverythingInAtlas(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	assert.Equal(t, appTables, tablesIn(t, pool, postgres.Schema))
	assert.Empty(t, tablesIn(t, pool, "public"), "public keeps only extension objects")
	for _, name := range []string{"client_type", "user_role", "user_status", "organization_type", "membership_status"} {
		assert.Equal(t, postgres.Schema, typeSchema(t, pool, name), name)
	}
	var postgisSchema string
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'postgis'").Scan(&postgisSchema))
	assert.Equal(t, "public", postgisSchema)
}

func TestMigrate_NewDatabaseLivesInAtlas(t *testing.T) {
	url := startPostgres(t)
	require.NoError(t, postgres.Migrate(url, "up"))
	pool := connect(t, url)

	assertEverythingInAtlas(t, pool)

	var clients int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM clients").Scan(&clients))
	assert.Equal(t, 4, clients, "unqualified queries resolve through the search_path")

	t.Run("down and up again", func(t *testing.T) {
		require.NoError(t, postgres.Migrate(url, "down"))
		require.NoError(t, postgres.Migrate(url, "up"))
		assertEverythingInAtlas(t, pool)
	})
}

func TestMigrate_MovesALegacyPublicDatabaseKeepingData(t *testing.T) {
	url := startPostgres(t)
	ctx := context.Background()

	// The runner before this change: default search_path, everything in public.
	src, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	legacy, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+strings.TrimPrefix(url, "postgres://"))
	require.NoError(t, err)
	require.NoError(t, legacy.Migrate(6))
	_, _ = legacy.Close()

	pool := connect(t, url)
	_, err = pool.Exec(ctx, `INSERT INTO public.users (id, name, phone, password, role)
		VALUES ('00000000-0000-0000-0000-0000000000a3', 'Cliente', '+244923000001', 'hash', 'customer')`)
	require.NoError(t, err)

	require.NoError(t, postgres.Migrate(url, "up"))
	pool.Reset()

	assertEverythingInAtlas(t, pool)

	var name string
	require.NoError(t, pool.QueryRow(ctx, "SELECT name FROM users WHERE phone = '+244923000001'").Scan(&name))
	assert.Equal(t, "Cliente", name)

	var version int
	var dirty bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT version, dirty FROM atlas.schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 14, version)
	assert.False(t, dirty)

	_, err = pool.Exec(ctx, "UPDATE audit_log SET reason = 'x'")
	require.ErrorContains(t, err, "append-only", "the trigger function moved with the table")

	var canCreate bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT has_schema_privilege('public', 'public', 'CREATE')").Scan(&canCreate))
	assert.False(t, canCreate, "PUBLIC cannot create objects in schema public")
}
