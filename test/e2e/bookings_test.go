//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shop struct {
	id, serviceID string
	owner         *partner
}

func newShop(t *testing.T, capacity int) *shop {
	t.Helper()
	owner := newPartner(t)
	body := map[string]any{"name": "Oficina " + uuid.NewString()[:8], "currency": "AOA", "offers_home_service": true, "home_service_fee_cents": 300_000, "address": map[string]any{"lat": -8.84, "lng": 13.24, "line": "Talatona"}}
	created := owner.post(t, "/api/v1/fleet/workshops", body)
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	service := owner.post(t, "/api/v1/fleet/workshops/"+id+"/services", map[string]any{"name": "Revisão", "price_cents": 3_500_000, "duration_minutes": 120})
	require.Equal(t, http.StatusCreated, service.status, service.text)
	hours := make([]map[string]int, 0, 7)
	for day := range 7 {
		hours = append(hours, map[string]int{"weekday": day, "open_minute": 0, "close_minute": 1440})
	}
	schedule := owner.do(t, http.MethodPut, "/api/v1/fleet/workshops/"+id+"/schedule", map[string]any{"capacity": capacity, "hours": hours}, nil)
	require.Equal(t, http.StatusNoContent, schedule.status, schedule.text)
	return &shop{id: id, serviceID: service.str("id"), owner: owner}
}

func (s *shop) firstSlot(t *testing.T, customer *person) string {
	t.Helper()
	day := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	res := customer.get(t, fmt.Sprintf("/api/v1/workshops/%s/availability?service_ids=%s&from=%s", s.id, s.serviceID, day))
	require.Equal(t, http.StatusOK, res.status, res.text)
	first := res.body["days"].([]any)[0].(map[string]any)
	require.Equal(t, "free", first["status"])
	return first["slots"].([]any)[0].(map[string]any)["starts_at"].(string)
}

func customerVehicle(t *testing.T, customer *person) string {
	t.Helper()
	res := customer.post(t, "/api/v1/users/me/vehicles", vehiclePayload("LD-"+uuid.NewString()[:6]))
	require.Equal(t, http.StatusCreated, res.status, res.text)
	return res.str("id")
}

func (s *shop) book(t *testing.T, customer *person, slot string, method string) result {
	t.Helper()
	return customer.do(t, http.MethodPost, "/api/v1/bookings", map[string]any{"workshop_id": s.id, "service_ids": []string{s.serviceID}, "vehicle_id": customerVehicle(t, customer), "starts_at": slot, "payment_method": method}, map[string]string{"Idempotency-Key": uuid.NewString()})
}

func (s *shop) move(t *testing.T, id, status string) result {
	t.Helper()
	return s.owner.post(t, "/api/v1/fleet/bookings/"+id+"/status", map[string]string{"status": status})
}

