//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func categoryID(t *testing.T, slug string) string {
	t.Helper()
	var id string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id FROM service_categories WHERE slug = $1`, slug).Scan(&id))
	return id
}

func newVoucher(t *testing.T, extra map[string]any) string {
	t.Helper()
	code := fmt.Sprintf("QA%d", uuid.New().ID())
	body := map[string]any{"code": code, "discount_bps": 1000, "valid_from": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "valid_until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "per_customer_limit": 1}
	for key, value := range extra {
		body[key] = value
	}
	res := staffToken(t).post(t, "/api/v1/admin/vouchers", body)
	require.Equal(t, http.StatusCreated, res.status, res.text)
	return code
}

func TestAVoucherOnlyWorksWithinItsRules(t *testing.T) {
	clearOnlineProviders(t)
	admin := staffToken(t)
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	fare := int64(option["total_cents"].(float64))

	assert.Equal(t, http.StatusForbidden, customer.post(t, "/api/v1/admin/vouchers", map[string]any{"code": "HACK", "discount_bps": 10000}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/vouchers", map[string]any{"code": "BOTH1", "discount_bps": 1000, "discount_cents": 100, "valid_from": "2026-01-01T00:00:00Z", "valid_until": "2030-01-01T00:00:00Z"}).status, "a percentage or a value")
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/vouchers", map[string]any{"code": "BIG1", "discount_bps": 99_999_999_999, "valid_from": "2026-01-01T00:00:00Z", "valid_until": "2030-01-01T00:00:00Z"}).status)

	expired := newVoucher(t, map[string]any{"valid_from": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339), "valid_until": time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)})
	wrongService := newVoucher(t, map[string]any{"category_id": categoryID(t, "towing")})
	minimum := newVoucher(t, map[string]any{"min_fare_cents": fare + 1})

	for code, want := range map[string]string{"NOPE": "VOUCHER_NOT_FOUND", expired: "VOUCHER_EXPIRED", wrongService: "VOUCHER_NOT_APPLICABLE", minimum: "VOUCHER_BELOW_MINIMUM"} {
		res := askFor(t, customer, option, map[string]any{"voucher_code": code})
		assert.Equal(t, want, res.code(), "%s: %s", code, res.text)
		preview := customer.post(t, "/api/v1/vouchers/preview", map[string]any{"code": code, "fare_estimate_id": option["fare_estimate_id"]})
		assert.Equal(t, want, preview.code(), "preview of %s", code)
	}

	good := newVoucher(t, map[string]any{"category_id": categoryID(t, "taxi"), "is_public": true})
	assert.Contains(t, customer.get(t, "/api/v1/vouchers?category=taxi").text, good, "public vouchers are listed")
	preview := customer.post(t, "/api/v1/vouchers/preview", map[string]any{"code": good, "fare_estimate_id": option["fare_estimate_id"]})
	require.Equal(t, http.StatusOK, preview.status, preview.text)
	discount := preview.num("discount_cents")
	assert.InDelta(t, fare/10, discount, 1)
	other := newCustomer(t)
	assert.Equal(t, "FARE_ESTIMATE_INVALID", other.post(t, "/api/v1/vouchers/preview", map[string]any{"code": good, "fare_estimate_id": option["fare_estimate_id"]}).code(), "the price of another customer")

	created := askFor(t, customer, option, map[string]any{"voucher_code": good, "discount_cents": 99_999})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, fare, created.num("fare_cents"), "the fare keeps its value")
	assert.EqualValues(t, discount, created.num("discount_cents"), "the discount in the payload is ignored")
	assert.EqualValues(t, fare-discount, created.num("amount_due_cents"))
	id := created.str("id")

	again := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)})
	assert.Equal(t, "VOUCHER_USED", askFor(t, customer, again, map[string]any{"stops": pointsOf(stopA), "voucher_code": good}).code(), "once per customer")

	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", id).status)
	third := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopB)})
	reused := askFor(t, customer, third, map[string]any{"stops": pointsOf(stopB), "voucher_code": good})
	assert.Equal(t, http.StatusCreated, reused.status, "cancelling gives it back: %s", reused.text)
}

func TestAVoucherIsPaidByAgoraAndAppearsInTheTotal(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	code := newVoucher(t, nil)
	created := askFor(t, customer, option, map[string]any{"voucher_code": code, "payment_method": "gpo"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status)
	}
	fare, discount := created.num("fare_cents"), created.num("discount_cents")
	commission := commissionOf(t, id)
	require.Positive(t, discount)

	paid := payExpress(t, customer, id)
	assert.EqualValues(t, fare-discount, paid.num("amount_cents"), "the customer pays the total with the discount")
	assert.EqualValues(t, fare-commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "the driver earns as if there was no voucher")
	assertLedgerBalanced(t)

	view := customer.get(t, "/api/v1/requests/"+id)
	breakdown := view.body["breakdown"].(map[string]any)
	assert.EqualValues(t, fare-discount, breakdown["total_cents"])
	assert.Equal(t, code, breakdown["voucher_code"])
	sum := 0.0
	for _, item := range breakdown["items"].([]any) {
		sum += item.(map[string]any)["amount_cents"].(float64)
	}
	assert.EqualValues(t, fare-discount, sum, "the voucher line is in the total")
}

func TestAVoucherWithALimitServesOnlyThatManyAtTheSameTime(t *testing.T) {
	clearOnlineProviders(t)
	code := newVoucher(t, map[string]any{"max_redemptions": 1})
	type attempt struct {
		customer *person
		option   map[string]any
	}
	attempts := make([]attempt, 4)
	for i := range attempts {
		customer := newCustomer(t)
		attempts[i] = attempt{customer, taxiQuote(t, customer, nil)}
	}
	var wg sync.WaitGroup
	statuses := make([]int, len(attempts))
	for i := range attempts {
		wg.Go(func() {
			statuses[i] = askFor(t, attempts[i].customer, attempts[i].option, map[string]any{"voucher_code": code}).status
		})
	}
	wg.Wait()
	created := 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			created++
		}
	}
	assert.Equal(t, 1, created, "the limit holds under concurrency: %v", statuses)
}
