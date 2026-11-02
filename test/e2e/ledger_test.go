//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func completedTaxi(t *testing.T, method string) (customer, driver *person, requestID string, fare int64) {
	t.Helper()
	clearOnlineProviders(t)
	driver = newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer = newCustomer(t)
	option := taxiQuote(t, customer, nil)
	created := askFor(t, customer, option, map[string]any{"payment_method": method})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	requestID = created.str("id")
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+requestID+"/"+step, nil).status, step)
	}
	return customer, driver, requestID, customer.get(t, "/api/v1/requests/"+requestID).num("fare_cents")
}

func commissionOf(t *testing.T, requestID string) int64 {
	t.Helper()
	var commission int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT commission_cents FROM service_requests WHERE id = $1`, requestID).Scan(&commission))
	require.Positive(t, commission)
	return commission
}

func payExpress(t *testing.T, customer *person, requestID string) result {
	t.Helper()
	paid := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]any{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, paid.status, paid.text)
	env.appypay.settle(paid.str("code"), "Success", 0)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+requestID).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	return paid
}

func assertLedgerBalanced(t *testing.T) {
	t.Helper()
	summary := staffToken(t).get(t, "/api/v1/admin/ledger/summary")
	require.Equal(t, http.StatusOK, summary.status, summary.text)
	assert.EqualValues(t, 0, summary.num("unbalanced_transactions"), "every transaction adds up to zero")
}

func TestADigitalPaymentEarnsTheDriverTheFareMinusTheCommission(t *testing.T) {
	customer, driver, id, fare := completedTaxi(t, "gpo")
	commission := commissionOf(t, id)
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "nothing is earned before it is paid")

	payment := payExpress(t, customer, id)

	statement := driver.get(t, "/api/v1/providers/me/ledger")
	require.Equal(t, http.StatusOK, statement.status, statement.text)
	assert.EqualValues(t, fare-commission, statement.num("balance_cents"))
	lines := statement.body["items"].([]any)
	require.Len(t, lines, 1)
	assert.Equal(t, "fare_payment", lines[0].(map[string]any)["kind"])
	assertLedgerBalanced(t)

	env.appypay.settle(payment.str("code"), "Success", 0)
	require.NoError(t, env.api.container.Payments.Usecase.OnWebhook(context.Background(), "", payment.str("code")))
	assert.EqualValues(t, fare-commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "a second confirmation is not a second earning")

	view := customer.get(t, "/api/v1/requests/"+id)
	breakdown := view.body["breakdown"].(map[string]any)
	assert.EqualValues(t, fare, breakdown["total_cents"])
	assert.EqualValues(t, fare, breakdown["paid_cents"])
	assert.Equal(t, "gpo", breakdown["method"])
	sum := 0.0
	for _, item := range breakdown["items"].([]any) {
		sum += item.(map[string]any)["amount_cents"].(float64)
	}
	assert.EqualValues(t, fare, sum, "the lines add up to the total")
}

func TestCashLeavesTheCommissionAsADebtToSettle(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id, _ := completedTaxi(t, "cash")
	commission := commissionOf(t, id)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil).status)

	assert.EqualValues(t, -commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "the driver holds the money and owes the commission")
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil).status)
	assert.EqualValues(t, -commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "confirming again does not charge again")
	assertLedgerBalanced(t)

	path := "/api/v1/admin/providers/" + driver.id.String()
	assert.Equal(t, http.StatusOK, admin.get(t, path+"/ledger").status)
	assert.Equal(t, "SETTLEMENT_TOO_LARGE", admin.post(t, path+"/settlements", map[string]any{"direction": "payout", "amount_cents": 100, "reference": "TRF-1"}).code(), "there is nothing to pay")
	assert.Equal(t, "SETTLEMENT_TOO_LARGE", admin.post(t, path+"/settlements", map[string]any{"direction": "collect", "amount_cents": commission + 1, "reference": "TRF-1"}).code(), "above the debt")
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, path+"/settlements", map[string]any{"direction": "collect", "amount_cents": 99_999_999_999_999, "reference": "TRF-1"}).status)

	first := admin.post(t, path+"/settlements", map[string]any{"direction": "collect", "amount_cents": commission, "reference": "TRF-1"})
	require.Equal(t, http.StatusOK, first.status, first.text)
	assert.Equal(t, true, first.body["applied"])
	assert.EqualValues(t, 0, first.num("balance_cents"))

	second := admin.post(t, path+"/settlements", map[string]any{"direction": "collect", "amount_cents": commission, "reference": "TRF-1"})
	assert.Equal(t, "SETTLEMENT_TOO_LARGE", second.code(), "without a debt left the same document cannot count again")
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"))

	assert.Equal(t, http.StatusForbidden, customer.get(t, path+"/ledger").status)
	assert.Equal(t, http.StatusForbidden, driver.post(t, path+"/settlements", map[string]any{"direction": "payout", "amount_cents": 1, "reference": "X"}).status)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/providers/me/ledger").status, "a customer has no statement")
	assert.Equal(t, http.StatusForbidden, driver.get(t, "/api/v1/wallet").status, "a provider has no wallet")
	assert.Equal(t, http.StatusNotFound, admin.post(t, "/api/v1/admin/providers/"+uuid.NewString()+"/settlements", map[string]any{"direction": "payout", "amount_cents": 1, "reference": "X"}).status)
}

func TestARefundedReferenceGoesToTheWalletAndTheWalletPays(t *testing.T) {
	admin := staffToken(t)
	_, err := env.owner.Exec(context.Background(), `UPDATE category_payment_methods SET is_enabled = true WHERE method = 'reference'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.owner.Exec(context.Background(), `UPDATE category_payment_methods SET is_enabled = false WHERE method = 'reference' AND category_id IN (SELECT id FROM service_categories WHERE slug IN ('taxi', 'towing'))`)
	})
	customer, driver, id, fare := completedTaxi(t, "reference")
	commission := commissionOf(t, id)
	paid := customer.post(t, "/api/v1/requests/"+id+"/payments", map[string]any{"method": "reference"})
	require.Equal(t, http.StatusCreated, paid.status, paid.text)
	env.appypay.settle(paid.str("code"), "Success", 0)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	assert.EqualValues(t, fare-commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"))

	refundsBefore := env.appypay.refunds.Load()
	refund := admin.post(t, "/api/v1/admin/payments/"+paid.str("id")+"/refund", map[string]any{"amount_cents": fare, "reason": "Serviço com problema"})
	require.Equal(t, http.StatusOK, refund.status, refund.text)
	assert.Equal(t, refundsBefore, env.appypay.refunds.Load(), "AppyPay only refunds Express, so the gateway is not called")
	wallet := customer.get(t, "/api/v1/wallet")
	require.Equal(t, http.StatusOK, wallet.status, wallet.text)
	assert.EqualValues(t, fare, wallet.num("balance_cents"))
	assert.EqualValues(t, 0, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "the refund takes the earning back")
	assert.Equal(t, "REFUND_TOO_LARGE", admin.post(t, "/api/v1/admin/payments/"+paid.str("id")+"/refund", map[string]any{"amount_cents": 1, "reason": "De novo"}).code())
	assertLedgerBalanced(t)

	stream := driver.listen(t, "")
	second := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)})
	created := askFor(t, customer, second, map[string]any{"stops": pointsOf(stopA), "payment_method": "wallet"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	secondID := created.str("id")
	offer := stream.waitFor(t, "request.offer", func(e sseEvent) bool { return e.Data["request_id"] == secondID })
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+secondID+"/"+step, nil).status)
	}
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Go(func() {
			statuses[i] = customer.post(t, "/api/v1/requests/"+secondID+"/payments", map[string]any{"method": "wallet"}).status
		})
	}
	wg.Wait()
	secondFare := customer.get(t, "/api/v1/requests/"+secondID).num("fare_cents")
	if secondFare <= fare {
		assert.Contains(t, statuses, http.StatusCreated)
		assert.EqualValues(t, fare-secondFare, customer.get(t, "/api/v1/wallet").num("balance_cents"), "paid once, even with two clicks: %v", statuses)
	} else {
		assert.EqualValues(t, fare, customer.get(t, "/api/v1/wallet").num("balance_cents"))
	}
	assertLedgerBalanced(t)
}

