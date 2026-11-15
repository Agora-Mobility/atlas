//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCharge struct {
	ID         string
	MerchantID string
	Amount     float64
	Status     string
	Method     string
}

type appypayStub struct {
	server    *httptest.Server
	mu        sync.Mutex
	charges   map[string]*stubCharge
	failNext  atomic.Bool
	refunds   atomic.Int64
	lastPhone atomic.Value
}

func newAppyPayStub() *appypayStub {
	stub := &appypayStub{charges: map[string]*stubCharge{}}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.serve))
	return stub
}

func (s *appypayStub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.URL.Path == "/token":
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":"3599","access_token":"e2e"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/charges":
		s.createCharge(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/charges":
		for _, charge := range s.charges {
			if charge.MerchantID == r.URL.Query().Get("merchantTransactionId") {
				_ = json.NewEncoder(w).Encode(map[string]any{"payments": []any{chargeBody(charge)}})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"payments": []any{}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/charges/"):
		charge, known := s.charges[strings.TrimPrefix(r.URL.Path, "/charges/")]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"payment": chargeBody(charge)})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/refunds/"):
		s.refunds.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": uuid.NewString(), "responseStatus": map[string]any{"successful": true, "code": 100}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func chargeBody(charge *stubCharge) map[string]any {
	return map[string]any{"id": charge.ID, "merchantTransactionId": charge.MerchantID, "amount": charge.Amount, "status": charge.Status}
}

func (s *appypayStub) createCharge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Amount      float64 `json:"amount"`
		Merchant    string  `json:"merchantTransactionId"`
		Method      string  `json:"paymentMethod"`
		PaymentInfo struct {
			Phone string `json:"phoneNumber"`
		} `json:"paymentInfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if body.PaymentInfo.Phone == "900000003" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"responseStatus":{"successful":false,"code":722,"message":"Pagamento recusado"}}`))
		return
	}
	s.lastPhone.Store(body.PaymentInfo.Phone)
	charge := &stubCharge{ID: uuid.NewString(), MerchantID: body.Merchant, Amount: body.Amount, Status: "Pending", Method: body.Method}
	s.charges[charge.ID] = charge
	if s.failNext.CompareAndSwap(true, false) {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	status := map[string]any{"successful": true, "status": "Pending", "code": 101, "message": "Aceite"}
	if strings.HasPrefix(body.Method, "REF_") {
		status["reference"] = map[string]any{"referenceNumber": "968481213", "entity": "00348", "dueDate": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)}
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": charge.ID, "responseStatus": status})
}

func (s *appypayStub) settle(merchantID, status string, amount float64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, charge := range s.charges {
		if charge.MerchantID == merchantID {
			charge.Status = status
			if amount > 0 {
				charge.Amount = amount
			}
			return charge.ID
		}
	}
	return ""
}

func completedRide(t *testing.T, customer *person, method string) (driver *person, requestID string, fare int64) {
	t.Helper()
	clearOnlineProviders(t)
	env.googleMaps.down.Store(false)
	driver = newProvider(t, "taxi", point{-8.8400, 13.2350})
	stream := driver.listen(t, "")
	defer stream.stop()
	estimateID, serviceID := estimateFor(t, customer, "taxi")
	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{
		"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target, "payment_method": method,
	}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, method, created.str("payment_method"))
	assert.Equal(t, "unpaid", created.str("payment_status"))
	requestID = created.str("id")
	offer := stream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == requestID })
	assert.Equal(t, method, offer.Data["payment_method"], "the driver knows how the customer pays")
	accepted := acceptOffer(t, driver, offer)
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		res := driver.post(t, "/api/v1/requests/"+requestID+"/"+step, nil)
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
	return driver, requestID, customer.get(t, "/api/v1/requests/"+requestID).num("fare_cents")
}

func grantRefunds(t *testing.T, staffEmail string) {
	t.Helper()
	_, err := env.owner.Exec(context.Background(), `INSERT INTO membership_roles (membership_id, role_id)
		SELECT m.id, r.id FROM memberships m JOIN users u ON u.id = m.user_id JOIN roles r ON r.scope = 'platform' AND r.code = 'finance'
		WHERE u.email = $1 ON CONFLICT DO NOTHING`, staffEmail)
	require.NoError(t, err)
	keys, err := env.redisDirect.Keys(context.Background(), "*perm*").Result()
	require.NoError(t, err)
	if len(keys) > 0 {
		require.NoError(t, env.redisDirect.Del(context.Background(), keys...).Err())
	}
}

func requestPaymentStatus(t *testing.T, customer *person, requestID string) string {
	t.Helper()
	return customer.get(t, "/api/v1/requests/"+requestID).str("payment_status")
}

func webhook(t *testing.T, payload map[string]any) int {
	t.Helper()
	anonymous := &person{api: env.api.baseURL, client: customerClient}
	return anonymous.post(t, "/api/v1/webhooks/appypay", payload).status
}

