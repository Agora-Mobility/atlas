//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/tenant"
)

// supportToken signs in a member of the Suporte role: providers:read and
// users:read, but not finance:read.
func supportToken(t *testing.T) *person {
	t.Helper()
	ctx := context.Background()
	pool := env.api.container.Pool
	org, err := repository.NewOrganizationRepository(pool).Platform(ctx)
	require.NoError(t, err)
	ctx = tenant.WithOrganization(ctx, org.ID)
	role, err := repository.NewRoleRepository(pool).FindSystemRole(ctx, enums.OrganizationTypePlatform, "support")
	require.NoError(t, err)
	require.NotNil(t, role)

	const secret = "uma-frase-longa-segura"
	hash, err := password.NewArgon2Hasher(password.DefaultParams).Hash(secret)
	require.NoError(t, err)
	email := fmt.Sprintf("suporte-%s@agora.ao", uuid.NewString()[:8])
	user, err := entities.NewUser("Suporte", email, fmt.Sprintf("+24493%07d", uuid.New().ID()%10_000_000), hash, enums.RoleStaff)
	require.NoError(t, err)
	user.MarkVerified()
	require.NoError(t, repository.NewUserRepository(pool).Create(ctx, user))
	membership, err := entities.NewActiveMembership(user.ID, org.ID, []uuid.UUID{role.ID})
	require.NoError(t, err)
	require.NoError(t, repository.NewMembershipRepository(pool).Create(ctx, membership))

	staff := &person{client: backofficeClient, api: env.api.baseURL}
	login := staff.post(t, "/api/v1/auth/login", map[string]string{"email": email, "password": secret})
	require.Equal(t, http.StatusOK, login.status, login.text)
	staff.token = login.str("access_token")
	return staff
}

func money(t *testing.T, view result, key string) map[string]any {
	t.Helper()
	value, ok := view.body[key].(map[string]any)
	require.True(t, ok, "%s in %s", key, view.text)
	return value
}

func TestTheActivityOfADriverCountsTheRidesAndWhatTheyEarned(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id, fare := completedTaxi(t, "gpo")
	commission := commissionOf(t, id)
	payExpress(t, customer, id)
	path := "/api/v1/admin/providers/" + driver.id.String() + "/activity"

	view := admin.get(t, path)
	require.Equal(t, http.StatusOK, view.status, view.text)
	assert.EqualValues(t, 1, view.num("completed_requests"))
	assert.EqualValues(t, 0, view.num("cancelled_requests"))
	assert.NotEmpty(t, view.str("last_request_at"))
	total := money(t, view, "total")
	assert.EqualValues(t, fare, total["gross_cents"], "what the customer paid")
	assert.EqualValues(t, fare-commission, total["earned_cents"], "the fare minus the commission")
	assert.EqualValues(t, fare-commission, total["balance_cents"], "still to pay to the driver")
	assert.Nil(t, view.body["period"], "no period without from or to")

	payout := admin.post(t, "/api/v1/admin/providers/"+driver.id.String()+"/settlements", map[string]any{"direction": "payout", "amount_cents": fare - commission, "reference": "TRF-ACT-1"})
	require.Equal(t, http.StatusOK, payout.status, payout.text)
	total = money(t, admin.get(t, path), "total")
	assert.EqualValues(t, fare-commission, total["earned_cents"], "paying the driver does not change what they earned")
	assert.EqualValues(t, 0, total["balance_cents"])

	future := money(t, admin.get(t, path+"?from=2999-01-01"), "period")
	assert.EqualValues(t, 0, future["earned_cents"])
	assert.EqualValues(t, 0, future["gross_cents"])
	past := money(t, admin.get(t, path+"?from=2000-01-01&to=2999-01-01"), "period")
	assert.EqualValues(t, fare-commission, past["earned_cents"])

	support := supportToken(t)
	limited := support.get(t, path)
	require.Equal(t, http.StatusOK, limited.status, limited.text)
	assert.EqualValues(t, 1, limited.num("completed_requests"))
	assert.Nil(t, limited.body["total"], "no money without finance:read")

	assert.Equal(t, http.StatusNotFound, admin.get(t, "/api/v1/admin/providers/"+uuid.NewString()+"/activity").status)
	assert.Equal(t, http.StatusBadRequest, admin.get(t, path+"?from=ontem").status)
	assert.Equal(t, http.StatusForbidden, customer.get(t, path).status)
}

