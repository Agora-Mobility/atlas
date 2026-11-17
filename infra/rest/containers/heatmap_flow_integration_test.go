//go:build integration

package containers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

const luandaBox = "13.15,-8.95,13.35,-8.75"

func TestHeatmapCellsFlow(t *testing.T) {
	pool, owner, rdb := setupStores(t)
	a := api{t: t, engine: newTestEngine(t, pool, rdb, &smsInbox{last: map[string]string{}}, &emailOutbox{last: map[string]services.Email{}})}
	ctx := context.Background()
	adminToken := signInSuperAdmin(t, a, pool)
	geo := h3index.New()
	center := entities.GeoPoint{Lat: -8.8390, Lng: 13.2894}
	current := time.Now().UTC().Truncate(15 * time.Minute)
	last, before := current.Add(-15*time.Minute), current.Add(-30*time.Minute)
	for _, resolution := range []int{9, 8} {
		cell, err := geo.Cell(center, resolution)
		require.NoError(t, err)
		for _, window := range []time.Time{last, before} {
			_, err = owner.Exec(ctx, `INSERT INTO heatmap_cells (window_start, resolution, category_slug, h3, count, value, updated_at) VALUES ($1, $2, 'taxi', $3, 4, 2.5, now())`, window, resolution, cell)
			require.NoError(t, err)
		}
	}
	path := func(query string) string { return "/api/v1/admin/dispatch/heatmap/cells?" + query }

	t.Run("the route needs the permission", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, a.call(http.MethodGet, path("bbox="+luandaBox+"&zoom=13"), backofficeApp, "", nil).status)
	})

	t.Run("a valid view gives the cells of the last window", func(t *testing.T) {
		res := a.call(http.MethodGet, path("bbox="+luandaBox+"&zoom=13"), backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 8, res.body["resolution"])
		assert.Equal(t, "value", res.body["metric"])
		assert.InDelta(t, 2.5, res.body["max"], 1e-9)
		window := res.body["window"].(map[string]any)
		assert.Equal(t, last.Format(time.RFC3339), window["from"])
		assert.Equal(t, current.Format(time.RFC3339), window["to"])
		assert.Equal(t, []any{map[string]any{"h3": "88831e395dfffff", "value": 2.5}}, res.body["cells"])
	})

	t.Run("the windows of the period are summed", func(t *testing.T) {
		res := a.call(http.MethodGet, path("bbox="+luandaBox+"&zoom=13&metric=count&from="+before.Format(time.RFC3339)+"&to="+current.Format(time.RFC3339)), backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, []any{map[string]any{"h3": "88831e395dfffff", "value": float64(8)}}, res.body["cells"])
	})

	t.Run("a closer zoom changes the resolution", func(t *testing.T) {
		res := a.call(http.MethodGet, path("bbox="+luandaBox+"&zoom=16"), backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 9, res.body["resolution"])
		assert.Equal(t, []any{map[string]any{"h3": "89831e395d3ffff", "value": 2.5}}, res.body["cells"])

		far := a.call(http.MethodGet, path("bbox="+luandaBox+"&zoom=11"), backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, far.status, far.body)
		assert.EqualValues(t, 7, far.body["resolution"])
		assert.Empty(t, far.body["cells"])
	})

	t.Run("an invalid query is a 400 with a readable message", func(t *testing.T) {
		for query, code := range map[string]string{
			"zoom=13":                                   "INVALID_BBOX",
			"bbox=13.15,-8.95,13.35&zoom=13":            "INVALID_BBOX",
			"bbox=a,b,c,d&zoom=13":                      "INVALID_BBOX",
			"bbox=13.15,-95,13.35,-8.75&zoom=13":        "INVALID_BBOX",
			"bbox=13.35,-8.95,13.15,-8.75&zoom=13":      "INVALID_BBOX",
			"bbox=" + luandaBox:                         "INVALID_ZOOM",
			"bbox=" + luandaBox + "&zoom=30":            "INVALID_ZOOM",
			"bbox=" + luandaBox + "&zoom=13&metric=sum": "INVALID_METRIC",
			"bbox=" + luandaBox + "&zoom=13&from=2026-01-01T00:00:00Z&to=2026-01-09T00:00:00Z": "INVALID_PERIOD",
			"bbox=11,-18,24,-4&zoom=13": "AREA_TOO_LARGE",
		} {
			res := a.call(http.MethodGet, path(query), backofficeApp, adminToken, nil)
			assert.Equal(t, http.StatusBadRequest, res.status, query)
			assert.Equal(t, code, res.code(), query)
		}
	})
}