func TestCashIsConfirmedByTheDriverOnly(t *testing.T) {
	customer := newCustomer(t)
	driver, requestID, fare := completedRide(t, customer, "cash")
	path := "/api/v1/requests/" + requestID + "/payments"

	assert.Equal(t, "WRONG_ACCOUNT_TYPE", customer.post(t, path+"/cash", nil).code())
	other := newProvider(t, "taxi", point{-8.9, 13.3})
	assert.Equal(t, "REQUEST_NOT_FOUND", other.post(t, path+"/cash", nil).code())

	paid := driver.post(t, path+"/cash", nil)
	require.Equal(t, http.StatusOK, paid.status, paid.text)
	assert.Equal(t, "paid", paid.str("status"))
	assert.Equal(t, "cash", paid.str("method"))
	assert.Equal(t, fare, paid.num("amount_cents"))
	assert.Equal(t, "paid", requestPaymentStatus(t, customer, requestID))

	again := driver.post(t, path+"/cash", nil)
	assert.Equal(t, paid.str("id"), again.str("id"), "confirming twice changes nothing")
	assert.Equal(t, "ALREADY_PAID", customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"}).code())

	list := customer.get(t, path)
	assert.Len(t, list.body["items"], 1)
	assert.Equal(t, http.StatusNotFound, newCustomer(t).get(t, path).status, "other customers see nothing")
}

func TestGPOIsPaidOnlyWhenTheGatewayConfirms(t *testing.T) {
	customer := newCustomer(t)
	stream := customer.listen(t, "")
	defer stream.stop()
	driver, requestID, fare := completedRide(t, customer, "gpo")
	path := "/api/v1/requests/" + requestID + "/payments"

	pending := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "+244 900 000 000"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	assert.Equal(t, "pending", pending.str("status"))
	assert.Equal(t, fare, pending.num("amount_cents"), "the amount comes from the request")
	assert.Equal(t, "*********000", pending.str("payer_phone"))
	assert.Equal(t, "244900000000", env.appypay.lastPhone.Load())
	code := pending.str("code")
	assert.Len(t, code, 15)

	repeated := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"})
	assert.Equal(t, http.StatusOK, repeated.status)
	assert.Equal(t, pending.str("id"), repeated.str("id"), "a retry never charges twice")
	assert.Equal(t, "PAYMENT_PENDING", customer.post(t, path, map[string]string{"method": "reference"}).code())
	assert.Equal(t, "pending", requestPaymentStatus(t, customer, requestID))

	estimateID, serviceID := estimateFor(t, customer, "taxi")
	blocked := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "PAYMENT_OUTSTANDING", blocked.code(), "a completed request stays to pay first")
	assert.Equal(t, map[string]any{"request_id": requestID, "reason": "completed_request"}, blocked.errorData())

	chargeID := env.appypay.settle(code, "Pending", 0)
	assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": chargeID, "merchantTransactionId": code, "amount": fare, "responseStatus": map[string]any{"successful": true, "status": "Success"}}))
	assert.Equal(t, "pending", requestPaymentStatus(t, customer, requestID), "a forged notification changes nothing")
	assert.Equal(t, http.StatusBadRequest, webhook(t, map[string]any{}))
	assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": uuid.NewString()}), "an unknown charge is ignored")

	env.appypay.settle(code, "Success", 0)
	assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": chargeID, "merchantTransactionId": code}))
	assert.Equal(t, "paid", requestPaymentStatus(t, customer, requestID))
	event := stream.waitFor(t, "request.payment", func(event sseEvent) bool { return event.Data["status"] == "paid" })
	assert.Equal(t, requestID, event.Data["request_id"])

	driverView := driver.get(t, path)
	item := driverView.body["items"].([]any)[0].(map[string]any)
	assert.Nil(t, item["payer_phone"], "the driver does not see the phone")

	again := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, http.StatusCreated, again.status, again.text)
	cancelled := cancelAs(t, customer, "customer", again.str("id"))
	assert.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
}

func TestReconcilerConfirmsWithoutAWebhook(t *testing.T) {
	customer := newCustomer(t)
	_, requestID, _ := completedRide(t, customer, "gpo")
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	env.appypay.settle(pending.str("code"), "Success", 0)
	assert.Eventually(t, func() bool { return requestPaymentStatus(t, customer, requestID) == "paid" }, 10*time.Second, 100*time.Millisecond)
}

