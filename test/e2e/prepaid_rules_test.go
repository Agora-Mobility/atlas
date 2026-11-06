//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refundAs(t *testing.T, admin *person, path string, body map[string]any) result {
	t.Helper()
	return admin.do(t, http.MethodPost, path, body, map[string]string{"Idempotency-Key": uuid.NewString()})
}

func driverBalance(t *testing.T, who *person) int64 {
	t.Helper()
	res := who.get(t, "/api/v1/providers/me/ledger")
	require.Equal(t, http.StatusOK, res.status, res.text)
	return res.num("balance_cents")
}

func paidRemoval(t *testing.T, days int) (customer *person, id string, amount int64) {
	t.Helper()
	customer = newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, days)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id = created.str("id")
	merchantID, amount := payReference(t, customer, id)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	return customer, id, amount
}

func assignDriver(t *testing.T, driver *person, id string) {
	t.Helper()
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET provider_id = $1, status = 'accepted', accepted_at = now(), next_action_at = NULL WHERE id = $2`, driver.id, id)
	require.NoError(t, err)
}

func TestPrepaidRemovalPaysTheDriverOnCompletion(t *testing.T) {
	_, id, amount := paidRemoval(t, 11)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		res := driver.post(t, "/api/v1/requests/"+id+"/"+step, nil)
		require.Equal(t, http.StatusOK, res.status, step+" "+res.text)
	}
	var commission int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT commission_cents FROM service_requests WHERE id = $1`, id).Scan(&commission))
	var platform int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT COALESCE(sum(e.amount_cents),0) FROM ledger_entries e JOIN ledger_transactions x ON x.id = e.transaction_id WHERE x.request_id = $1 AND e.account_type = 'platform'`, id).Scan(&platform))
	balance := driverBalance(t, driver)
	assert.EqualValues(t, commission, platform, "the platform keeps only the commission")
	assert.EqualValues(t, amount-commission, balance, "the driver who did the prepaid removal earns fare minus commission")
}

func TestDriverCancelsPrepaidRemovalAndIsNotCharged(t *testing.T) {
	customer, id, amount := paidRemoval(t, 12)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	cancelled := cancelAs(t, driver, "provider", id)
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	balance := driverBalance(t, driver)
	wallet := walletOf(t, customer)
	assert.EqualValues(t, amount, wallet)
	assert.EqualValues(t, 0, balance, "the driver never received this money, so a refund cannot leave a debt")
}

func TestTipRefundDoesNotTouchTheFareStatus(t *testing.T) {
	admin := staffToken(t)
	customer, _, id, _ := completedTaxi(t, "gpo")
	require.Equal(t, "unpaid", customer.get(t, "/api/v1/requests/"+id).str("payment_status"))
	tip := customer.post(t, "/api/v1/requests/"+id+"/tips", map[string]any{"amount_cents": 50_000, "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, tip.status, tip.text)
	env.appypay.settle(tip.str("code"), "Success", 0)
	require.Eventually(t, func() bool {
		return customer.get(t, "/api/v1/requests/"+id+"/payments").text != "" && admin.get(t, "/api/v1/admin/payments/"+tip.str("id")).body["payment"].(map[string]any)["status"] == "paid"
	}, 15*time.Second, 50*time.Millisecond)
	refund := refundAs(t, admin, "/api/v1/admin/payments/"+tip.str("id")+"/refund", map[string]any{"amount_cents": 10_000, "reason": "Parcial da gorjeta"})
	require.Equal(t, http.StatusOK, refund.status, refund.text)
	after := customer.get(t, "/api/v1/requests/"+id)
	pay := customer.post(t, "/api/v1/requests/"+id+"/payments", map[string]any{"method": "gpo", "phone_number": "900000000"})
	assert.Equal(t, http.StatusCreated, pay.status, "the fare can still be charged: %s", pay.text)
	assert.Equal(t, "unpaid", after.str("payment_status"), "refunding part of a tip does not pay the fare")
}

func TestFullTipRefundKeepsTheFareStatus(t *testing.T) {
	admin := staffToken(t)
	customer, _, id, _ := completedTaxi(t, "gpo")
	payExpress(t, customer, id)
	tip := customer.post(t, "/api/v1/requests/"+id+"/tips", map[string]any{"amount_cents": 50_000, "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, tip.status, tip.text)
	env.appypay.settle(tip.str("code"), "Success", 0)
	require.Eventually(t, func() bool {
		return admin.get(t, "/api/v1/admin/payments/"+tip.str("id")).body["payment"].(map[string]any)["status"] == "paid"
	}, 15*time.Second, 50*time.Millisecond)
	refund := refundAs(t, admin, "/api/v1/admin/payments/"+tip.str("id")+"/refund", map[string]any{"reason": "Gorjeta toda"})
	require.Equal(t, http.StatusOK, refund.status, refund.text)
	after := customer.get(t, "/api/v1/requests/"+id).str("payment_status")
	assert.Equal(t, "paid", after, "the fare was paid and was not refunded")
}

func TestPartialRefundWithTheSameKeyHappensOnce(t *testing.T) {
	admin := staffToken(t)
	customer, _, id, fare := completedTaxi(t, "gpo")
	paid := payExpress(t, customer, id)
	before := env.appypay.refunds.Load()
	headers := map[string]string{"Idempotency-Key": uuid.NewString()}
	body := map[string]any{"amount_cents": 10_000, "reason": "Duplo clique"}
	first := admin.do(t, http.MethodPost, "/api/v1/admin/payments/"+paid.str("id")+"/refund", body, headers)
	second := admin.do(t, http.MethodPost, "/api/v1/admin/payments/"+paid.str("id")+"/refund", body, headers)
	var refunded int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT refunded_cents FROM payments WHERE id = $1`, paid.str("id")).Scan(&refunded))
	assert.EqualValues(t, 10_000, refunded, "the same click twice refunds once")
	assert.EqualValues(t, 1, env.appypay.refunds.Load()-before)
	assert.Equal(t, first.text, second.text)
	assert.NotZero(t, fare)
	assert.Equal(t, http.StatusBadRequest, admin.post(t, "/api/v1/admin/payments/"+paid.str("id")+"/refund", body).status, "a refund needs a key")
}

