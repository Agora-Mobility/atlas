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

func payReference(t *testing.T, customer *person, requestID string) (merchantID string, amount int64) {
	t.Helper()
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	return pending.str("code"), pending.num("amount_cents")
}

func settleReference(t *testing.T, merchantID string, amount int64) {
	t.Helper()
	require.NotEmpty(t, env.appypay.settle(merchantID, "Success", 0), amount)
}

func waitBooking(t *testing.T, customer *person, id, status string) result {
	t.Helper()
	var last result
	require.Eventuallyf(t, func() bool {
		last = customer.get(t, "/api/v1/bookings/"+id)
		return last.str("status") == status
	}, 15*time.Second, 50*time.Millisecond, "booking never reached %s", status)
	return last
}

func walletOf(t *testing.T, who *person) int64 {
	t.Helper()
	return who.get(t, "/api/v1/wallet").num("balance_cents")
}

func TestAReferenceConfirmsTheBookingAndComesBackToTheWalletOnCancel(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	slot := workshop.firstSlot(t, customer)
	created := workshop.book(t, customer, slot, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, "awaiting_payment", created.str("status"))
	assert.EqualValues(t, 3_500_000, created.num("amount_due_cents"))
	assert.Equal(t, "BOOKING_SLOT_FULL", workshop.book(t, newCustomer(t), slot, "gpo").code(), "the slot is held while it waits")
	assert.Equal(t, "BOOKING_STATE_INVALID", workshop.move(t, created.str("id"), "vehicle_received").code(), "not before it is paid")
	assert.Equal(t, "REQUEST_NOT_PAYABLE", customer.post(t, "/api/v1/requests/"+created.str("request_id")+"/payments", map[string]string{"method": "gpo"}).code())

	merchantID, amount := payReference(t, customer, created.str("request_id"))
	assert.EqualValues(t, 3_500_000, amount)
	settleReference(t, merchantID, amount)
	confirmed := waitBooking(t, customer, created.str("id"), "confirmed")
	assert.Equal(t, "paid", confirmed.str("payment_status"))
	assert.Zero(t, walletOf(t, customer))

	reason := cancelReason(t, customer, "customer", "maintenance", "changed_date")
	cancelled := customer.post(t, "/api/v1/bookings/"+created.str("id")+"/cancel", map[string]any{"reason_id": reason})
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	assert.Equal(t, amount, walletOf(t, customer), "a reference comes back to the wallet")
	var workshopBalance int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT COALESCE(sum(amount_cents), 0) FROM ledger_entries WHERE account_type = 'workshop' AND account_id = $1`, workshop.id).Scan(&workshopBalance))
	assert.Zero(t, workshopBalance, "the workshop gives back what it was credited")
	assert.Equal(t, http.StatusCreated, workshop.book(t, newCustomer(t), slot, "gpo").status, "the slot is free again")
}

func TestAnExpiredReferenceCancelsTheBookingAndFreesTheSlot(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	slot := workshop.firstSlot(t, customer)
	created := workshop.book(t, customer, slot, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	payReference(t, customer, created.str("request_id"))
	_, err := env.owner.Exec(context.Background(), `UPDATE payments SET expires_at = now() - interval '1 second', next_check_at = now() WHERE request_id = $1`, created.str("request_id"))
	require.NoError(t, err)
	waitBooking(t, customer, created.str("id"), "cancelled")
	assert.Equal(t, "cancelled", customer.get(t, "/api/v1/requests/"+created.str("request_id")).str("status"))
	assert.Zero(t, walletOf(t, customer))
	assert.Equal(t, http.StatusCreated, workshop.book(t, newCustomer(t), slot, "gpo").status)
}

func TestAReferencePaidAfterTheCancellationGoesToTheWallet(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	merchantID, amount := payReference(t, customer, created.str("request_id"))
	reason := cancelReason(t, customer, "customer", "maintenance", "changed_date")
	require.Equal(t, http.StatusOK, customer.post(t, "/api/v1/bookings/"+created.str("id")+"/cancel", map[string]any{"reason_id": reason}).status)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return walletOf(t, customer) == amount }, 15*time.Second, 50*time.Millisecond, "the late money is not lost")
}

func TestAnExtraOfAPrepaidBookingIsChargedApart(t *testing.T) {
	workshop := newShop(t, 1)
	customer := newCustomer(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, requestID := created.str("id"), created.str("request_id")
	merchantID, amount := payReference(t, customer, requestID)
	settleReference(t, merchantID, amount)
	waitBooking(t, customer, id, "confirmed")

	require.Equal(t, http.StatusOK, workshop.move(t, id, "vehicle_received").status)
	require.Equal(t, http.StatusOK, workshop.move(t, id, "in_service").status)
	extra := workshop.owner.post(t, "/api/v1/fleet/bookings/"+id+"/extras", map[string]any{"description": "Pastilhas", "amount_cents": 1_000_000})
	require.Equal(t, http.StatusCreated, extra.status, extra.text)
	extraID := extra.body["extras"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, http.StatusOK, customer.post(t, "/api/v1/bookings/"+id+"/extras/"+extraID+"/answer", map[string]bool{"approve": true}).status)
	require.Equal(t, http.StatusOK, workshop.move(t, id, "ready").status)
	approved := customer.post(t, "/api/v1/bookings/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)
	assert.EqualValues(t, 1_000_000, approved.num("amount_due_cents"), "only the extra is left")
	assert.Equal(t, "unpaid", approved.str("payment_status"))

	second := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "reference"})
	require.Equal(t, http.StatusCreated, second.status, second.text)
	assert.EqualValues(t, 1_000_000, second.num("amount_cents"))
	assert.Equal(t, "extra", second.str("kind"))
	settleReference(t, second.str("code"), 1_000_000)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+requestID).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	assert.Zero(t, walletOf(t, customer), "the extra is not taken as a duplicate")
	var commission, balance int64
	ctx := context.Background()
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT commission_cents FROM service_requests WHERE id = $1`, requestID).Scan(&commission))
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents), 0) FROM ledger_entries WHERE account_type = 'workshop' AND account_id = $1`, workshop.id).Scan(&balance))
	assert.InDelta(t, 4_500_000-commission, balance, 1, "the workshop gets the whole total minus the commission")
}

func TestAReferenceHoldsTheSearchOfARemovalUntilItIsPaid(t *testing.T) {
	ctx := context.Background()
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 5)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	nextAction := func() *time.Time {
		var at *time.Time
		require.NoError(t, env.owner.QueryRow(ctx, `SELECT next_action_at FROM service_requests WHERE id = $1`, id).Scan(&at))
		return at
	}
	assert.Nil(t, nextAction(), "no search before it is paid")
	merchantID, amount := payReference(t, customer, id)
	assert.EqualValues(t, 4_500_000, amount)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return nextAction() != nil }, 15*time.Second, 50*time.Millisecond, "the search is scheduled once paid")
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", id).status)
	assert.Equal(t, amount, walletOf(t, customer), "more than 24 h before, all of it comes back")
}

func TestAPaidRemovalCancelledLateKeepsTheFee(t *testing.T) {
	ctx := context.Background()
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	when = when.AddDate(0, 0, 6)
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "reference")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	merchantID, amount := payReference(t, customer, id)
	settleReference(t, merchantID, amount)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 50*time.Millisecond)
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET scheduled_for = now() + interval '2 hours' WHERE id = $1`, id)
	require.NoError(t, err)
	fee := customer.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents")
	require.Positive(t, fee)
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", id).status)
	assert.Equal(t, amount-fee, walletOf(t, customer), "the fee stays, the rest comes back")
}
