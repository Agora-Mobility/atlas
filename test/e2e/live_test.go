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

func TestChatOnlyWorksDuringTheRequestAndArrivesBySSE(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	require.Equal(t, http.StatusNoContent, driver.do(t, http.MethodPut, "/api/v1/devices", map[string]string{"token": "driver-" + uuid.NewString(), "platform": "android"}, nil).status)
	driverStream := driver.listen(t, "")
	customer := newCustomer(t)
	customerStream := customer.listen(t, "")
	created := requestTaxi(t, customer)
	id := created.str("id")
	path := "/api/v1/requests/" + id + "/messages"

	assert.Equal(t, "CHAT_CLOSED", customer.post(t, path, map[string]any{"body": "Olá"}).code(), "no provider yet")
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, driverStream.waitFor(t, "request.offer", nil)).status)

	intruder, stranger := newCustomer(t), newProvider(t, "taxi", point{-8.85, 13.24})
	assert.Equal(t, http.StatusNotFound, intruder.post(t, path, map[string]any{"body": "Olá"}).status)
	assert.Equal(t, http.StatusNotFound, stranger.get(t, path).status)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"body": "   "}).status)

	sent := customer.post(t, path, map[string]any{"body": "Estou junto à farmácia"})
	require.Equal(t, http.StatusCreated, sent.status, sent.text)
	assert.Equal(t, "customer", sent.str("sender"))
	event := driverStream.waitFor(t, "request.message", nil)
	assert.Equal(t, "Estou junto à farmácia", event.Data["body"])
	require.Eventually(t, func() bool { return len(env.fcm.forMessage(sent.str("id"))) == 1 }, 5*time.Second, 20*time.Millisecond, "a push for the app in the background")
	assert.Equal(t, "request.message", env.fcm.forMessage(sent.str("id"))[0].Type)

	replies := driver.get(t, "/api/v1/quick-replies")
	require.Equal(t, http.StatusOK, replies.status, replies.text)
	items := replies.body["items"].([]any)
	require.NotEmpty(t, items)
	reply := items[0].(map[string]any)
	assert.Equal(t, "Estou a caminho", reply["label"], "the replies of the driver")
	quick := driver.post(t, path, map[string]any{"quick_reply_id": reply["id"]})
	require.Equal(t, http.StatusCreated, quick.status, quick.text)
	assert.Equal(t, "Estou a caminho", quick.str("body"))
	assert.Equal(t, "Estou a caminho", customerStream.waitFor(t, "request.message", nil).Data["body"])
	assert.Equal(t, "QUICK_REPLY_NOT_FOUND", driver.post(t, path, map[string]any{"quick_reply_id": uuid.NewString()}).code())
	assert.Equal(t, "QUICK_REPLY_NOT_FOUND", customer.post(t, path, map[string]any{"quick_reply_id": reply["id"]}).code(), "the customer does not send the replies of the driver")

	own := customer.get(t, "/api/v1/quick-replies")
	require.Equal(t, http.StatusOK, own.status, own.text)
	var labels []string
	for _, item := range own.body["items"].([]any) {
		labels = append(labels, item.(map[string]any)["label"].(string))
	}
	assert.Equal(t, []string{"Já estou à sua espera", "Quanto tempo falta?", "Já vi o carro, vou já"}, labels, "the replies of the customer")

	history := customer.get(t, path)
	require.Equal(t, http.StatusOK, history.status)
	assert.Len(t, history.body["items"], 2)

	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status)
	}
	assert.Equal(t, "CHAT_CLOSED", customer.post(t, path, map[string]any{"body": "Obrigado"}).code(), "the chat ends with the request")
	assert.Len(t, customer.get(t, path).body["items"], 2, "the history stays")
}

func TestMaskedCallsHideTheNumbersAndFailClearlyWhenTheProviderIsDown(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	path := "/api/v1/requests/" + id + "/call"
	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, path, nil).status)

	call := customer.post(t, path, nil)
	require.Equal(t, http.StatusOK, call.status, call.text)
	assert.Equal(t, "+244900111222", call.str("number"))
	assert.Equal(t, true, call.body["masked"])
	var driverPhone string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT phone FROM users WHERE id = $1`, driver.id).Scan(&driverPhone))
	assert.NotContains(t, call.text, driverPhone, "the number of the driver is not shared")
	env.calls.mu.Lock()
	assert.Equal(t, driverPhone, env.calls.last["to"], "the bridge is the one who dials")
	env.calls.mu.Unlock()

	env.calls.down.Store(true)
	defer env.calls.down.Store(false)
	assert.Equal(t, "CALLS_UNAVAILABLE", customer.post(t, path, nil).code())
	assert.Equal(t, http.StatusServiceUnavailable, customer.post(t, path, nil).status)
}

func TestASharedLinkIsPublicButLimited(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := askFor(t, customer, taxiQuote(t, customer, nil), map[string]any{"contact_phone": "+244923000321"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, stream.waitFor(t, "request.offer", nil)).status)

	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, "/api/v1/requests/"+id+"/share", nil).status)
	assert.Equal(t, http.StatusNotFound, driver.post(t, "/api/v1/requests/"+id+"/share", nil).status, "only the customer shares")
	share := customer.post(t, "/api/v1/requests/"+id+"/share", nil)
	require.Equal(t, http.StatusCreated, share.status, share.text)
	token := share.str("token")
	assert.Contains(t, share.str("url"), token)

	anonymous := &person{client: customerClient, api: env.api.baseURL}
	public := anonymous.get(t, "/api/v1/public/trips/"+token)
	require.Equal(t, http.StatusOK, public.status, public.text)
	assert.Equal(t, "Pessoa Teste", public.str("provider_name"))
	assert.Equal(t, "LD-45-21-AF", public.str("vehicle_plate"))
	assert.NotContains(t, public.text, "+244923000321", "no phone")
	assert.Equal(t, http.StatusNotFound, anonymous.get(t, "/api/v1/public/trips/"+uuid.NewString()).status)

	var stored string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT token_hash FROM request_shares WHERE request_id = $1 LIMIT 1`, id).Scan(&stored))
	assert.NotEqual(t, token, stored, "only the hash is stored")

	_, err := env.owner.Exec(context.Background(), `UPDATE request_shares SET expires_at = now() - interval '1 second' WHERE request_id = $1`, id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, anonymous.get(t, "/api/v1/public/trips/"+token).status, "it expires")

	again := customer.post(t, "/api/v1/requests/"+id+"/share", nil)
	require.Equal(t, http.StatusCreated, again.status)
	require.Equal(t, http.StatusNoContent, customer.do(t, http.MethodDelete, "/api/v1/requests/"+id+"/share", nil, nil).status)
	assert.Equal(t, http.StatusNotFound, anonymous.get(t, "/api/v1/public/trips/"+again.str("token")).status, "it can be revoked")
}

