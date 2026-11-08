//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/database/postgres"
)

func TestMigrationsServeTheWholeLuandaProvinceAndNothingBeyond(t *testing.T) {
	url := startPostgres(t)
	require.NoError(t, postgres.Migrate(url, "up"))
	pool := connect(t, url)

	places := []struct {
		name     string
		lat, lng float64
		served   bool
	}{
		{"Baixa de Luanda", -8.8147, 13.2302, true},
		{"Ilha do Cabo", -8.7950, 13.2250, true},
		{"Futungo de Belas", -8.9000, 13.1780, true},
		{"Talatona", -8.9170, 13.1840, true},
		{"Kilamba", -8.9960, 13.2700, true},
		{"Viana", -8.9030, 13.3740, true},
		{"Cacuaco", -8.7760, 13.3700, true},
		{"Zango", -8.9900, 13.4300, true},
		{"Ramiros", -9.0700, 13.1300, true},
		{"Mussulo", -8.9500, 13.0900, true},
		{"Aeroporto AIAAN", -9.0400, 13.5000, true},
		{"Catete (Icolo e Bengo)", -9.1100, 13.7000, false},
		{"Cabo Ledo (Quiçama)", -9.6600, 13.2400, false},
		{"Benguela", -12.5800, 13.4000, false},
	}
	for _, place := range places {
		var zones int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM service_zones
			WHERE is_active AND name = 'Luanda' AND ST_Covers(geom, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography)`, place.lat, place.lng).Scan(&zones))
		assert.Equal(t, place.served, zones == 1, place.name)
	}
}
