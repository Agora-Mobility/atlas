//go:build integration

package seeders_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/postgres/seeders"
)

func TestSeedPutsBookableWorkshopsAroundFutungoAndIsRepeatable(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgis/postgis:18-3.6-alpine",
		tcpostgres.WithDatabase("atlas_test"), tcpostgres.WithUsername("app"), tcpostgres.WithPassword("app"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(url, "up"))
	pool, err := postgres.OwnerConnection(ctx, config.DatabaseConfig{URL: url, MaxConns: 2, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, seeders.Run(ctx, pool))
	require.NoError(t, seeders.Run(ctx, pool), "running the seed again changes nothing")

	const futungo = `ST_SetSRID(ST_MakePoint(13.18, -8.90), 4326)::geography`
	var workshops, services, hours, inZone int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM workshops w JOIN addresses a ON a.id = w.address_id WHERE w.is_active AND ST_DWithin(a.geom, `+futungo+`, 3000)`).Scan(&workshops))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM workshop_services s JOIN workshops w ON w.id = s.workshop_id WHERE s.category_id IS NOT NULL AND w.name LIKE ANY (ARRAY['Oficina Futungo%', 'Mussulo%', 'Belas%'])`).Scan(&services))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM workshop_hours h JOIN workshops w ON w.id = h.workshop_id WHERE w.name LIKE ANY (ARRAY['Oficina Futungo%', 'Mussulo%', 'Belas%'])`).Scan(&hours))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM workshops w JOIN addresses a ON a.id = w.address_id JOIN service_zones z ON ST_Covers(z.geom, a.geom) WHERE z.is_active`).Scan(&inZone))
	assert.Equal(t, 3, workshops)
	assert.Equal(t, 12, services, "each workshop offers the four categories")
	assert.Equal(t, 21, hours, "open every day")
	assert.Equal(t, 3, inZone, "inside the Luanda service area, so booking does not answer OUTSIDE_SERVICE_AREA")
}