func TestCashEarnsTheDriverTheFareTheyKeptMinusTheCommission(t *testing.T) {
	admin := staffToken(t)
	_, driver, id, fare := completedTaxi(t, "cash")
	commission := commissionOf(t, id)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil).status)

	total := money(t, admin.get(t, "/api/v1/admin/providers/"+driver.id.String()+"/activity"), "total")
	assert.EqualValues(t, fare, total["gross_cents"])
	assert.EqualValues(t, fare-commission, total["earned_cents"], "the driver holds the fare and owes the commission")
	assert.EqualValues(t, -commission, total["balance_cents"])

	// Without commission the ledger has no entry: the cash alone is what was earned
	_, free, freeID, freeFare := completedTaxi(t, "cash")
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET commission_cents = 0 WHERE id = $1`, freeID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, free.post(t, "/api/v1/requests/"+freeID+"/payments/cash", nil).status)
	total = money(t, admin.get(t, "/api/v1/admin/providers/"+free.id.String()+"/activity"), "total")
	assert.EqualValues(t, freeFare, total["earned_cents"])
	assert.EqualValues(t, 0, total["balance_cents"])
}

func TestTheBackofficeListsShowWhoIsWhoAndHowMuchTheyUseThePlatform(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id, fare := completedTaxi(t, "gpo")
	commission := commissionOf(t, id)
	payExpress(t, customer, id)

	accounts := admin.get(t, "/api/v1/admin/users?category=taxi&size=100")
	require.Equal(t, http.StatusOK, accounts.status, accounts.text)
	account := admin.get(t, "/api/v1/admin/users/"+driver.id.String())
	require.Equal(t, http.StatusOK, account.status, account.text)
	assert.Equal(t, "taxi", account.str("provider_category"))
	assert.NotEmpty(t, account.str("provider_category_name"))
	assert.EqualValues(t, 1, account.num("completed_requests"))
	buyer := admin.get(t, "/api/v1/admin/users/"+customer.id.String())
	assert.Empty(t, buyer.str("provider_category"), "a customer has no category")
	assert.EqualValues(t, 1, buyer.num("completed_requests"), "the ride the customer asked for")
	for _, item := range accounts.body["items"].([]any) {
		assert.Equal(t, "provider", item.(map[string]any)["role"], "category keeps only providers")
	}
	none := admin.get(t, "/api/v1/admin/users?category=removals-que-nao-existe")
	assert.EqualValues(t, 0, none.num("total"))

	rides := admin.get(t, "/api/v1/admin/requests?customer_id="+customer.id.String())
	require.Equal(t, http.StatusOK, rides.status, rides.text)
	require.EqualValues(t, 1, rides.num("total"))
	assert.Equal(t, id, rides.body["items"].([]any)[0].(map[string]any)["id"])
	assert.Equal(t, http.StatusBadRequest, admin.get(t, "/api/v1/admin/requests?customer_id=x").status)

	providers := admin.get(t, "/api/v1/admin/providers?status=approved&category=taxi&sort=earned_desc&size=100")
	require.Equal(t, http.StatusOK, providers.status, providers.text)
	var found map[string]any
	previous := int64(1 << 62)
	for _, raw := range providers.body["items"].([]any) {
		item := raw.(map[string]any)
		earned := int64(item["earned_cents"].(float64))
		assert.LessOrEqual(t, earned, previous, "the one who earned the most first")
		previous = earned
		if item["user_id"] == driver.id.String() {
			found = item
		}
	}
	require.NotNil(t, found, "the driver is in the first page")
	assert.EqualValues(t, 1, found["completed_requests"])
	assert.EqualValues(t, fare-commission, found["earned_cents"])
	assert.EqualValues(t, fare, found["gross_cents"])

	support := supportToken(t)
	limited := support.get(t, "/api/v1/admin/providers?status=approved&size=5")
	require.Equal(t, http.StatusOK, limited.status, limited.text)
	for _, raw := range limited.body["items"].([]any) {
		item := raw.(map[string]any)
		assert.NotContains(t, item, "earned_cents", "no money without finance:read")
		assert.Contains(t, item, "completed_requests")
	}
	assert.Equal(t, http.StatusForbidden, support.get(t, "/api/v1/admin/providers?sort=earned_desc").status)
	assert.Equal(t, http.StatusBadRequest, admin.get(t, "/api/v1/admin/providers?sort=nome").status)
}

func TestTheDriverSeesTheRidesTheyDidAndWhatTheyEarnedPerPeriod(t *testing.T) {
	customer, driver, id, fare := completedTaxi(t, "gpo")
	commission := commissionOf(t, id)
	payExpress(t, customer, id)

	done := driver.get(t, "/api/v1/providers/me/requests?status=completed,cancelled")
	require.Equal(t, http.StatusOK, done.status, done.text)
	assert.EqualValues(t, 1, done.num("total"))
	item := done.body["items"].([]any)[0].(map[string]any)
	assert.Equal(t, id, item["id"])
	assert.Equal(t, "completed", item["status"])
	assert.EqualValues(t, fare-commission, item["provider_net_cents"], "what the driver earned from the ride")
	assert.NotEmpty(t, item["service_name"])
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/requests?status=cancelled").num("total"))
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/requests?category=towing").num("total"))

	for _, period := range []string{"day", "week", "month"} {
		earnings := driver.get(t, "/api/v1/providers/me/earnings?period="+period)
		require.Equal(t, http.StatusOK, earnings.status, earnings.text)
		assert.Equal(t, period, earnings.str("period"))
		assert.EqualValues(t, fare-commission, earnings.num("earned_cents"), period)
		assert.EqualValues(t, fare, earnings.num("gross_cents"), period)
		assert.EqualValues(t, 1, earnings.num("completed_requests"), period)
		assert.EqualValues(t, fare-commission, earnings.num("balance_cents"), period)
		days := earnings.body["days"].([]any)
		want := map[string]int{"day": 1, "week": 7}[period]
		if want > 0 {
			assert.Len(t, days, want, period)
		}
		var earned float64
		for _, day := range days {
			earned += day.(map[string]any)["earned_cents"].(float64)
		}
		assert.EqualValues(t, fare-commission, earned, "the days add up to the period")
	}
	past := driver.get(t, "/api/v1/providers/me/earnings?period=month&date=2000-01-15")
	require.Equal(t, http.StatusOK, past.status, past.text)
	assert.EqualValues(t, 0, past.num("earned_cents"))
	assert.Len(t, past.body["days"].([]any), 31)
	assert.Equal(t, "2000-01-01", past.body["days"].([]any)[0].(map[string]any)["date"])

	assert.Equal(t, http.StatusBadRequest, driver.get(t, "/api/v1/providers/me/earnings?period=year").status)
	assert.Equal(t, http.StatusBadRequest, driver.get(t, "/api/v1/providers/me/earnings?date=ontem").status)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/providers/me/earnings").status)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/providers/me/requests").status)
}

func TestTheDriverSeesTheRidesCancelledAfterTheyTookThem(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	cancel := customer.post(t, "/api/v1/requests/"+id+"/cancel", map[string]any{"reason_id": cancelReason(t, customer, "customer", "taxi", "changed_plans")})
	require.Equal(t, http.StatusOK, cancel.status, cancel.text)

	cancelled := driver.get(t, "/api/v1/providers/me/requests?status=cancelled")
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	assert.EqualValues(t, 1, cancelled.num("total"))
	assert.Equal(t, id, cancelled.body["items"].([]any)[0].(map[string]any)["id"])
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/requests?status=completed").num("total"))

	today := driver.get(t, "/api/v1/providers/me/earnings")
	require.Equal(t, http.StatusOK, today.status, today.text)
	assert.Equal(t, "day", today.str("period"), "day by default")
	assert.EqualValues(t, 1, today.num("cancelled_requests"))
	assert.EqualValues(t, 0, today.num("completed_requests"))
}