func TestETAComesFromTheRouteOfTheProvider(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	assert.Equal(t, http.StatusNotFound, newCustomer(t).get(t, "/api/v1/requests/"+id+"/eta").status)
	eta := customer.get(t, "/api/v1/requests/"+id+"/eta")
	require.Equal(t, http.StatusOK, eta.status, eta.text)
	assert.Equal(t, "google", eta.str("source"))
	assert.Equal(t, "pickup", eta.str("target"))
	assert.Positive(t, eta.num("duration_s"))
	assert.Equal(t, "polyline", eta.str("route_geometry"), "the app draws the way of the driver to the customer")

	toPickup := driver.get(t, "/api/v1/requests/"+id+"/eta")
	require.Equal(t, http.StatusOK, toPickup.status, toPickup.text)
	assert.Equal(t, "polyline", toPickup.str("route_geometry"), "the driver gets the same route")

	env.googleMaps.down.Store(true)
	defer env.googleMaps.down.Store(false)
	fallback := customer.get(t, "/api/v1/requests/"+id+"/eta")
	require.Equal(t, http.StatusOK, fallback.status, fallback.text)
	assert.Equal(t, "fallback", fallback.str("source"))
	assert.Empty(t, fallback.str("route_geometry"), "no route to draw without Google")
}

func TestSOSReachesTheOperatorAndStaysUntilResolved(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	admin := staffToken(t)
	path := "/api/v1/requests/" + id + "/sos"
	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, path, map[string]any{}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"lat": 999.0, "lng": 13.2}).status)

	raised := customer.post(t, path, map[string]any{"lat": -8.84, "lng": 13.23, "note": "Preciso de ajuda"})
	require.Equal(t, http.StatusCreated, raised.status, raised.text)
	repeat := customer.post(t, path, map[string]any{"note": "De novo"})
	assert.Equal(t, http.StatusOK, repeat.status, "a second press returns the open alert")
	assert.Equal(t, raised.str("id"), repeat.str("id"))
	assert.Equal(t, http.StatusCreated, driver.post(t, path, map[string]any{}).status, "the driver can raise it too")

	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/admin/sos").status)
	open := admin.get(t, "/api/v1/admin/sos?status=open")
	require.Equal(t, http.StatusOK, open.status, open.text)
	assert.Contains(t, open.text, raised.str("id"))

	require.Eventually(t, func() bool {
		var alerts int
		err := env.owner.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE event_type = 'requests.operator-alerted' AND aggregate_id = $1`, id).Scan(&alerts)
		return err == nil && alerts >= 1
	}, 10*time.Second, 50*time.Millisecond, "the operator is alerted")

	require.Equal(t, http.StatusNoContent, admin.post(t, "/api/v1/admin/sos/"+raised.str("id")+"/resolve", nil).status)
	assert.Equal(t, "SOS_NOT_FOUND", admin.post(t, "/api/v1/admin/sos/"+raised.str("id")+"/resolve", nil).code())
	assert.NotContains(t, admin.get(t, "/api/v1/admin/sos?status=open").text, raised.str("id"))
}

func TestTheCustomerSeesTheAvatarOfTheDriver(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	path := "/api/v1/requests/" + id
	before := customer.get(t, path)
	require.Equal(t, http.StatusOK, before.status, before.text)
	assert.NotContains(t, before.body["provider"], "avatar_url", "no avatar yet")

	set := driver.do(t, http.MethodPut, "/api/v1/users/me/avatar", map[string]string{"photo_id": uploadPhoto(t, driver)}, nil)
	require.Equal(t, http.StatusOK, set.status, set.text)

	after := customer.get(t, path)
	require.Equal(t, http.StatusOK, after.status, after.text)
	assert.NotEmpty(t, after.body["provider"].(map[string]any)["avatar_url"])
}
