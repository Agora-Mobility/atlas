//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestLedger(t *testing.T, id string) map[string]int64 {
	t.Helper()
	rows, err := env.owner.Query(context.Background(), `SELECT e.account_type::text, COALESCE(sum(e.amount_cents),0)::bigint FROM ledger_entries e JOIN ledger_transactions x ON x.id = e.transaction_id WHERE x.request_id = $1 GROUP BY 1`, id)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]int64{}
	sum := int64(0)
	for rows.Next() {
		var k string
		var v int64
		require.NoError(t, rows.Scan(&k, &v))
		out[k] = v
		sum += v
	}
	require.Zero(t, sum, "request ledger must sum to zero")
	return out
}

func makeItLate(t *testing.T, id string) {
	t.Helper()
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET scheduled_for = now() + interval '2 hours', accepted_at = now() - interval '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)
}

func paidPaymentOf(t *testing.T, id, kind string) (string, int64) {
	t.Helper()
	var pid string
	var cents int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id::text, amount_cents FROM payments WHERE request_id = $1 AND kind = $2 AND status = 'paid' ORDER BY created_at LIMIT 1`, id, kind).Scan(&pid, &cents))
	return pid, cents
}

func paidPrepaidBooking(t *testing.T) (w *shop, customer *person, id, requestID string, amount int64, payID string) {
	t.Helper()
	w = newShop(t, 1)
	customer = newCustomer(t)
	created := w.book(t, customer, w.firstSlot(t, customer), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, requestID = created.str("id"), created.str("request_id")
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	settleReference(t, pending.str("code"), pending.num("amount_cents"))
	waitBooking(t, customer, id, "confirmed")
	return w, customer, id, requestID, pending.num("amount_cents"), pending.str("id")
}

func approvedExtra(t *testing.T, w *shop, customer *person, id string, cents int64) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.move(t, id, "vehicle_received").status)
	require.Equal(t, http.StatusOK, w.move(t, id, "in_service").status)
	extra := w.owner.post(t, "/api/v1/fleet/bookings/"+id+"/extras", map[string]any{"description": "Pastilhas", "amount_cents": cents})
	require.Equal(t, http.StatusCreated, extra.status, extra.text)
	extraID := extra.body["extras"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, http.StatusOK, customer.post(t, "/api/v1/bookings/"+id+"/extras/"+extraID+"/answer", map[string]bool{"approve": true}).status)
	require.Equal(t, http.StatusOK, w.move(t, id, "ready").status)
	approved := customer.post(t, "/api/v1/bookings/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)
}

func TestRefundOfAnExtraGivesBackItsCommission(t *testing.T) {
	admin := staffToken(t)
	w, customer, id, requestID, _, _ := paidPrepaidBooking(t)
	before := requestLedger(t, requestID)
	approvedExtra(t, w, customer, id, 1_000_000)
	second := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, second.status, second.text)
	settleReference(t, second.str("code"), 1_000_000)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+requestID).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	assert.Greater(t, requestLedger(t, requestID)["workshop"], before["workshop"], "the extra was credited")
	res := refundAs(t, admin, "/api/v1/admin/payments/"+second.str("id")+"/refund", map[string]any{"reason": "extra"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	after := requestLedger(t, requestID)
	assert.EqualValues(t, before["workshop"], after["workshop"], "workshop returns to before the extra")
	assert.EqualValues(t, before["platform"], after["platform"], "platform returns to before the extra")
}

func TestRefundOfACancellationFeeUndoesItsSplit(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id := assignedTaxi(t)
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET accepted_at = now() - interval '121 seconds' WHERE id = $1`, id)
	require.NoError(t, err)
	cancelled := cancelAs(t, customer, "customer", id)
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	fee := cancelled.num("cancel_fee_cents")
	require.Positive(t, fee)
	paid := payExpress(t, customer, id)
	assert.Positive(t, driverBalance(t, driver), "the fee reached the driver")
	res := refundAs(t, admin, "/api/v1/admin/payments/"+paid.str("id")+"/refund", map[string]any{"reason": "fee"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	after := requestLedger(t, id)
	assert.Zero(t, after["provider"], "driver returns to zero")
	assert.Zero(t, after["platform"], "platform returns to zero")
}

func TestKeptFeeCreditIsLimitedToWhatWasKept(t *testing.T) {
	admin := staffToken(t)
	customer, id, amount := paidRemoval(t, 21)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	makeItLate(t, id)
	pid, _ := paidPaymentOf(t, id, "fare")
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	require.Positive(t, fee)
	refund := amount - fee/2
	res := refundAs(t, admin, "/api/v1/admin/payments/"+pid+"/refund", map[string]any{"amount_cents": refund, "reason": "parcial"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	cancelled := cancelAs(t, customer, "customer", id)
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	kept := amount - refund
	led := requestLedger(t, id)
	assert.LessOrEqual(t, driverBalance(t, driver), kept, "the driver cannot receive more than what stayed")
	assert.GreaterOrEqual(t, led["platform"], int64(0), "the platform does not pay out of its own pocket")
}

func TestRefundOfTheKeptFeeTakesItFromTheDriver(t *testing.T) {
	admin := staffToken(t)
	customer, id, _ := paidRemoval(t, 22)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	makeItLate(t, id)
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", id).status)
	pid, _ := paidPaymentOf(t, id, "fare")
	credited := driverBalance(t, driver)
	require.Positive(t, credited)
	res := refundAs(t, admin, "/api/v1/admin/payments/"+pid+"/refund", map[string]any{"reason": "taxa indevida"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	led := requestLedger(t, id)
	assert.Zero(t, driverBalance(t, driver), "everything was given back, nothing is left to earn")
	assert.Zero(t, led["platform"], "platform ends at zero")
}

func TestWaitingDoesNotChangeAClosedRemovalPrice(t *testing.T) {
	customer, id, amount := paidRemoval(t, 23)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = env.owner.Exec(ctx, `ALTER TABLE service_tariffs DISABLE TRIGGER service_tariffs_immutable`)
		_, _ = env.owner.Exec(ctx, `UPDATE service_tariffs t SET per_minute_fee_cents = 0 FROM services s WHERE s.id = t.service_id AND s.slug = 'removals'`)
		_, _ = env.owner.Exec(ctx, `ALTER TABLE service_tariffs ENABLE TRIGGER service_tariffs_immutable`)
	})
	var perMinute int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT t.per_minute_fee_cents FROM fare_estimates e JOIN service_requests r ON r.fare_estimate_id = e.id JOIN service_tariffs t ON t.id = e.tariff_id WHERE r.id = $1`, id).Scan(&perMinute))
	_, err0 := env.owner.Exec(context.Background(), `ALTER TABLE service_tariffs DISABLE TRIGGER service_tariffs_immutable`)
	require.NoError(t, err0)
	_, err0 = env.owner.Exec(context.Background(), `UPDATE service_tariffs t SET per_minute_fee_cents = 1500 FROM fare_estimates e JOIN service_requests r ON r.fare_estimate_id = e.id WHERE r.id = $1 AND t.id = e.tariff_id`, id)
	require.NoError(t, err0)
	_, err0 = env.owner.Exec(context.Background(), `ALTER TABLE service_tariffs ENABLE TRIGGER service_tariffs_immutable`)
	require.NoError(t, err0)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/en-route", nil).status)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/arrive", nil).status)
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET arrived_at = now() - interval '60 minutes' WHERE id = $1`, id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/start", nil).status)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/complete", nil).status)
	view := customer.get(t, "/api/v1/requests/"+id)
	commission := commissionOf(t, id)
	led := requestLedger(t, id)
	assert.EqualValues(t, amount-commission, driverBalance(t, driver), "paid minus commission")
	assert.EqualValues(t, amount, view.num("fare_cents"), "the fare did not grow without a payment")
	assert.EqualValues(t, commission, led["platform"], "the platform keeps its commission")
}

func TestAPaidRemovalCannotBeBoosted(t *testing.T) {
	ctx := context.Background()
	customer, id, amount := paidRemoval(t, 24)
	_, err := env.owner.Exec(ctx, `UPDATE dispatch_policies SET allow_boost = true WHERE id = (SELECT policy_id FROM service_requests WHERE id = $1)`, id)
	require.NoError(t, err)
	defer func() {
		_, _ = env.owner.Exec(ctx, `UPDATE dispatch_policies SET allow_boost = false WHERE id = (SELECT policy_id FROM service_requests WHERE id = $1)`, id)
	}()
	_, err = env.owner.Exec(ctx, `UPDATE service_requests SET status = 'searching', search_deadline_at = now() + interval '1 hour', next_action_at = now() + interval '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)
	boosted := customer.post(t, "/api/v1/requests/"+id+"/boost", map[string]int{"percent": 20})
	assert.NotEqual(t, http.StatusOK, boosted.status, boosted.text)
	assert.EqualValues(t, amount, customer.get(t, "/api/v1/requests/"+id).num("fare_cents"), "the price is closed")
}

func TestPartialRefundDoesNotForgiveTheExtra(t *testing.T) {
	admin := staffToken(t)
	w, customer, id, requestID, _, pay := paidPrepaidBooking(t)
	approvedExtra(t, w, customer, id, 1_000_000)
	view := customer.get(t, "/api/v1/requests/"+requestID)
	require.Equal(t, "unpaid", view.str("payment_status"))
	res := refundAs(t, admin, "/api/v1/admin/payments/"+pay+"/refund", map[string]any{"amount_cents": 1000, "reason": "parcial"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	view = customer.get(t, "/api/v1/requests/"+requestID)
	second := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	assert.Equal(t, "unpaid", view.str("payment_status"), "the extra is still owed")
	assert.Equal(t, http.StatusCreated, second.status, "the extra can still be paid")
}

func TestNoReferralAfterAFullRefund(t *testing.T) {
	admin := staffToken(t)
	friend, invited := newCustomer(t), newCustomer(t)
	code := friend.get(t, "/api/v1/users/me/referral")
	require.Equal(t, http.StatusNoContent, invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).status)
	w := newShop(t, 1)
	created := w.book(t, invited, w.firstSlot(t, invited), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, requestID := created.str("id"), created.str("request_id")
	pending := invited.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status)
	settleReference(t, pending.str("code"), pending.num("amount_cents"))
	waitBooking(t, invited, id, "confirmed")
	assert.Equal(t, "REFUND_NEEDS_CANCEL", refundAs(t, admin, "/api/v1/admin/payments/"+pending.str("id")+"/refund", map[string]any{"reason": "tudo"}).code(), "the service cannot go on for free")
	require.Equal(t, http.StatusOK, invited.post(t, "/api/v1/bookings/"+id+"/cancel", map[string]any{"reason_id": cancelReason(t, invited, "customer", "maintenance", "changed_date")}).status)
	assert.Zero(t, walletOf(t, friend), "nothing was paid in the end")
}

func TestVoucherTipAndPartialRefundsEndAtZero(t *testing.T) {
	admin := staffToken(t)
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	created := askFor(t, customer, option, map[string]any{"payment_method": "gpo", "voucher_code": newVoucher(t, nil)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, stream.waitFor(t, "request.offer", nil)).status)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}
	view := customer.get(t, "/api/v1/requests/"+id)
	fare := view.num("fare_cents")
	require.Positive(t, view.num("discount_cents"))
	pending := payExpress(t, customer, id)
	commission := commissionOf(t, id)
	assert.EqualValues(t, fare-commission, driverBalance(t, driver))
	tip := customer.post(t, "/api/v1/requests/"+id+"/tips", map[string]any{"amount_cents": 50_000, "method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, tip.status, tip.text)
	env.appypay.settle(tip.str("code"), "Success", 0)
	require.Eventually(t, func() bool { return driverBalance(t, driver) == fare-commission+50_000 }, 15*time.Second, 50*time.Millisecond)
	path := "/api/v1/admin/payments/" + pending.str("id") + "/refund"
	for _, cents := range []int64{333, 777, 1001, 12_345} {
		res := refundAs(t, admin, path, map[string]any{"amount_cents": cents, "reason": "parte"})
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
	res := refundAs(t, admin, path, map[string]any{"reason": "resto"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	res = refundAs(t, admin, "/api/v1/admin/payments/"+tip.str("id")+"/refund", map[string]any{"reason": "tip"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	led := requestLedger(t, id)
	assert.Zero(t, driverBalance(t, driver), "all refunded: driver at zero")
	assert.Zero(t, led["platform"], "all refunded: platform at zero")
}

func TestPrepaidVoucherPartialCompleteRestEndAtZero(t *testing.T) {
	ctx := context.Background()
	admin := staffToken(t)
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 25)
	estimate := removalEstimate(t, customer, when, "Carrinha", map[string]any{})
	body := map[string]any{"service_id": estimate["service_id"], "pickup": pickup, "destination": target, "fare_estimate_id": estimate["fare_estimate_id"], "scheduled_for": when, "payment_method": "reference", "voucher_code": newVoucher(t, nil),
		"removal": map[string]any{"window_id": windowID, "property_code": "home", "pickup_floor": 2, "destination_floor": 0}}
	created := askFor(t, customer, estimate, body)
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	require.Positive(t, created.num("discount_cents"))
	pending := customer.post(t, "/api/v1/requests/"+id+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	settleReference(t, pending.str("code"), pending.num("amount_cents"))
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	path := "/api/v1/admin/payments/" + pending.str("id") + "/refund"
	res := refundAs(t, admin, path, map[string]any{"amount_cents": 100_000, "reason": "parte"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}
	assert.Positive(t, driverBalance(t, driver), "the driver earned what is left")
	res = refundAs(t, admin, path, map[string]any{"reason": "resto"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	led := requestLedger(t, id)
	var commission int64
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT commission_cents FROM service_requests WHERE id = $1`, id).Scan(&commission))
	assert.Zero(t, driverBalance(t, driver))
	assert.Zero(t, led["platform"])
}

