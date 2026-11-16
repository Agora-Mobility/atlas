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

func assignedTaxi(t *testing.T) (customer, driver *person, requestID string) {
	t.Helper()
	clearOnlineProviders(t)
	driver = newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer = newCustomer(t)
	created := requestTaxi(t, customer)
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	return customer, driver, created.str("id")
}

func TestReasonsComeFromTheCatalogueOfTheActorAndService(t *testing.T) {
	customer := newCustomer(t)
	taxi := customer.get(t, "/api/v1/cancellation-reasons?actor=customer&category=taxi")
	require.Equal(t, http.StatusOK, taxi.status, taxi.text)
	byCode := map[string]map[string]any{}
	for _, item := range taxi.body["items"].([]any) {
		reason := item.(map[string]any)
		byCode[reason["code"].(string)] = reason
	}
	assert.Contains(t, byCode, "driver_late")
	assert.Equal(t, true, byCode["other"]["requires_note"], "other needs a text")
	assert.NotContains(t, byCode, "solved", "that one is of towing")
	assert.NotContains(t, byCode, "customer_no_show", "that one is of the driver")
	assert.Equal(t, "UNKNOWN_REASON_ACTOR", customer.get(t, "/api/v1/cancellation-reasons?actor=nobody&category=taxi").code())
}

func TestCancellingFollowsTheCatalogueAndTheFeeRule(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	id := created.str("id")
	path := "/api/v1/requests/" + id + "/cancel"

	t.Run("before a driver it is free and a reason is mandatory", func(t *testing.T) {
		assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{}).status)
		assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"reason": "texto livre"}).status, "free text does not replace the catalogue")
		assert.Equal(t, "REASON_NOT_APPLICABLE", customer.post(t, path, map[string]any{"reason_id": uuid.NewString()}).code(), "a reason that does not exist")
		assert.Equal(t, "REASON_NOT_APPLICABLE", customer.post(t, path, map[string]any{"reason_id": cancelReason(t, customer, "provider", "taxi", "customer_no_show")}).code(), "a reason of another actor")
		assert.Equal(t, "REASON_NOT_APPLICABLE", customer.post(t, path, map[string]any{"reason_id": cancelReason(t, customer, "customer", "towing", "solved")}).code(), "a reason of another service")
		other := cancelReason(t, customer, "customer", "taxi", "other")
		assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"reason_id": other}).status, "other needs a note")

		preview := customer.get(t, "/api/v1/requests/"+id+"/cancellation")
		require.Equal(t, http.StatusOK, preview.status)
		assert.EqualValues(t, 0, preview.num("fee_cents"))

		cancelled := customer.post(t, path, map[string]any{"reason_id": other, "note": "Tive um imprevisto"})
		require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
		assert.EqualValues(t, 0, cancelled.num("cancel_fee_cents"))
		assert.Equal(t, other, cancelled.str("cancel_reason_id"), "kept by id for the reports")
		assert.Contains(t, cancelled.str("cancel_reason"), "Tive um imprevisto")
	})

	t.Run("with a driver it is free in the first minutes and then the fee is told first", func(t *testing.T) {
		customer, driver, id := assignedTaxi(t)
		reason := cancelReason(t, customer, "customer", "taxi", "changed_plans")
		preview := customer.get(t, "/api/v1/requests/"+id+"/cancellation")
		assert.EqualValues(t, 0, preview.num("fee_cents"))
		assert.NotEmpty(t, preview.str("free_until"))

		_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET accepted_at = now() - interval '121 seconds' WHERE id = $1`, id)
		require.NoError(t, err)

		preview = customer.get(t, "/api/v1/requests/"+id+"/cancellation")
		var fee int64
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT r.fee_cents FROM cancellation_rules r JOIN services s ON s.id = r.service_id WHERE s.slug = 'taxi'`).Scan(&fee))
		assert.Positive(t, fee)
		assert.EqualValues(t, fee, preview.num("fee_cents"), "the rule of the service")

		path := "/api/v1/requests/" + id + "/cancel"
		blind := customer.post(t, path, map[string]any{"reason_id": reason})
		assert.Equal(t, "CANCEL_FEE_CHANGED", blind.code(), "no charge the customer did not see")
		assert.EqualValues(t, fee, blind.num("fee_cents"))
		assert.Equal(t, "accepted", customer.get(t, "/api/v1/requests/"+id).str("status"), "nothing happened")

		cancelled := customer.post(t, path, map[string]any{"reason_id": reason, "expected_fee_cents": fee})
		require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
		assert.EqualValues(t, fee, cancelled.num("cancel_fee_cents"))

		paid := customer.post(t, "/api/v1/requests/"+id+"/payments", map[string]any{"method": "gpo", "phone_number": "900000000"})
		require.Equal(t, http.StatusCreated, paid.status, paid.text)
		assert.EqualValues(t, fee, paid.num("amount_cents"), "the customer pays the fee and not the fare")
		env.appypay.settle(paid.str("code"), "Success", 0)
		require.Eventually(t, func() bool { return customer.get(t, "/api/v1/requests/"+id).str("payment_status") == "paid" }, 15*time.Second, 100*time.Millisecond)
		_ = driver
	})

	t.Run("when the driver cancels the customer pays nothing and it counts for the driver", func(t *testing.T) {
		customer, driver, id := assignedTaxi(t)
		_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET accepted_at = now() - interval '600 seconds' WHERE id = $1`, id)
		require.NoError(t, err)
		reason := cancelReason(t, driver, "provider", "taxi", "vehicle_problem")
		assert.Equal(t, "REASON_NOT_APPLICABLE", driver.post(t, "/api/v1/requests/"+id+"/cancel", map[string]any{"reason_id": cancelReason(t, driver, "customer", "taxi", "changed_plans")}).code())
		cancelled := driver.post(t, "/api/v1/requests/"+id+"/cancel", map[string]any{"reason_id": reason})
		require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
		assert.EqualValues(t, 0, cancelled.num("cancel_fee_cents"))
		var count int
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT cancelled_requests FROM providers WHERE user_id = $1`, driver.id).Scan(&count))
		assert.Equal(t, 1, count)
		_ = customer
	})

	t.Run("the operator sees the reports by reason", func(t *testing.T) {
		admin := staffToken(t)
		report := admin.get(t, "/api/v1/admin/reports/cancellations?scope=customer_cancel&category=taxi")
		require.Equal(t, http.StatusOK, report.status, report.text)
		found := map[string]float64{}
		for _, item := range report.body["items"].([]any) {
			row := item.(map[string]any)
			found[row["code"].(string)] = row["total"].(float64)
		}
		assert.GreaterOrEqual(t, found["changed_plans"], 1.0)
		assert.Equal(t, "INVALID_REPORT_SCOPE", admin.get(t, "/api/v1/admin/reports/cancellations?scope=document").code())
		assert.Equal(t, http.StatusForbidden, newCustomer(t).get(t, "/api/v1/admin/reports/cancellations?scope=customer_cancel").status)
	})
}