func TestDeclinedAndFailedPaymentsCanBeRetried(t *testing.T) {
	customer := newCustomer(t)
	_, requestID, _ := completedRide(t, customer, "gpo")
	path := "/api/v1/requests/" + requestID + "/payments"

	declined := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000003"})
	require.Equal(t, http.StatusCreated, declined.status, declined.text)
	assert.Equal(t, "failed", declined.str("status"))
	assert.Equal(t, "Pagamento recusado", declined.str("failure_message"))
	assert.Equal(t, "unpaid", requestPaymentStatus(t, customer, requestID))

	second := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, second.status, second.text)
	assert.Equal(t, "pending", second.str("status"))
	env.appypay.settle(second.str("code"), "Failed", 0)
	assert.Eventually(t, func() bool { return requestPaymentStatus(t, customer, requestID) == "unpaid" }, 10*time.Second, 100*time.Millisecond)

	third := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, third.status, third.text)
	assert.NotEqual(t, second.str("code"), third.str("code"), "every attempt has its own code")
	env.appypay.settle(third.str("code"), "Success", 0)
	assert.Eventually(t, func() bool { return requestPaymentStatus(t, customer, requestID) == "paid" }, 10*time.Second, 100*time.Millisecond)
}

func TestAChargeWithTheWrongAmountIsNotAccepted(t *testing.T) {
	customer := newCustomer(t)
	_, requestID, _ := completedRide(t, customer, "gpo")
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	chargeID := env.appypay.settle(pending.str("code"), "Success", 1)
	assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": chargeID}))
	assert.Equal(t, "pending", requestPaymentStatus(t, customer, requestID))
}

func TestAnUnansweredChargeIsFoundByItsCode(t *testing.T) {
	customer := newCustomer(t)
	_, requestID, _ := completedRide(t, customer, "gpo")
	env.appypay.failNext.Store(true)
	pending := customer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	assert.Equal(t, "pending", pending.str("status"), "without an answer the outcome stays open")
	env.appypay.settle(pending.str("code"), "Success", 0)
	assert.Eventually(t, func() bool { return requestPaymentStatus(t, customer, requestID) == "paid" }, 10*time.Second, 100*time.Millisecond)
}

func TestAPaymentNobodyApprovesExpires(t *testing.T) {
	customer := newCustomer(t)
	_, requestID, _ := completedRide(t, customer, "gpo")
	path := "/api/v1/requests/" + requestID + "/payments"
	pending := customer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, pending.status, pending.text)
	_, err := env.owner.Exec(context.Background(), `UPDATE payments SET expires_at = now() - interval '1 minute' WHERE id = $1`, pending.str("id"))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return requestPaymentStatus(t, customer, requestID) == "unpaid" }, 10*time.Second, 100*time.Millisecond)
	item := customer.get(t, path).body["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "expired", item["status"])

	env.appypay.settle(pending.str("code"), "Success", 0)
	assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"merchantTransactionId": pending.str("code")}))
	assert.Equal(t, "paid", requestPaymentStatus(t, customer, requestID), "money that arrives late is still recorded")
}