func TestBookingTheLastPlaceOfASlotOnlyWorksOnce(t *testing.T) {
	workshop := newShop(t, 1)
	first := newCustomer(t)
	slot := workshop.firstSlot(t, first)
	rivals := make([]*person, 6)
	vehicles := make([]string, 6)
	for i := range rivals {
		rivals[i] = newCustomer(t)
		vehicles[i] = customerVehicle(t, rivals[i])
	}
	results := make([]result, 6)
	var wg sync.WaitGroup
	for i := range rivals {
		wg.Go(func() {
			results[i] = rivals[i].do(t, http.MethodPost, "/api/v1/bookings", map[string]any{"workshop_id": workshop.id, "service_ids": []string{workshop.serviceID}, "vehicle_id": vehicles[i], "starts_at": slot, "payment_method": "gpo"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		})
	}
	wg.Wait()
	won, full := 0, 0
	for i := range results {
		switch {
		case results[i].status == http.StatusCreated:
			won++
		case results[i].code() == "BOOKING_SLOT_FULL":
			full++
		}
	}
	assert.Equal(t, 1, won)
	assert.Equal(t, 5, full)
	var stored int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM bookings WHERE workshop_id = $1 AND status <> 'cancelled'`, workshop.id).Scan(&stored))
	assert.Equal(t, 1, stored)
}

func TestABookingGoesThroughItsStatesAndIsChargedOnlyAfterApproval(t *testing.T) {
	workshop := newShop(t, 2)
	customer, intruder, other := newCustomer(t), newCustomer(t), newPartner(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, requestID := created.str("id"), created.str("request_id")
	assert.Regexp(t, `^MK-\d+$`, created.str("code"))
	assert.Equal(t, "confirmed", created.str("status"))
	assert.EqualValues(t, 3_500_000, created.num("total_cents"), "the price comes from the workshop")

	assert.Equal(t, http.StatusNotFound, intruder.get(t, "/api/v1/bookings/"+id).status)
	assert.Equal(t, http.StatusNotFound, other.get(t, "/api/v1/fleet/bookings/"+id).status, "another partner")
	assert.Equal(t, http.StatusNotFound, other.post(t, "/api/v1/fleet/bookings/"+id+"/status", map[string]string{"status": "vehicle_received"}).status)
	assert.Equal(t, "BOOKING_STATE_INVALID", workshop.move(t, id, "ready").code(), "no skipping")
	assert.Equal(t, "REQUEST_NOT_PAYABLE", customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]any{"method": "gpo"}).code())

	require.Equal(t, http.StatusOK, workshop.move(t, id, "vehicle_received").status)
	require.Equal(t, http.StatusOK, workshop.move(t, id, "in_service").status)
	extra := workshop.owner.post(t, "/api/v1/fleet/bookings/"+id+"/extras", map[string]any{"description": "Pastilhas", "amount_cents": 1_000_000})
	require.Equal(t, http.StatusCreated, extra.status, extra.text)
	declined := workshop.owner.post(t, "/api/v1/fleet/bookings/"+id+"/extras", map[string]any{"description": "Filtro", "amount_cents": 500_000})
	extras := declined.body["extras"].([]any)
	assert.Equal(t, "BOOKING_EXTRAS_PENDING", workshop.move(t, id, "ready").code())

	answer := func(who *person, extraID string, approve bool) result {
		return who.post(t, "/api/v1/bookings/"+id+"/extras/"+extraID+"/answer", map[string]bool{"approve": approve})
	}
	assert.Equal(t, http.StatusNotFound, answer(intruder, extras[0].(map[string]any)["id"].(string), true).status)
	assert.EqualValues(t, 4_500_000, answer(customer, extras[0].(map[string]any)["id"].(string), true).num("total_cents"))
	assert.EqualValues(t, 4_500_000, answer(customer, extras[1].(map[string]any)["id"].(string), false).num("total_cents"), "a declined extra stays out")

	require.Equal(t, http.StatusOK, workshop.move(t, id, "ready").status)
	assert.Equal(t, "REQUEST_NOT_PAYABLE", customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]any{"method": "gpo"}).code(), "not before the approval")
	approved := customer.post(t, "/api/v1/bookings/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)
	assert.EqualValues(t, 4_500_000, approved.num("amount_due_cents"))
	assert.Equal(t, "BOOKING_STATE_INVALID", customer.post(t, "/api/v1/bookings/"+id+"/approve", nil).code(), "only once")
}

func TestAPromotionAppliesAloneOncePerCustomer(t *testing.T) {
	admin := staffToken(t)
	workshop := newShop(t, 5)
	now := time.Now().UTC()
	promotion := map[string]any{"description": "Primeira revisão -15%", "discount_bps": 1500, "valid_from": now.Add(-time.Hour), "valid_until": now.Add(24 * time.Hour), "per_customer_limit": 1, "workshop_id": workshop.id, "workshop_service_id": workshop.serviceID, "auto_apply": true}
	require.Equal(t, http.StatusCreated, admin.post(t, "/api/v1/admin/vouchers", promotion).status)
	customer := newCustomer(t)
	slot := workshop.firstSlot(t, customer)
	first := workshop.book(t, customer, slot, "gpo")
	require.Equal(t, http.StatusCreated, first.status, first.text)
	assert.EqualValues(t, 525_000, first.num("discount_cents"))
	assert.EqualValues(t, 2_975_000, first.num("total_cents"))
	second := workshop.book(t, customer, slot, "gpo")
	assert.Zero(t, second.num("discount_cents"), "once per customer")
	assert.EqualValues(t, 525_000, workshop.book(t, newCustomer(t), slot, "gpo").num("discount_cents"), "another customer still has it")
}

func TestRequestsOfMaintenanceAreRefusedAndTheBillingRequestIsNotCancelledApart(t *testing.T) {
	workshop := newShop(t, 2)
	customer := newCustomer(t)
	var service string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id FROM services WHERE slug = 'maintenance'`).Scan(&service))
	refused := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": service, "pickup": pickup}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "USE_BOOKINGS", refused.code())

	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	reason := cancelReason(t, customer, "customer", "maintenance", "changed_date")
	assert.Equal(t, "BOOKING_MANAGED", customer.post(t, "/api/v1/requests/"+created.str("request_id")+"/cancel", map[string]any{"reason_id": reason}).code())
	cancelled := customer.post(t, "/api/v1/bookings/"+created.str("id")+"/cancel", map[string]any{"reason_id": reason})
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	assert.Equal(t, "cancelled", cancelled.str("status"))
	assert.Equal(t, "cancelled", customer.get(t, "/api/v1/requests/"+created.str("request_id")).str("status"))
}

