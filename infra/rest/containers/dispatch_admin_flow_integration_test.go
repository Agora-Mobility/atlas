//go:build integration

package containers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const benguelaGeometry = `{"type":"MultiPolygon","coordinates":[[[[13.3,-12.7],[13.5,-12.7],[13.5,-12.5],[13.3,-12.5],[13.3,-12.7]]]]}`

func TestDispatchBackofficeFlow(t *testing.T) {
	pool, owner, rdb := setupStores(t)
	a := api{t: t, engine: newTestEngine(t, pool, rdb, &smsInbox{last: map[string]string{}}, &emailOutbox{last: map[string]services.Email{}})}
	ctx := context.Background()
	adminToken := signInSuperAdmin(t, a, pool)
	var zoneID, serviceID string

	t.Run("the routes need the permission", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, a.call(http.MethodGet, "/api/v1/admin/zones", backofficeApp, "", nil).status)
	})

	t.Run("a zone carries its currency and time zone and is validated", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/admin/zones", backofficeApp, adminToken, map[string]any{
			"country_code": "ao", "name": "Benguela", "timezone": "Africa/Luanda", "currency": "aoa", "geometry": rawJSON(benguelaGeometry),
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		zoneID = res.body["id"].(string)
		assert.Equal(t, "AO", res.body["country_code"])
		assert.Equal(t, "AOA", res.body["currency"])

		bad := a.call(http.MethodPost, "/api/v1/admin/zones", backofficeApp, adminToken, map[string]any{
			"country_code": "AO", "name": "X", "timezone": "Lua/Nada", "currency": "AOA", "geometry": rawJSON(benguelaGeometry),
		})
		assert.Equal(t, http.StatusUnprocessableEntity, bad.status)
		bad = a.call(http.MethodPost, "/api/v1/admin/zones", backofficeApp, adminToken, map[string]any{
			"country_code": "AO", "name": "X", "timezone": "Africa/Luanda", "currency": "AOA", "geometry": rawJSON(`{"type":"Nonsense"}`),
		})
		assert.Equal(t, http.StatusUnprocessableEntity, bad.status)

		list := a.call(http.MethodGet, "/api/v1/admin/zones", backofficeApp, adminToken, nil)
		assert.Len(t, list.body["items"], 1)

		updated := a.call(http.MethodPut, "/api/v1/admin/zones/"+zoneID, backofficeApp, adminToken, map[string]any{
			"country_code": "AO", "name": "Benguela Cidade", "timezone": "Africa/Luanda", "currency": "AOA", "is_active": true, "geometry": rawJSON(benguelaGeometry),
		})
		require.Equal(t, http.StatusOK, updated.status, updated.body)
		assert.Equal(t, "Benguela Cidade", updated.body["name"])
	})

	t.Run("policies come seeded and bad ones are refused", func(t *testing.T) {
		res := a.call(http.MethodGet, "/api/v1/admin/dispatch-policies", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		items := res.body["items"].([]any)
		require.Len(t, items, 4)
		taxi := items[0].(map[string]any)
		for _, candidate := range items {
			if candidate.(map[string]any)["offer_mode"] == "sequential" {
				taxi = candidate.(map[string]any)
			}
		}
		policyID := taxi["id"].(string)

		taxi["radii_km"] = []any{5000}
		wide := a.call(http.MethodPut, "/api/v1/admin/dispatch-policies/"+policyID, backofficeApp, adminToken, taxi)
		assert.Equal(t, http.StatusUnprocessableEntity, wide.status, "a search with too many cells is refused")

		taxi["radii_km"] = []any{2, 4}
		ok := a.call(http.MethodPut, "/api/v1/admin/dispatch-policies/"+policyID, backofficeApp, adminToken, taxi)
		require.Equal(t, http.StatusOK, ok.status, ok.body)
		assert.Equal(t, []any{2.0, 4.0}, ok.body["radii_km"])

		taxi["zone_id"] = zoneID
		taxi["radii_km"] = []any{3}
		zonal := a.call(http.MethodPost, "/api/v1/admin/dispatch-policies", backofficeApp, adminToken, taxi)
		require.Equal(t, http.StatusCreated, zonal.status, zonal.body)
		assert.Equal(t, zoneID, zonal.body["zone_id"])
	})

	t.Run("a tariff change creates a new version", func(t *testing.T) {
		found := a.call(http.MethodGet, "/api/v1/admin/services?search=t%C3%A1xi", backofficeApp, adminToken, nil)
		serviceID = found.body["items"].([]any)[0].(map[string]any)["id"].(string)
		first := a.call(http.MethodPost, "/api/v1/admin/tariffs", backofficeApp, adminToken, map[string]any{
			"service_id": serviceID, "currency": "AOA", "base_fee_cents": 30_000, "per_km_fee_cents": 13_000, "per_minute_fee_cents": 1_500, "minimum_fee_cents": 45_000, "commission_bps": 0,
		})
		require.Equal(t, http.StatusCreated, first.status, first.body)
		second := a.call(http.MethodPost, "/api/v1/admin/tariffs", backofficeApp, adminToken, map[string]any{
			"service_id": serviceID, "currency": "AOA", "base_fee_cents": 35_000, "per_km_fee_cents": 13_000, "per_minute_fee_cents": 1_500, "minimum_fee_cents": 45_000, "commission_bps": 1000,
		})
		require.Equal(t, http.StatusCreated, second.status, second.body)
		assert.NotEqual(t, first.body["id"], second.body["id"])

		current := a.call(http.MethodGet, "/api/v1/admin/tariffs", backofficeApp, adminToken, nil)
		require.Len(t, current.body["items"], 1)
		var versions int
		require.NoError(t, owner.QueryRow(ctx, `SELECT count(*) FROM service_tariffs`).Scan(&versions))
		assert.Equal(t, 2, versions, "the history explains every price")

		invalid := a.call(http.MethodPost, "/api/v1/admin/tariffs", backofficeApp, adminToken, map[string]any{"service_id": serviceID, "currency": "AOA", "commission_bps": 20_000})
		assert.Equal(t, http.StatusUnprocessableEntity, invalid.status)
	})

	t.Run("only provider accounts get a dispatch profile", func(t *testing.T) {
		var customerID, providerID string
		require.NoError(t, owner.QueryRow(ctx, `INSERT INTO users (id, name, phone, password, is_verified, status, role) VALUES (gen_random_uuid(), 'C', '+244900000001', 'x', true, 'active', 'customer') RETURNING id`).Scan(&customerID))
		require.NoError(t, owner.QueryRow(ctx, `INSERT INTO users (id, name, phone, password, is_verified, status, role) VALUES (gen_random_uuid(), 'P', '+244900000002', 'x', true, 'active', 'provider') RETURNING id`).Scan(&providerID))

		res := a.call(http.MethodPut, "/api/v1/admin/providers/"+customerID, backofficeApp, adminToken, map[string]any{"status": "approved"})
		assert.Equal(t, "PROVIDER_NOT_ELIGIBLE", res.code())
		res = a.call(http.MethodPut, "/api/v1/admin/providers/"+providerID, backofficeApp, adminToken, map[string]any{"status": "approved"})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, "an approved provider must have one service")
		res = a.call(http.MethodPut, "/api/v1/admin/providers/"+providerID, backofficeApp, adminToken, map[string]any{"status": "approved", "service_ids": []string{serviceID, serviceID}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, "a list of services does not count as the one service")
		res = a.call(http.MethodPut, "/api/v1/admin/providers/0192f1d2-7c3a-7b4e-9a10-2f3c4d5e6f70", backofficeApp, adminToken, map[string]any{"status": "approved"})
		assert.Equal(t, "PROVIDER_NOT_FOUND", res.code())

		res = a.call(http.MethodPut, "/api/v1/admin/providers/"+providerID, backofficeApp, adminToken, map[string]any{
			"status": "approved", "vehicle_plate": "LD-45-21-AF", "vehicle_description": "Toyota Corolla", "service_id": serviceID,
		})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, "approved", res.body["status"])

		res = a.call(http.MethodPut, "/api/v1/admin/providers/"+providerID, backofficeApp, adminToken, map[string]any{"status": "banned"})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	})

	t.Run("the operator sees the queue and supply and demand", func(t *testing.T) {
		queue := a.call(http.MethodGet, "/api/v1/admin/dispatch/queue", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, queue.status, queue.body)
		assert.Empty(t, queue.body["items"])

		heat := a.call(http.MethodGet, "/api/v1/admin/dispatch/heatmap", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, heat.status, heat.body)
		assert.EqualValues(t, 8, heat.body["resolution"])

		missing := a.call(http.MethodGet, "/api/v1/admin/dispatch/requests/0192f1d2-7c3a-7b4e-9a10-2f3c4d5e6f70", backofficeApp, adminToken, nil)
		assert.Equal(t, "REQUEST_NOT_FOUND", missing.code())
	})

	t.Run("every change is audited", func(t *testing.T) {
		var audited int
		require.NoError(t, owner.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type IN ('service_zone', 'dispatch_policy', 'service_tariff', 'provider_profile')`).Scan(&audited))
		assert.Equal(t, 7, audited, "zone twice, policy twice, tariff twice, provider once")
	})
}

func rawJSON(value string) json.RawMessage {
	return json.RawMessage(value)
}
