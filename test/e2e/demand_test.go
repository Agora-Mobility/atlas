//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

const (
	normalTaxiFare = 171_500
	demandArea     = 7
)

func zonesAtPickup(t *testing.T) []string {
	t.Helper()
	rows, err := env.owner.Query(context.Background(), `SELECT id FROM service_zones WHERE is_active AND ST_Covers(geom, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography)`, pickup.Lng, pickup.Lat)
	require.NoError(t, err)
	defer rows.Close()
	var zones []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		zones = append(zones, id)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, zones)
	return zones
}

func setDemand(t *testing.T, admin *person, zones []string, mode string) {
	t.Helper()
	for _, zone := range zones {
		res := admin.do(t, http.MethodPut, "/api/v1/admin/demand-pricing", map[string]any{
			"zone_id": zone, "category": "taxi", "mode": mode, "max_factor_bps": 13_000,
			"tiers": []any{map[string]any{"above_ratio_bps": 10_000, "factor_bps": 11_000}, map[string]any{"above_ratio_bps": 15_000, "factor_bps": 12_000}},
		}, nil)
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
}

func publishFactor(t *testing.T, zones []string, factor int) {
	t.Helper()
	area, err := h3index.New().Cell(entities.GeoPoint{Lat: pickup.Lat, Lng: pickup.Lng}, demandArea)
	require.NoError(t, err)
	for _, zone := range zones {
		require.NoError(t, env.redisDirect.HSet(context.Background(), "demand:factor:"+zone+":taxi", strconv.FormatInt(area, 10), factor).Err())
	}
}

func TestHighDemandRaisesTheClosedPriceAndShowsOnTheReceipt(t *testing.T) {
	clearOnlineProviders(t)
	admin := staffToken(t)
	zones := zonesAtPickup(t)
	t.Cleanup(func() {
		setDemand(t, admin, zones, "off")
		keys, err := env.redisDirect.Keys(context.Background(), "demand:*").Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, env.redisDirect.Del(context.Background(), keys...).Err())
		}
	})

	invalid := admin.do(t, http.MethodPut, "/api/v1/admin/demand-pricing", map[string]any{"zone_id": zones[0], "category": "taxi", "mode": "auto", "max_factor_bps": 13_000}, nil)
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, invalid.status, invalid.text)

	setDemand(t, admin, zones, "on")
	publishFactor(t, zones, 12_000)
	listed := admin.get(t, "/api/v1/admin/demand-pricing")
	require.Equal(t, http.StatusOK, listed.status, listed.text)
	assert.NotEmpty(t, listed.body["items"])

	customer := newCustomer(t)
	estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	option := estimate.body["items"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 205_800, option["total_cents"], "1.2 × the normal 171 500")
	assert.EqualValues(t, 12_000, option["demand_factor_bps"])
	assert.Equal(t, true, option["high_demand"], "the app warns before the customer confirms")

	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{
		"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target,
	}, map[string]string{"Idempotency-Key": strconv.Itoa(int(customer.id.ID()))})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, 205_800, created.num("fare_cents"), "the request keeps the quoted price")
	requestID := created.str("id")

	view := customer.get(t, "/api/v1/requests/"+requestID)
	adjustments := view.body["adjustments"].([]any)
	require.Len(t, adjustments, 1)
	assert.Equal(t, "demand", adjustments[0].(map[string]any)["reason"])
	assert.EqualValues(t, 205_800-normalTaxiFare, adjustments[0].(map[string]any)["amount_cents"])
	var commission, expected int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `
		SELECT r.commission_cents, round((e.total_cents - e.demand_cents) * t.commission_bps / 10000.0)::bigint
		FROM service_requests r JOIN fare_estimates e ON e.id = r.fare_estimate_id JOIN service_tariffs t ON t.id = e.tariff_id
		WHERE r.id = $1`, requestID).Scan(&commission, &expected))
	assert.Equal(t, expected, commission, "without commission_on_surge the whole rise goes to the driver")
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", requestID).status)

	setDemand(t, admin, zones, "observe")
	publishFactor(t, zones, 12_000)
	observed := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, observed.status, observed.text)
	plain := observed.body["items"].([]any)[0].(map[string]any)
	assert.EqualValues(t, normalTaxiFare, plain["total_cents"], "observing never changes the price")
	assert.Equal(t, false, plain["high_demand"])
	counted := 0
	for _, zone := range zones {
		members, err := env.redisDirect.ZRange(context.Background(), "demand:quotes:"+zone+":taxi", 0, -1).Result()
		require.NoError(t, err)
		for _, member := range members {
			if len(member) > 36 && member[len(member)-36:] == customer.id.String() {
				counted++
			}
		}
	}
	assert.Positive(t, counted, "the quote still counts as demand while observing")

	setDemand(t, admin, zones, "off")
	keys, err := env.redisDirect.Keys(context.Background(), "demand:factor:*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "switching off clears the factors at once")
}