func TestAClosedDayIsNotBookable(t *testing.T) {
	workshop := newShop(t, 2)
	customer := newCustomer(t)
	slot := workshop.firstSlot(t, customer)
	day := slot[:10]
	require.Equal(t, http.StatusNoContent, workshop.owner.post(t, "/api/v1/fleet/workshops/"+workshop.id+"/closures", map[string]string{"date": day}).status)
	assert.Equal(t, "BOOKING_SLOT_INVALID", workshop.book(t, customer, slot, "gpo").code())
	assert.Equal(t, http.StatusNotFound, newPartner(t).post(t, "/api/v1/fleet/workshops/"+workshop.id+"/closures", map[string]string{"date": day}).status, "another partner")
	require.Equal(t, http.StatusNoContent, workshop.owner.do(t, http.MethodDelete, "/api/v1/fleet/workshops/"+workshop.id+"/closures/"+day, nil, nil).status)
	assert.Equal(t, http.StatusCreated, workshop.book(t, customer, slot, "gpo").status)
}

func TestAWorkshopShowsItsPhotosWithTheFirstAsCover(t *testing.T) {
	workshop := newShop(t, 1)
	owner := workshop.owner
	path := "/api/v1/fleet/workshops/" + workshop.id + "/photos"
	front, inside := uploadPhoto(t, owner.person), uploadPhoto(t, owner.person)

	set := owner.do(t, http.MethodPut, path, map[string]any{"photo_ids": []string{inside, front}}, nil)
	require.Equal(t, http.StatusOK, set.status, set.text)
	gallery := set.body["items"].([]any)
	require.Len(t, gallery, 2)
	assert.Equal(t, inside, gallery[0].(map[string]any)["id"], "the order of the partner is kept")

	customer := newCustomer(t)
	detail := customer.get(t, "/api/v1/workshops/"+workshop.id+"?lat=-8.84&lng=13.24")
	require.Equal(t, http.StatusOK, detail.status, detail.text)
	photos := detail.body["photos"].([]any)
	require.Len(t, photos, 2)
	assert.NotEmpty(t, photos[0].(map[string]any)["url"])
	assert.NotEmpty(t, detail.str("cover_url"))

	search := customer.get(t, fmt.Sprintf("/api/v1/workshops?lat=-8.84&lng=13.24&search=%s", url.QueryEscape(detail.str("name"))))
	require.Equal(t, http.StatusOK, search.status, search.text)
	cards := search.body["items"].([]any)
	require.Len(t, cards, 1)
	assert.NotEmpty(t, cards[0].(map[string]any)["cover_url"], "the list shows the cover")

	kept := owner.do(t, http.MethodPut, path, map[string]any{"photo_ids": []string{front}}, nil)
	require.Equal(t, http.StatusOK, kept.status, kept.text)
	assert.Len(t, kept.body["items"], 1, "a photo left out leaves the gallery")
	assert.Equal(t, http.StatusUnprocessableEntity, owner.do(t, http.MethodPut, path, map[string]any{"photo_ids": []string{front, front}}, nil).status, "a photo appears once")

	other := newShop(t, 1)
	assert.Equal(t, "PHOTO_NOT_FOUND", other.owner.do(t, http.MethodPut, "/api/v1/fleet/workshops/"+other.id+"/photos", map[string]any{"photo_ids": []string{inside}}, nil).code(), "a partner uses only their own uploads")
	assert.Equal(t, http.StatusNotFound, other.owner.do(t, http.MethodPut, path, map[string]any{"photo_ids": []string{}}, nil).status, "nor changes another workshop")

	cleared := owner.do(t, http.MethodPut, path, map[string]any{"photo_ids": []string{}}, nil)
	require.Equal(t, http.StatusOK, cleared.status, cleared.text)
	assert.Empty(t, customer.get(t, "/api/v1/workshops/"+workshop.id+"?lat=-8.84&lng=13.24").str("cover_url"))
}