func TestChangingToReferenceDoesNotSkipThePayAheadRule(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	require.Equal(t, "confirmed", created.str("status"))
	changed := customer.do(t, http.MethodPut, "/api/v1/requests/"+created.str("request_id")+"/payment-method", map[string]any{"method": "reference"}, nil)
	assert.Equal(t, "PAYMENT_METHOD_LOCKED", changed.code(), "with reference a booking only confirms after it is paid")
	assert.Equal(t, "gpo", customer.get(t, "/api/v1/bookings/"+created.str("id")).str("payment_method"))
}

func TestReferralIsPaidWhenTheFirstRequestWasPaidAhead(t *testing.T) {
	friend, invited := newCustomer(t), newCustomer(t)
	code := friend.get(t, "/api/v1/users/me/referral")
	require.Equal(t, http.StatusNoContent, invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).status)
	workshop := newShop(t, 1)
	created := workshop.book(t, invited, workshop.firstSlot(t, invited), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	merchantID, amount := payReference(t, invited, created.str("request_id"))
	settleReference(t, merchantID, amount)
	waitBooking(t, invited, id, "confirmed")
	for _, to := range []string{"vehicle_received", "in_service", "ready"} {
		require.Equal(t, http.StatusOK, workshop.move(t, id, to).status)
	}
	approved := invited.post(t, "/api/v1/bookings/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)
	time.Sleep(time.Second)
	wallet := walletOf(t, friend)
	assert.EqualValues(t, code.num("reward_cents"), wallet, "the friend completed and paid the first request")
}

func TestRefundOfAFareAfterAnExtraKeepsTheSplit(t *testing.T) {
	admin := staffToken(t)
	ctx := context.Background()
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, requestID := created.str("id"), created.str("request_id")
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	settleReference(t, pending.str("code"), pending.num("amount_cents"))
	waitBooking(t, customer, id, "confirmed")
	balance := func() int64 {
		var value int64
		require.NoError(t, env.owner.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type = 'workshop' AND account_id = $1`, workshop.id).Scan(&value))
		return value
	}
	assert.Positive(t, balance(), "the workshop was credited")
	require.Equal(t, http.StatusOK, workshop.move(t, id, "vehicle_received").status)
	require.Equal(t, http.StatusOK, workshop.move(t, id, "in_service").status)
	extra := workshop.owner.post(t, "/api/v1/fleet/bookings/"+id+"/extras", map[string]any{"description": "Pastilhas", "amount_cents": 3_500_000})
	require.Equal(t, http.StatusCreated, extra.status, extra.text)
	extraID := extra.body["extras"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, http.StatusOK, customer.post(t, "/api/v1/bookings/"+id+"/extras/"+extraID+"/answer", map[string]bool{"approve": true}).status)
	require.Equal(t, http.StatusOK, workshop.move(t, id, "ready").status)
	require.Equal(t, http.StatusOK, customer.post(t, "/api/v1/bookings/"+id+"/approve", nil).status)
	refund := refundAs(t, admin, "/api/v1/admin/payments/"+pending.str("id")+"/refund", map[string]any{"reason": "Fatura errada"})
	require.Equal(t, http.StatusOK, refund.status, refund.text)
	after := balance()
	assert.EqualValues(t, 0, after, "refunding the whole payment takes back exactly what it credited")
}

func TestAnExpiredRequestGivesTheVoucherBack(t *testing.T) {
	clearOnlineProviders(t)
	setPolicy(t, "taxi", "search_ttl_seconds = 3, offer_ttl_seconds = 1")
	code := newVoucher(t, map[string]any{"category_id": categoryID(t, "taxi")})
	customer := newCustomer(t)
	stream := customer.listen(t, "")
	created := askFor(t, customer, taxiQuote(t, customer, nil), map[string]any{"voucher_code": code})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	stream.waitForStatus(t, "expired")
	again := askFor(t, customer, taxiQuote(t, customer, nil), map[string]any{"voucher_code": code})
	var released bool
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT released_at IS NOT NULL FROM voucher_redemptions WHERE request_id = $1`, created.str("id")).Scan(&released))
	assert.True(t, released, "the customer received no service, so the voucher comes back")
	if again.status == http.StatusCreated {
		_ = cancelAs(t, customer, "customer", again.str("id"))
	}
}

func TestABookingNobodyPaysDoesNotHoldTheSlotForever(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	slot := workshop.firstSlot(t, customer)
	created := workshop.book(t, customer, slot, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	var rows int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE request_id = $1`, created.str("request_id")).Scan(&rows))
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET created_at = now() - interval '2 days', updated_at = now() - interval '2 days' WHERE id = $1`, created.str("request_id"))
	require.NoError(t, err)
	_, err = env.owner.Exec(context.Background(), `UPDATE bookings SET created_at = now() - interval '2 days', updated_at = now() - interval '2 days' WHERE id = $1`, created.str("id"))
	require.NoError(t, err)
	assert.Zero(t, rows, "nobody asked for the reference")
	view := waitBooking(t, customer, created.str("id"), "cancelled")
	rival := workshop.book(t, newCustomer(t), slot, "gpo")
	assert.Equal(t, "cancelled", view.str("status"), "an unpaid hold has to end")
	assert.Equal(t, http.StatusCreated, rival.status, rival.text)
}

func TestAReferencePaidAfterACancellationComesBackWhole(t *testing.T) {
	ctx := context.Background()
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 13)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	merchantID, amount := payReference(t, customer, id)
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET scheduled_for = now() + interval '2 hours' WHERE id = $1`, id)
	require.NoError(t, err)
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	cancelled := cancelAs(t, customer, "customer", id)
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return walletOf(t, customer) > 0 }, 15*time.Second, 50*time.Millisecond)
	wallet := walletOf(t, customer)
	assert.Zero(t, fee, "it was not paid when cancelled, so it had no fee")
	assert.EqualValues(t, amount, wallet, "the late money comes back whole")
}

func TestACancelledUnpaidRemovalHasNoFee(t *testing.T) {
	ctx := context.Background()
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 14)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET scheduled_for = now() + interval '2 hours' WHERE id = $1`, id)
	require.NoError(t, err)
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	cancelled := cancelAs(t, customer, "customer", id)
	assert.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	blocked := askFor(t, customer, taxiQuote(t, customer, nil), nil)
	assert.Zero(t, fee, "a removal that never confirmed has no fee")
	assert.Equal(t, http.StatusCreated, blocked.status, blocked.text)
}

func TestCustomerCancelsPaidRemovalLateWithDriver(t *testing.T) {
	customer, id, amount := paidRemoval(t, 15)
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET scheduled_for = now() + interval '2 hours', accepted_at = now() - interval '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	cancelled := cancelAs(t, customer, "customer", id)
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	balance, wallet := driverBalance(t, driver), walletOf(t, customer)
	commission := commissionOf(t, id)
	assert.EqualValues(t, fee-fee*commission/amount, balance, "the fee goes to the assigned driver minus the commission")
	assert.EqualValues(t, amount-fee, wallet)
}

func TestGpoRemovalPaysTheDriver(t *testing.T) {
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 16)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	driver := newApprovedProvider(t, "removals")
	assignDriver(t, driver, id)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}
	payExpress(t, customer, id)
	assert.EqualValues(t, created.num("fare_cents")-commissionOf(t, id), driverBalance(t, driver))
}
