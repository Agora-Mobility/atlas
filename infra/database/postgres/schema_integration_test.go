//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
)

var appTables = []string{
	"addresses", "audit_log", "block_reasons", "booking_events", "booking_extras", "booking_items", "bookings", "cancellation_rules", "catalog_items", "category_payment_methods", "clients", "customer_vehicles", "device_tokens", "dispatch_policies", "eta_observations", "fare_adjustments", "fare_estimates", "inbox_items", "invitations", "ledger_entries", "ledger_transactions", "media_uploads", "membership_roles", "memberships", "mfa_recovery_codes", "notifications", "organizations", "outbox", "payment_events", "payment_refunds", "payments", "permissions", "platform_settings", "provider_coverage", "provider_document_rejection_reasons", "provider_document_types", "provider_documents", "provider_location_samples", "provider_rejection_reasons", "providers", "ratings", "referrals", "refresh_tokens", "rejection_reasons", "request_messages", "request_offers", "request_shares", "request_team_members", "role_permissions", "roles", "schema_migrations", "service_categories", "service_provider_requirements", "service_request_events", "service_request_stops", "service_requests", "service_required_documents", "service_tariffs", "service_zones", "services", "sos_alerts", "support_messages", "support_tickets", "user_addresses", "user_block_reasons", "user_blocks", "user_mfa", "users", "vehicles", "voucher_redemptions", "vouchers", "workshop_closures", "workshop_favorites", "workshop_hours", "workshop_mechanics", "workshop_services", "workshops",
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
	ctx := context.Background()
	assert.Equal(t, appTables, tablesIn(t, pool, postgres.Schema))
	for _, name := range []string{"client_type", "user_role", "user_status", "organization_type", "membership_status"} {
		assert.Equal(t, postgres.Schema, typeSchema(t, pool, name), name)
	}
	var postgisSchema string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'postgis'").Scan(&postgisSchema))
	assert.Equal(t, postgres.Schema, postgisSchema)

	rows, err := pool.Query(ctx, "SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\\_%' AND nspname <> 'information_schema'")
	require.NoError(t, err)
	var schemas []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		schemas = append(schemas, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{postgres.Schema}, schemas, "atlas is the only schema")
}

func TestMigrate_NewDatabaseLivesInAtlas(t *testing.T) {
	url := startPostgres(t)
	require.NoError(t, postgres.Migrate(url, "up"))
	pool := connect(t, url)
	ctx := context.Background()

	assertEverythingInAtlas(t, pool)

	var clients int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM clients").Scan(&clients))
	assert.Equal(t, 4, clients, "unqualified queries resolve through the search_path")

	var version int
	var dirty bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT version, dirty FROM atlas.schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 34, version)
	assert.False(t, dirty)

	var inMeters float64
	require.NoError(t, pool.QueryRow(ctx, "SELECT ST_Distance(ST_MakePoint(13.23, -8.84)::geography, ST_MakePoint(13.24, -8.84)::geography)").Scan(&inMeters))
	assert.Greater(t, inMeters, 1000.0, "PostGIS works from the atlas schema")

	t.Run("down and up again", func(t *testing.T) {
		require.NoError(t, postgres.Migrate(url, "down"))
		require.NoError(t, postgres.Migrate(url, "up"))
		assertEverythingInAtlas(t, pool)
	})
}

func TestProviderAndAddressRules(t *testing.T) {
	url := startPostgres(t)
	require.NoError(t, postgres.Migrate(url, "up"))
	pool := connect(t, url)
	ctx := context.Background()
	const provider = "00000000-0000-0000-0000-000000000001"
	_, err := pool.Exec(ctx, `INSERT INTO users (id, name, phone, password, status, role) VALUES ($1, 'P', '+244923000001', 'x', 'active', 'provider')`, provider)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO providers (user_id) VALUES ($1)`, provider)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO vehicles (provider_id, brand, model, color, plate) VALUES ($1, 'B', 'M', 'C', 'LD-1')`, provider)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO vehicles (provider_id, brand, model, color, plate) VALUES ($1, 'B', 'M', 'C', 'LD-2')`, provider)
	assert.Error(t, err, "one provider, one vehicle")

	_, err = pool.Exec(ctx, `UPDATE providers SET status = 'approved' WHERE user_id = $1`, provider)
	assert.Error(t, err, "an approved provider has a service")

	_, err = pool.Exec(ctx, `INSERT INTO addresses (id, lat, lng) VALUES (gen_random_uuid(), 95, 13)`)
	assert.Error(t, err, "latitude out of range")

	_, err = pool.Exec(ctx, `INSERT INTO addresses (id, lat, lng) VALUES ('00000000-0000-0000-0000-0000000000f1', -8.84, 13.23), ('00000000-0000-0000-0000-0000000000f2', -8.85, 13.24)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO user_addresses (address_id, user_id, kind) VALUES ('00000000-0000-0000-0000-0000000000f1', $1, 'home')`, provider)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO user_addresses (address_id, user_id, kind) VALUES ('00000000-0000-0000-0000-0000000000f2', $1, 'home')`, provider)
	assert.Error(t, err, "one home per user")
	_, err = pool.Exec(ctx, `INSERT INTO user_addresses (address_id, user_id, kind) VALUES ('00000000-0000-0000-0000-0000000000f2', $1, 'other')`, provider)
	assert.NoError(t, err, "as many others as needed")
}