func TestAFeeAboveWhatWasPaidOnlyKeepsWhatWasPaid(t *testing.T) {
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 27)
	estimate := removalEstimate(t, customer, when, "Carrinha", map[string]any{})
	body := map[string]any{"scheduled_for": when, "payment_method": "reference", "voucher_code": newVoucher(t, map[string]any{"discount_bps": 9000}),
		"removal": map[string]any{"window_id": windowID, "property_code": "home", "pickup_floor": 2, "destination_floor": 0}}
	created := askFor(t, customer, estimate, body)
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	merchantID, amount := payReference(t, customer, id)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	makeItLate(t, id)
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", id).status)
	led := requestLedger(t, id)
	require.Greater(t, fee, amount, "the fee is above what was paid")
	assert.LessOrEqual(t, driverBalance(t, driver), amount, "the driver cannot get more than the customer paid")
	assert.GreaterOrEqual(t, led["platform"], int64(0), "the platform does not pay it")
	assert.Zero(t, walletOf(t, customer), "all that was paid stays as the fee")
}

func twoAttempts(t *testing.T, customer *person, id string) (first, second result) {
	t.Helper()
	path := "/api/v1/requests/" + id + "/payments"
	first = customer.post(t, path, map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, first.status, first.text)
	env.appypay.settle(first.str("code"), "Failed", 0)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "unpaid" }, 15*time.Second, 50*time.Millisecond)
	second = customer.post(t, path, map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, second.status, second.text)
	return first, second
}