func TestScheduledCancellationRespectsTheWindowOfTheService(t *testing.T) {
	setPolicy(t, "removals", "pricing_mode = 'quote', offer_mode = 'quote', ranking = 'customer_choice'")
	clearOnlineProviders(t)
	admin := staffToken(t)
	customer := newCustomer(t)
	removals := serviceBySlug(t, "removals")
	rules := admin.get(t, "/api/v1/admin/cancellation-rules")
	require.Equal(t, http.StatusOK, rules.status, rules.text)
	var removalRule map[string]any
	for _, item := range rules.body["items"].([]any) {
		if rule := item.(map[string]any); rule["service_id"] == removals {
			removalRule = rule
		}
	}
	require.NotNil(t, removalRule)
	assert.EqualValues(t, 86_400, removalRule["scheduled_free_seconds"], "free until 24 h before")
	assert.Equal(t, true, removalRule["charge_without_provider"])

	soon := time.Now().Add(12 * time.Hour).UTC().Truncate(15 * time.Minute).Format(time.RFC3339)
	far := time.Now().Add(72 * time.Hour).UTC().Truncate(15 * time.Minute).Format(time.RFC3339)
	inWindow := requestCategory(t, customer, "removals", map[string]any{"destination": target, "scheduled_for": far})
	preview := customer.get(t, "/api/v1/requests/"+inWindow.str("id")+"/cancellation")
	assert.EqualValues(t, 0, preview.num("fee_cents"), "72 h before is free")
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", inWindow.str("id")).status)

	late := requestCategory(t, customer, "removals", map[string]any{"destination": target, "scheduled_for": soon})
	preview = customer.get(t, "/api/v1/requests/"+late.str("id")+"/cancellation")
	assert.EqualValues(t, removalRule["fee_cents"], preview.num("fee_cents"), "12 h before pays the fee, even without a provider")

	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, "/api/v1/admin/cancellation-rules/"+removals, map[string]any{"fee_cents": 99_999_999_999_999}, nil).status)
	changed := admin.do(t, http.MethodPut, "/api/v1/admin/cancellation-rules/"+removals, map[string]any{"fee_cents": 1000, "scheduled_free_seconds": 86_400, "charge_without_provider": true}, nil)
	require.Equal(t, http.StatusOK, changed.status, changed.text)
	t.Cleanup(func() {
		admin.do(t, http.MethodPut, "/api/v1/admin/cancellation-rules/"+removals, map[string]any{"fee_cents": removalRule["fee_cents"], "scheduled_free_seconds": 86_400, "charge_without_provider": true}, nil)
	})
	preview = customer.get(t, "/api/v1/requests/"+late.str("id")+"/cancellation")
	assert.EqualValues(t, 1000, preview.num("fee_cents"), "the backoffice changes the fee")
	assert.Equal(t, http.StatusForbidden, customer.do(t, http.MethodPut, "/api/v1/admin/cancellation-rules/"+removals, map[string]any{"fee_cents": 0}, nil).status)
}

func TestARefusedOfferKeepsItsReason(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	requestTaxi(t, customer)
	offer := stream.waitFor(t, "request.offer", nil)
	path := "/api/v1/offers/" + offer.Data["offer_id"].(string) + "/reject"

	assert.Equal(t, http.StatusUnprocessableEntity, driver.post(t, path, map[string]any{}).status, "a reason is mandatory")
	assert.Equal(t, "REASON_NOT_APPLICABLE", driver.post(t, path, map[string]any{"reason_id": uuid.NewString()}).code())
	reason := cancelReason(t, driver, "offer", "taxi", "too_far")
	require.Equal(t, http.StatusNoContent, driver.post(t, path, map[string]any{"reason_id": reason, "note": "Do outro lado da cidade"}).status)

	var stored, note string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT reject_reason_id, reject_note FROM request_offers WHERE id = $1`, offer.Data["offer_id"]).Scan(&stored, &note))
	assert.Equal(t, reason, stored)
	assert.Equal(t, "Do outro lado da cidade", note)
	report := staffToken(t).get(t, "/api/v1/admin/reports/cancellations?scope=offer_reject&category=taxi")
	require.Equal(t, http.StatusOK, report.status)
	assert.Contains(t, report.text, "too_far")
}