func TestPaymentRulesAndBackoffice(t *testing.T) {
	admin := staffToken(t)
	grantRefunds(t, "admin-e2e@agora.ao")
	var taxiCategory string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id FROM service_categories WHERE slug = 'taxi'`).Scan(&taxiCategory))
	methods := func(person *person) []string {
		list := person.get(t, "/api/v1/payment-methods?category=taxi")
		require.Equal(t, http.StatusOK, list.status, list.text)
		out := []string{}
		for _, item := range list.body["items"].([]any) {
			out = append(out, item.(map[string]any)["method"].(string))
		}
		return out
	}

	customer := newCustomer(t)
	assert.Equal(t, []string{"cash", "gpo", "wallet"}, methods(customer), "taxi has no reference by default")
	estimateID, serviceID := estimateFor(t, customer, "taxi")
	refused := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target, "payment_method": "reference"}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "PAYMENT_METHOD_UNAVAILABLE", refused.code())

	assert.Equal(t, "PERMISSION_DENIED", customer.get(t, "/api/v1/admin/payments").code())
	toggle := func(enabled bool) {
		res := admin.do(t, http.MethodPut, "/api/v1/admin/payment-methods", map[string]any{"category_id": taxiCategory, "method": "reference", "is_enabled": enabled}, nil)
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
	toggle(true)
	defer toggle(false)
	assert.Equal(t, []string{"cash", "gpo", "reference", "wallet"}, methods(customer))

	t.Run("the method changes until the request is completed", func(t *testing.T) {
		buyer := newCustomer(t)
		_, requestID, _ := completedRide(t, buyer, "gpo")
		path := "/api/v1/requests/" + requestID + "/payment-method"
		assert.Equal(t, "PAYMENT_METHOD_LOCKED", buyer.do(t, http.MethodPut, path, map[string]string{"method": "cash"}, nil).code(), "a debt does not turn into cash")
		changed := buyer.do(t, http.MethodPut, path, map[string]string{"method": "reference"}, nil)
		require.Equal(t, http.StatusOK, changed.status, changed.text)
		assert.Equal(t, "reference", buyer.get(t, "/api/v1/requests/"+requestID).str("payment_method"))
		assert.Equal(t, http.StatusNotFound, newCustomer(t).do(t, http.MethodPut, path, map[string]string{"method": "gpo"}, nil).status)
	})

	t.Run("a request is paid only when completed", func(t *testing.T) {
		clearOnlineProviders(t)
		buyer := newCustomer(t)
		created := requestTaxi(t, buyer)
		path := "/api/v1/requests/" + created.str("id") + "/payments"
		assert.Equal(t, "REQUEST_NOT_PAYABLE", buyer.post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"}).code())
		assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, path, map[string]string{"method": "gpo", "phone_number": "900000000"}).status)
		require.Equal(t, http.StatusOK, cancelAs(t, buyer, "customer", created.str("id")).status)
	})

	t.Run("a reference is paid and refunded to the wallet", func(t *testing.T) {
		buyer := newCustomer(t)
		_, requestID, fare := completedRide(t, buyer, "reference")
		path := "/api/v1/requests/" + requestID + "/payments"
		pending := buyer.post(t, path, map[string]string{"method": "reference"})
		require.Equal(t, http.StatusCreated, pending.status, pending.text)
		reference := pending.body["reference"].(map[string]any)
		assert.Equal(t, "968481213", reference["number"])
		assert.Equal(t, "00348", reference["entity"])
		assert.NotEmpty(t, reference["due_at"])
		chargeID := env.appypay.settle(pending.str("code"), "Success", 0)
		assert.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": chargeID}))
		assert.Equal(t, "paid", requestPaymentStatus(t, buyer, requestID))
		before := env.appypay.refunds.Load()
		refund := refundAs(t, admin, "/api/v1/admin/payments/"+pending.str("id")+"/refund", map[string]any{"reason": "Teste"})
		require.Equal(t, http.StatusOK, refund.status, refund.text)
		assert.Equal(t, before, env.appypay.refunds.Load(), "AppyPay only refunds Express")
		assert.Equal(t, fare, buyer.get(t, "/api/v1/wallet").num("balance_cents"), "the reference comes back to the wallet")
		assert.Equal(t, fare, pending.num("amount_cents"))
	})

	t.Run("gpo is refunded in parts up to the whole", func(t *testing.T) {
		buyer := newCustomer(t)
		_, requestID, fare := completedRide(t, buyer, "gpo")
		pending := buyer.post(t, "/api/v1/requests/"+requestID+"/payments", map[string]string{"method": "gpo", "phone_number": "900000000"})
		require.Equal(t, http.StatusCreated, pending.status, pending.text)
		chargeID := env.appypay.settle(pending.str("code"), "Success", 0)
		require.Equal(t, http.StatusNoContent, webhook(t, map[string]any{"id": chargeID}))

		refundPath := "/api/v1/admin/payments/" + pending.str("id") + "/refund"
		assert.Equal(t, "REFUND_TOO_LARGE", refundAs(t, admin, refundPath, map[string]any{"amount_cents": fare + 1, "reason": "Teste"}).code())
		before := env.appypay.refunds.Load()
		partial := refundAs(t, admin, refundPath, map[string]any{"amount_cents": 1000, "reason": "Parcial"})
		require.Equal(t, http.StatusOK, partial.status, partial.text)
		assert.EqualValues(t, 1000, partial.num("refunded_cents"))
		assert.Equal(t, "paid", requestPaymentStatus(t, buyer, requestID))
		full := refundAs(t, admin, refundPath, map[string]any{"reason": "O resto"})
		require.Equal(t, http.StatusOK, full.status, full.text)
		assert.Equal(t, fare, full.num("refunded_cents"))
		assert.Equal(t, "refunded", requestPaymentStatus(t, buyer, requestID))
		assert.Equal(t, before+2, env.appypay.refunds.Load())
		assert.Equal(t, "REFUND_TOO_LARGE", refundAs(t, admin, refundPath, map[string]any{"reason": "De novo"}).code())

		listed := admin.get(t, "/api/v1/admin/payments?status=paid&method=gpo&category=taxi&request_id="+requestID)
		require.Equal(t, http.StatusOK, listed.status, listed.text)
		assert.EqualValues(t, 1, listed.num("total"))
		assert.Equal(t, http.StatusBadRequest, admin.get(t, "/api/v1/admin/payments?request_id=x").status)
		detail := admin.get(t, "/api/v1/admin/payments/"+pending.str("id"))
		require.Equal(t, http.StatusOK, detail.status, detail.text)
		assert.Len(t, detail.body["refunds"], 2)
		assert.NotEmpty(t, detail.body["events"])
		assert.Equal(t, "PAYMENT_NOT_FOUND", admin.get(t, "/api/v1/admin/payments/"+uuid.NewString()).code())
	})
}