func lateConfirmation(t *testing.T, attempt result) {
	t.Helper()
	require.NotEmpty(t, env.appypay.settle(attempt.str("code"), "Success", 0))
	require.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"merchantTransactionId": attempt.str("code")}))
}

func openRemovalByReference(t *testing.T, days int) (*person, string) {
	t.Helper()
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, days)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	return customer, created.str("id")
}

func TestAnExpiredAttemptDoesNotCancelAPaidRemoval(t *testing.T) {
	customer, id := openRemovalByReference(t, 28)
	first, second := twoAttempts(t, customer, id)
	lateConfirmation(t, first)
	require.Equal(t, "paid", customer.get(t, "/api/v1/requests/"+id).str("payment_status"))
	_, err := env.owner.Exec(context.Background(), `UPDATE payments SET expires_at = now() - interval '1 second', next_check_at = now() WHERE id = $1`, second.str("id"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var status string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT status FROM payments WHERE id = $1`, second.str("id")).Scan(&status))
		return status == "expired"
	}, 15*time.Second, 50*time.Millisecond)
	view := customer.get(t, "/api/v1/requests/"+id)
	assert.Equal(t, "scheduled", view.str("status"))
	assert.Equal(t, "paid", view.str("payment_status"))
	assert.Zero(t, walletOf(t, customer))
}

func TestADuplicateInTheWalletCannotBeRefundedAgainAndIsNotTheMainPayment(t *testing.T) {
	admin := staffToken(t)
	customer, id := openRemovalByReference(t, 29)
	first, second := twoAttempts(t, customer, id)
	lateConfirmation(t, second)
	lateConfirmation(t, first)
	amount := first.num("amount_cents")
	require.Equal(t, amount, walletOf(t, customer), "the duplicate goes to the wallet")
	assert.Equal(t, "REFUND_TOO_LARGE", refundAs(t, admin, "/api/v1/admin/payments/"+first.str("id")+"/refund", map[string]any{"reason": "outra vez"}).code())
	assert.Equal(t, amount, walletOf(t, customer))

	half := amount / 2
	res := refundAs(t, admin, "/api/v1/admin/payments/"+second.str("id")+"/refund", map[string]any{"amount_cents": half, "reason": "metade"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}
	assert.EqualValues(t, (amount-commissionOf(t, id))/2, driverBalance(t, driver), "half of the service stayed paid")
	assert.GreaterOrEqual(t, requestLedger(t, id)["platform"], int64(0))
}

func TestARefundDuringAPendingExtraDoesNotForgiveIt(t *testing.T) {
	admin := staffToken(t)
	w, customer, id, requestID, amount, pay := paidPrepaidBooking(t)
	approvedExtra(t, w, customer, id, 1_000_000)
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	res := refundAs(t, admin, "/api/v1/admin/payments/"+pay+"/refund", map[string]any{"amount_cents": amount / 4, "reason": "parte"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	env.appypay.settle(pending.str("code"), "Failed", 0)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+requestID).str("payment_status") == "unpaid" }, 15*time.Second, 50*time.Millisecond, "the extra is still owed")
	assert.Equal(t, http.StatusCreated, customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"}).status)
}

func TestAPrepaidServiceCannotGoOnAfterAFullRefund(t *testing.T) {
	admin := staffToken(t)
	customer, id, _ := paidRemoval(t, 30)
	pid, _ := paidPaymentOf(t, id, "fare")
	assert.Equal(t, "REFUND_NEEDS_CANCEL", refundAs(t, admin, "/api/v1/admin/payments/"+pid+"/refund", map[string]any{"reason": "tudo"}).code())
	assert.Equal(t, "paid", customer.get(t, "/api/v1/requests/"+id).str("payment_status"))
}

func TestAFullRefundIsAllowedWhenTheServiceCanNoLongerBeCancelled(t *testing.T) {
	admin := staffToken(t)
	w, customer, id, requestID, _, pay := paidPrepaidBooking(t)
	assert.Equal(t, "REFUND_NEEDS_CANCEL", refundAs(t, admin, "/api/v1/admin/payments/"+pay+"/refund", map[string]any{"reason": "tudo"}).code())
	require.Equal(t, http.StatusOK, w.move(t, id, "vehicle_received").status)
	res := refundAs(t, admin, "/api/v1/admin/payments/"+pay+"/refund", map[string]any{"reason": "tudo"})
	require.Equal(t, http.StatusOK, res.status, res.text)
	assert.Equal(t, "refunded", customer.get(t, "/api/v1/requests/"+requestID).str("payment_status"))
	assert.Zero(t, requestLedger(t, requestID)["workshop"])
}

func TestADuplicateIsMarkedAndLeftOutOfTheTotals(t *testing.T) {
	customer, id := openRemovalByReference(t, 31)
	first, second := twoAttempts(t, customer, id)
	lateConfirmation(t, second)
	lateConfirmation(t, first)
	view := customer.get(t, "/api/v1/requests/"+id)
	breakdown := view.body["breakdown"].(map[string]any)
	assert.EqualValues(t, second.num("amount_cents"), breakdown["paid_cents"], "the duplicate is not part of what paid the service")
	found := false
	for _, item := range customer.get(t, "/api/v1/requests/"+id+"/payments").body["items"].([]any) {
		payment := item.(map[string]any)
		if payment["id"] == first.str("id") {
			found = payment["duplicate"] == true
		}
	}
	assert.True(t, found, "the duplicate says so")
}