func TestAWalletWithoutBalanceDoesNotPay(t *testing.T) {
	customer, _, id, _ := completedTaxi(t, "wallet")
	res := customer.post(t, "/api/v1/requests/"+id+"/payments", map[string]any{"method": "wallet"})
	assert.Equal(t, "WALLET_INSUFFICIENT", res.code(), res.text)
	assert.Equal(t, "unpaid", customer.get(t, "/api/v1/requests/"+id).str("payment_status"))
}

func TestTheRealGatewayFeeIsACostOfThePlatformRecordedOnce(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id, fare := completedTaxi(t, "gpo")
	commission := commissionOf(t, id)
	payment := payExpress(t, customer, id)
	path := "/api/v1/admin/payments/" + payment.str("id") + "/gateway-fee"

	assert.Equal(t, http.StatusForbidden, customer.do(t, http.MethodPut, path, map[string]any{"fee_cents": 1}, nil).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, path, map[string]any{"fee_cents": -1}, nil).status)
	recorded := admin.do(t, http.MethodPut, path, map[string]any{"fee_cents": 9_000}, nil)
	require.Equal(t, http.StatusOK, recorded.status, recorded.text)
	assert.Equal(t, "GATEWAY_FEE_RECORDED", admin.do(t, http.MethodPut, path, map[string]any{"fee_cents": 9_000}, nil).code())

	assert.EqualValues(t, fare-commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"), "the fee does not come out of the driver")
	assertLedgerBalanced(t)

	cashCustomer, cashDriver, cashID, _ := completedTaxi(t, "cash")
	cash := cashDriver.post(t, "/api/v1/requests/"+cashID+"/payments/cash", nil)
	require.Equal(t, http.StatusOK, cash.status, cash.text)
	assert.Equal(t, "GATEWAY_FEE_NOT_APPLICABLE", admin.do(t, http.MethodPut, "/api/v1/admin/payments/"+cash.str("id")+"/gateway-fee", map[string]any{"fee_cents": 1}, nil).code())
	_ = cashCustomer
}

func TestCashWithoutCommissionStillConfirmsAndLeavesNoLedgerEntry(t *testing.T) {
	ctx := context.Background()
	customer, driver, id, _ := completedTaxi(t, "cash")
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET commission_cents = 0 WHERE id = $1`, id)
	require.NoError(t, err)
	confirmed := driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil)
	require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
	assert.Equal(t, "paid", customer.get(t, "/api/v1/requests/"+id).str("payment_status"))
	var entries int
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM ledger_transactions WHERE request_id = $1`, id).Scan(&entries))
	assert.Zero(t, entries, "nobody owes anything: the driver holds the whole fare")
}
