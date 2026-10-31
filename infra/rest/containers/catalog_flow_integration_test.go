//go:build integration

package containers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

// signInSuperAdmin bootstraps the first super admin and returns a session
// after the MFA setup.
func signInSuperAdmin(t *testing.T, a api, pool *pgxpool.Pool) string {
	t.Helper()
	bootstrap := usecases.NewAdminBootstrap(repository.NewUserRepository(pool), repository.NewOrganizationRepository(pool),
		repository.NewRoleRepository(pool), repository.NewMembershipRepository(pool), outbox.NewRepository(pool),
		repository.NewAuditLogRepository(pool), postgres.NewTxManager(pool), password.NewArgon2Hasher(password.DefaultParams))
	_, err := bootstrap.Run(context.Background(), "Admin", adminEmail, adminPassword)
	require.NoError(t, err)

	res := a.call(http.MethodPost, "/api/v1/auth/login", backofficeApp, "", map[string]string{"identifier": adminEmail, "password": adminPassword})
	require.Equal(t, http.StatusAccepted, res.status, res.body)
	mfaToken := res.body["mfa_token"].(string)
	return a.verifyMFA(backofficeApp, mfaToken, a.setupMFA(mfaToken), time.Now())
}

// catalogSlugs returns the category slugs of the public catalog and the
// service slugs of each.
func (a api) catalogSlugs() ([]string, map[string][]string) {
	a.t.Helper()
	res := a.call(http.MethodGet, "/api/v1/catalog", customerApp, "", nil)
	require.Equal(a.t, http.StatusOK, res.status, res.body)
	var categories []string
	offered := map[string][]string{}
	for _, raw := range res.body["categories"].([]any) {
		c := raw.(map[string]any)
		slug := c["slug"].(string)
		categories = append(categories, slug)
		for _, s := range c["services"].([]any) {
			offered[slug] = append(offered[slug], s.(map[string]any)["slug"].(string))
		}
	}
	return categories, offered
}

func TestCatalogFlow(t *testing.T) {
	pool, owner, rdb := setupStores(t)
	a := api{t: t, engine: newTestEngine(t, pool, rdb, &smsInbox{last: map[string]string{}}, &emailOutbox{last: map[string]services.Email{}})}
	ctx := context.Background()
	var adminToken, taxiID, cleaningID, cleaningServiceID string

	t.Run("the app lists the four MVP categories before sign-in", func(t *testing.T) {
		categories, offered := a.catalogSlugs()

		assert.Equal(t, []string{"taxi", "maintenance", "towing", "removals"}, categories)
		assert.Equal(t, map[string][]string{
			"taxi": {"taxi"}, "maintenance": {"maintenance"}, "towing": {"towing"}, "removals": {"removals"},
		}, offered)

		res := a.call(http.MethodGet, "/api/v1/catalog", customerApp, "", nil)
		maintenance := res.body["categories"].([]any)[1].(map[string]any)["services"].([]any)[0].(map[string]any)
		assert.Equal(t, false, maintenance["requires_destination"], "maintenance only needs the location")
		assert.Equal(t, map[string]any{"base_fee_cents": 0.0, "per_km_fee_cents": 0.0, "per_minute_fee_cents": 0.0}, maintenance["pricing"])

		assert.Equal(t, "MISSING_CLIENT", a.call(http.MethodGet, "/api/v1/catalog", "", "", nil).code())
	})

	t.Run("the backoffice routes need a session with the permission", func(t *testing.T) {
		res := a.call(http.MethodGet, "/api/v1/admin/categories", backofficeApp, "", nil)
		assert.Equal(t, http.StatusUnauthorized, res.status)

		adminToken = signInSuperAdmin(t, a, pool)
		res = a.call(http.MethodGet, "/api/v1/admin/categories", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		items := res.body["items"].([]any)
		require.Len(t, items, 4)
		taxi := items[0].(map[string]any)
		assert.Equal(t, "taxi", taxi["slug"])
		taxiID = taxi["id"].(string)
	})

	t.Run("a new category reaches the app once it has an active service", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/admin/categories", backofficeApp, adminToken, map[string]any{
			"slug": "cleaning", "name": "Limpeza", "icon": "broom", "display_order": 5,
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		cleaningID = res.body["id"].(string)
		categories, _ := a.catalogSlugs()
		assert.NotContains(t, categories, "cleaning", "nothing can be requested from an empty category")

		res = a.call(http.MethodPost, "/api/v1/admin/services", backofficeApp, adminToken, map[string]any{
			"category_id": cleaningID, "slug": "home-cleaning", "name": "Limpeza de casa", "requires_destination": false,
			"pricing": map[string]any{"base_fee_cents": 500_000},
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		cleaningServiceID = res.body["id"].(string)
		assert.Equal(t, true, res.body["is_active"])

		categories, offered := a.catalogSlugs()
		assert.Equal(t, "cleaning", categories[len(categories)-1], "the cached catalog was dropped")
		assert.Equal(t, []string{"home-cleaning"}, offered["cleaning"])
	})

	t.Run("invalid changes are rejected", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/admin/categories", backofficeApp, adminToken, map[string]any{"slug": "taxi", "name": "Outro táxi"})
		assert.Equal(t, "CATEGORY_SLUG_TAKEN", res.code())

		res = a.call(http.MethodPost, "/api/v1/admin/services", backofficeApp, adminToken, map[string]any{
			"category_id": taxiID, "slug": "taxi", "name": "Táxi", "requires_destination": true,
		})
		assert.Equal(t, "SERVICE_SLUG_TAKEN", res.code())

		res = a.call(http.MethodPost, "/api/v1/admin/services", backofficeApp, adminToken, map[string]any{
			"category_id": "0192f1d2-7c3a-7b4e-9a10-2f3c4d5e6f70", "slug": "ghost", "name": "Fantasma", "requires_destination": true,
		})
		assert.Equal(t, "UNKNOWN_CATEGORY", res.code())

		res = a.call(http.MethodPatch, "/api/v1/admin/services/"+cleaningServiceID, backofficeApp, adminToken, map[string]any{
			"pricing": map[string]any{"base_fee_cents": -1},
		})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, res.body)

		res = a.call(http.MethodPost, "/api/v1/admin/services", backofficeApp, adminToken, map[string]any{
			"category_id": taxiID, "slug": "no-destination-flag", "name": "Sem destino",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, "requires_destination must be explicit")

		assert.Equal(t, "CATEGORY_NOT_FOUND", a.call(http.MethodGet, "/api/v1/admin/categories/nope", backofficeApp, adminToken, nil).code())
		assert.Equal(t, "SERVICE_NOT_FOUND", a.call(http.MethodGet, "/api/v1/admin/services/0192f1d2-7c3a-7b4e-9a10-2f3c4d5e6f70", backofficeApp, adminToken, nil).code())
	})

	t.Run("deactivating a category hides it and its services from the app", func(t *testing.T) {
		res := a.call(http.MethodPatch, "/api/v1/admin/categories/"+taxiID, backofficeApp, adminToken, map[string]any{"is_active": false})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, "taxi", res.body["slug"])
		assert.Equal(t, "Táxi", res.body["name"], "fields not sent are kept")

		categories, _ := a.catalogSlugs()
		assert.NotContains(t, categories, "taxi")

		res = a.call(http.MethodGet, "/api/v1/admin/services?is_active=true&category_id="+taxiID, backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 1, res.body["total"], "the service itself stays active for when the category returns")

		res = a.call(http.MethodPatch, "/api/v1/admin/categories/"+taxiID, backofficeApp, adminToken, map[string]any{"is_active": true})
		require.Equal(t, http.StatusOK, res.status, res.body)
		categories, _ = a.catalogSlugs()
		assert.Equal(t, "taxi", categories[0])
	})

	t.Run("the tariff set in the backoffice reaches the app", func(t *testing.T) {
		res := a.call(http.MethodGet, "/api/v1/admin/services?search=t%C3%A1xi", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		require.EqualValues(t, 1, res.body["total"], res.body)
		taxiServiceID := res.body["items"].([]any)[0].(map[string]any)["id"].(string)

		res = a.call(http.MethodPatch, "/api/v1/admin/services/"+taxiServiceID, backofficeApp, adminToken, map[string]any{
			"pricing": map[string]any{"base_fee_cents": 150_000, "per_km_fee_cents": 20_000, "per_minute_fee_cents": 1_000},
		})
		require.Equal(t, http.StatusOK, res.status, res.body)

		res = a.call(http.MethodGet, "/api/v1/catalog", customerApp, "", nil)
		taxi := res.body["categories"].([]any)[0].(map[string]any)["services"].([]any)[0].(map[string]any)
		assert.Equal(t, map[string]any{"base_fee_cents": 150_000.0, "per_km_fee_cents": 20_000.0, "per_minute_fee_cents": 1_000.0}, taxi["pricing"])

		res = a.call(http.MethodPatch, "/api/v1/admin/services/"+cleaningServiceID, backofficeApp, adminToken, map[string]any{"is_active": false})
		require.Equal(t, http.StatusOK, res.status, res.body)
		categories, _ := a.catalogSlugs()
		assert.NotContains(t, categories, "cleaning", "the category has no active service left")
	})

	t.Run("every change is audited and reaches the outbox", func(t *testing.T) {
		var audited, published int
		require.NoError(t, owner.QueryRow(ctx,
			"SELECT count(*) FROM audit_log WHERE entity_type IN ('service_category', 'service')").Scan(&audited))
		require.NoError(t, owner.QueryRow(ctx,
			"SELECT count(*) FROM outbox WHERE event_type LIKE 'catalog.%'").Scan(&published))
		// cleaning created, its service created, taxi off and on, taxi tariff, cleaning service off.
		assert.Equal(t, 6, audited)
		assert.Equal(t, 6, published)
	})
}
