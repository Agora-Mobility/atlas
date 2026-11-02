//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/realtime"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

func acceptOffer(t *testing.T, provider *person, offer sseEvent) result {
	t.Helper()
	return provider.post(t, "/api/v1/offers/"+offer.Data["offer_id"].(string)+"/accept", map[string]any{"version": offer.Data["request_version"]})
}

func ack(t *testing.T, provider *person, offer sseEvent) {
	t.Helper()
	res := provider.post(t, "/api/v1/offers/"+offer.Data["offer_id"].(string)+"/delivered", map[string]string{"channel": "sse"})
	require.Equal(t, http.StatusNoContent, res.status, res.text)
}

func clearOnlineProviders(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, pattern := range []string{"live:*", "offer:lock:*"} {
		keys, err := env.redisDirect.Keys(ctx, pattern).Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, env.redisDirect.Del(ctx, keys...).Err())
		}
	}
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET status = 'cancelled', next_action_at = NULL, cancel_reason = 'test isolation', needs_operator = false, version = version + 1 WHERE status NOT IN ('completed', 'cancelled', 'expired')`)
	require.NoError(t, err)
	_, err = env.owner.Exec(ctx, `UPDATE request_offers SET status = 'cancelled', next_action_at = NULL WHERE status IN ('sent', 'delivered', 'quoted')`)
	require.NoError(t, err)
}

func TestTaxiFullFlowWithFixedPriceTrackingAndHonoredFare(t *testing.T) {
	clearOnlineProviders(t)
	env.mapbox.down.Store(false)
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})
	driverStream := driver.listen(t, "")
	customer := newCustomer(t)
	customerStream := customer.listen(t, "")

	estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	option := estimate.body["items"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 171_500, option["total_cents"], "the 8 km and 25 min example of the proposal")
	assert.Equal(t, "mapbox", option["route_source"])

	key := uuid.NewString()
	payload := map[string]any{"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target}
	created := customer.do(t, http.MethodPost, "/api/v1/requests", payload, map[string]string{"Idempotency-Key": key})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, 171_500, created.num("fare_cents"))
	requestID := created.str("id")

	again := customer.do(t, http.MethodPost, "/api/v1/requests", payload, map[string]string{"Idempotency-Key": key})
	assert.Equal(t, http.StatusCreated, again.status)
	assert.Equal(t, "true", again.header.Get("Idempotent-Replayed"))
	assert.Equal(t, requestID, again.str("id"))

	offer := driverStream.waitFor(t, "request.offer", nil)
	assert.Less(t, len(offer.Raw), 2048)
	assert.Equal(t, "critical", offer.Data["class"])
	assert.Greater(t, offer.Data["expires_in_s"], 10.0)
	assert.EqualValues(t, 154_350, offer.Data["net_cents"], "the driver sees the net value")
	assert.NotContains(t, customerStream.ofType("request.offer"), offer, "the customer never sees offers")

	ack(t, driver, offer)
	accepted := acceptOffer(t, driver, offer)
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	assert.Equal(t, "mapbox", accepted.body["route"].(map[string]any)["source"])

	customerStream.waitForStatus(t, "accepted")
	view := customer.get(t, "/api/v1/requests/"+requestID)
	assert.Equal(t, "LD-45-21-AF", view.body["provider"].(map[string]any)["vehicle_plate"])

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+requestID+"/en-route", nil).status)
	customerStream.waitForStatus(t, "provider_en_route")

	moved := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.8395, 13.2345})}})
	require.Equal(t, http.StatusOK, moved.status, moved.text)
	assert.EqualValues(t, 7, moved.num("interval_s"), "on the way the cadence tightens")
	position := customerStream.waitFor(t, "request.position", nil)
	assert.Empty(t, position.ID, "positions are ephemeral and never move the cursor")
	assert.NotNil(t, position.Data["eta_s"])

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+requestID+"/arrive", nil).status)
	time.Sleep(3100 * time.Millisecond)
	started := driver.post(t, "/api/v1/requests/"+requestID+"/start", nil)
	require.Equal(t, http.StatusOK, started.status, started.text)
	assert.Greater(t, started.num("fare_cents"), int64(171_500), "waiting beyond the free period is charged")

	completed := driver.post(t, "/api/v1/requests/"+requestID+"/complete", nil)
	require.Equal(t, http.StatusOK, completed.status, completed.text)
	customerStream.waitForStatus(t, "completed")
	previous := 0.0
	for _, event := range customerStream.ofType("request.status") {
		version := event.Data["request_version"].(float64)
		assert.Greater(t, version, previous, "the events of one request arrive in order")
		previous = version
	}

	final := customer.get(t, "/api/v1/requests/"+requestID)
	adjustments := final.body["adjustments"].([]any)
	require.Len(t, adjustments, 1, "every change to the quoted price is recorded")
	first := adjustments[0].(map[string]any)
	assert.Equal(t, "waiting", first["reason"])
	assert.EqualValues(t, final.num("fare_cents")-171_500, first["amount_cents"], "the charged value is the quote plus what is recorded")

	var history, observations int
	ctx := context.Background()
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM service_request_events WHERE request_id = $1`, requestID).Scan(&history))
	assert.GreaterOrEqual(t, history, 7, "every transition is in the history")
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM eta_observations`).Scan(&observations))
	assert.GreaterOrEqual(t, observations, 1, "completed trips feed the own ETA table")

	active := customer.get(t, "/api/v1/requests/active")
	assert.Nil(t, active.body["request"])
	list := customer.get(t, "/api/v1/requests?category=taxi&status=completed")
	assert.EqualValues(t, 1, list.num("total"))
}

func TestCriterion1EventsGeneratedWhileDisconnectedArriveOnReconnect(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})
	customer := newCustomer(t)
	first := customer.listen(t, "")
	created := requestTaxi(t, customer)
	first.waitForStatus(t, "offered")
	cursor := first.lastID()
	first.stop()

	offers := driver.get(t, "/api/v1/requests/active").body["offers"].([]any)
	require.Len(t, offers, 1)
	offerID := offers[0].(map[string]any)["id"].(string)
	version := offers[0].(map[string]any)["request_version"]
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/offers/"+offerID+"/accept", map[string]any{"version": version}).status)

	second := customer.listen(t, cursor)
	accepted := second.waitForStatus(t, "accepted")
	assert.Equal(t, created.str("id"), accepted.Data["request_id"])
	assert.Empty(t, second.ofType("resync"))
}

func TestCriterion2SlowSubscriberReceivesEverythingInOrder(t *testing.T) {
	customer := newCustomer(t)
	slow := customer.listenSlow(t, "", 5*time.Millisecond)
	mailbox := realtime.NewMailbox(env.redisDirect, 200, time.Hour)
	ctx := context.Background()
	const total = 150
	for i := range total {
		data := fmt.Appendf(nil, `{"n":%d}`, i)
		require.NoError(t, mailbox.Deliver(ctx, customer.id.String(), services.MailboxMessage{EventID: uuid.NewString(), Type: "request.status", Class: services.ClassState, Data: data}))
	}
	require.Eventually(t, func() bool { return len(slow.ofType("request.status")) == total }, 15*time.Second, 20*time.Millisecond)
	for i, event := range slow.ofType("request.status") {
		assert.EqualValues(t, i, event.Data["n"])
	}
}

func TestResyncWhenTheCursorIsOlderThanTheMailbox(t *testing.T) {
	customer := newCustomer(t)
	mailbox := realtime.NewMailbox(env.redisDirect, 200, time.Hour)
	ctx := context.Background()
	require.NoError(t, mailbox.Deliver(ctx, customer.id.String(), services.MailboxMessage{EventID: uuid.NewString(), Type: "request.status", Class: services.ClassState, Data: []byte(`{"n":1}`)}))

	stream := customer.listen(t, "1-0")
	stream.waitFor(t, "resync", nil)
	assert.Empty(t, stream.ofType("request.status"))
}

func TestMailboxAppendIsIdempotentPerRecipient(t *testing.T) {
	user := uuid.NewString()
	mailbox := realtime.NewMailbox(env.redisDirect, 200, time.Hour)
	ctx := context.Background()
	message := services.MailboxMessage{EventID: uuid.NewString(), Type: "request.status", Class: services.ClassState, Data: []byte(`{"n":1}`)}
	require.NoError(t, mailbox.Deliver(ctx, user, message))
	require.NoError(t, mailbox.Deliver(ctx, user, message))

	entries, err := mailbox.Read(ctx, user, "", 10)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestCriterion3KillingAnInstanceDuringAnOfferLosesNothing(t *testing.T) {
	clearOnlineProviders(t)
	ctx := context.Background()
	victim, err := startInstance(ctx)
	require.NoError(t, err)
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})
	driverStream := driver.listen(t, "")
	customer := newCustomer(t)
	customer.api = victim.baseURL
	customer.client = customerClient

	created := requestTaxi(t, customer)
	victim.shutdown()

	offer := driverStream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == created.str("id") })
	customer.api = env.api.baseURL
	accepted := acceptOffer(t, driver, offer)
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	assert.Equal(t, "accepted", customer.get(t, "/api/v1/requests/"+created.str("id")).str("status"))
}

func TestCriterion4ConcurrentAcceptsYieldExactlyOneWinner(t *testing.T) {
	clearOnlineProviders(t)
	providers := []*person{
		newProvider(t, "towing", point{-8.84, 13.24}),
		newProvider(t, "towing", point{-8.85, 13.24}),
		newProvider(t, "towing", point{-8.86, 13.24}),
	}
	streams := make([]*stream, len(providers))
	for i, provider := range providers {
		streams[i] = provider.listen(t, "")
	}
	customer := newCustomer(t)
	estimateID, serviceID := estimateFor(t, customer, "towing")
	res := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, res.status, res.text)

	offers := make([]sseEvent, len(providers))
	for i := range providers {
		offers[i] = streams[i].waitFor(t, "request.offer", nil)
		assert.Equal(t, offers[0].Data["request_version"], offers[i].Data["request_version"])
	}

	const attemptsPerProvider = 5
	var wins, conflicts atomic.Int64
	var wg sync.WaitGroup
	for i, provider := range providers {
		for range attemptsPerProvider {
			wg.Go(func() {
				out, err := provider.attempt(http.MethodPost, "/api/v1/offers/"+offers[i].Data["offer_id"].(string)+"/accept", map[string]any{"version": offers[i].Data["request_version"]}, nil)
				if !assert.NoError(t, err) {
					return
				}
				switch out.status {
				case http.StatusOK:
					wins.Add(1)
				case http.StatusConflict:
					conflicts.Add(1)
				default:
					t.Errorf("unexpected %d: %s", out.status, out.text)
				}
			})
		}
	}
	wg.Wait()
	assert.EqualValues(t, 1, wins.Load())
	assert.EqualValues(t, len(providers)*attemptsPerProvider-1, conflicts.Load())

	var winner int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM request_offers WHERE request_id = $1 AND status = 'accepted'`, res.str("id")).Scan(&winner))
	assert.Equal(t, 1, winner)
	cancelled := 0
	for i := range providers {
		if len(streams[i].ofType("request.offer_cancelled")) > 0 {
			cancelled++
		}
	}
	require.Eventually(t, func() bool {
		cancelled = 0
		for i := range providers {
			if len(streams[i].ofType("request.offer_cancelled")) > 0 {
				cancelled++
			}
		}
		return cancelled == len(providers)-1
	}, 5*time.Second, 20*time.Millisecond, "the losers see the offer cancelled")
}

func TestCriterion5WithoutAckTheWatcherFallsBackAtThePolicyTimeThenMovesOn(t *testing.T) {
	clearOnlineProviders(t)
	silent := newProvider(t, "taxi", point{-8.8391, 13.2341})
	require.Equal(t, http.StatusNoContent, silent.do(t, http.MethodPut, "/api/v1/devices", map[string]string{"token": "device-" + uuid.NewString(), "platform": "android"}, nil).status)
	silentStream := silent.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)

	offer := silentStream.waitFor(t, "request.offer", nil)
	offerID := offer.Data["offer_id"].(string)
	sentAt := time.Now()

	var pushes []fcmSeen
	require.Eventually(t, func() bool {
		pushes = env.fcm.forOffer(offerID)
		return len(pushes) > 0
	}, 6*time.Second, 20*time.Millisecond, "FCM after 3 s without ack")
	elapsed := pushes[0].At.Sub(sentAt)
	assert.GreaterOrEqual(t, elapsed, 2*time.Second)
	assert.Less(t, elapsed, 4500*time.Millisecond)
	assert.Equal(t, "request.offer", pushes[0].Type)

	next := newProvider(t, "taxi", point{-8.8392, 13.2342})
	nextStream := next.listen(t, "")
	second := nextStream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == created.str("id") })
	assert.NotEqual(t, offerID, second.Data["offer_id"], "after 8 s without ack the next provider gets the offer")
	var status string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT status FROM request_offers WHERE id = $1`, offerID).Scan(&status))
	assert.Equal(t, "no_response", status)
}

func TestCriterion6ExpiredOfferNeverAppearsInTheReplay(t *testing.T) {
	clearOnlineProviders(t)
	setPolicy(t, "taxi", "offer_ttl_seconds = 2, ack_timeout_seconds = 10, ack_giveup_seconds = 0")
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})
	live := driver.listen(t, "")
	customer := newCustomer(t)
	requestTaxi(t, customer)
	live.waitFor(t, "request.offer", nil)
	live.stop()

	time.Sleep(2500 * time.Millisecond)
	replay := driver.listen(t, "")
	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, replay.ofType("request.offer"))
}

func TestCriterion7ProviderAtTheEdgeOfTheRadiusIsFound(t *testing.T) {
	clearOnlineProviders(t)
	near := newProvider(t, "taxi", point{pickup.Lat + 0.0263, pickup.Lng})
	stream := near.listen(t, "")
	requestTaxi(t, newCustomer(t))
	stream.waitFor(t, "request.offer", nil)
}

func TestCriterion8TaxiCompletesWithMapboxDown(t *testing.T) {
	clearOnlineProviders(t)
	env.mapbox.down.Store(true)
	t.Cleanup(func() { env.mapbox.down.Store(false) })
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})
	stream := driver.listen(t, "")
	customer := newCustomer(t)

	estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	option := estimate.body["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "fallback", option["route_source"], "the quote is marked to measure the deviation")

	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	offer := stream.waitFor(t, "request.offer", nil)
	accepted := acceptOffer(t, driver, offer)
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	assert.Equal(t, "fallback", accepted.body["route"].(map[string]any)["source"])
	for _, step := range []string{"en-route", "start", "complete"} {
		res := driver.post(t, "/api/v1/requests/"+created.str("id")+"/"+step, nil)
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
	assert.Equal(t, "completed", customer.get(t, "/api/v1/requests/"+created.str("id")).str("status"))
}

type badNetwork struct {
	server         *httptest.Server
	requests       atomic.Int64
	lostOnTheWay   atomic.Int64
	lostOnTheReply atomic.Int64
}

func newBadNetwork(t *testing.T, upstream string, latency time.Duration) *badNetwork {
	t.Helper()
	target, err := url.Parse(upstream)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	network := &badNetwork{}
	network.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(latency + rand.N(latency))
		if r.URL.Path == "/events" {
			proxy.ServeHTTP(w, r)
			return
		}
		switch network.requests.Add(1) % 4 {
		case 1:
			network.lostOnTheWay.Add(1)
			panic(http.ErrAbortHandler)
		case 3:
			proxy.ServeHTTP(httptest.NewRecorder(), r)
			network.lostOnTheReply.Add(1)
			panic(http.ErrAbortHandler)
		default:
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(network.server.Close)
	return network
}

func (p *person) retrying(t *testing.T, method, path string, payload any) result {
	t.Helper()
	headers := map[string]string{"Idempotency-Key": uuid.NewString()}
	backoff := 50 * time.Millisecond
	for range 30 {
		res, err := p.attempt(method, path, payload, headers)
		if err == nil && res.code() != "IDEMPOTENCY_IN_PROGRESS" {
			return res
		}
		time.Sleep(backoff + rand.N(backoff))
		backoff = min(backoff*2, time.Second)
	}
	t.Fatalf("%s %s never got through", method, path)
	return result{}
}

func TestCriterion9FullFlowOverABadNetworkWithIdempotentRetries(t *testing.T) {
	clearOnlineProviders(t)
	network := newBadNetwork(t, env.api.baseURL, 100*time.Millisecond)
	driver := newApprovedProvider(t, "taxi")
	driver.api = network.server.URL
	customer := newCustomer(t)
	customer.api = network.server.URL

	online := driver.retrying(t, http.MethodPost, "/api/v1/providers/me/online", map[string]any{"position": positionAt(point{-8.8400, 13.2350})})
	require.Equal(t, http.StatusOK, online.status, online.text)
	stream := driver.listen(t, "")

	estimate := customer.retrying(t, http.MethodPost, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	option := estimate.body["items"].([]any)[0].(map[string]any)
	created := customer.retrying(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target})
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, created.status, created.text)
	requestID := created.str("id")

	offer := stream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == requestID })
	offerID := offer.Data["offer_id"].(string)
	require.Equal(t, http.StatusNoContent, driver.retrying(t, http.MethodPost, "/api/v1/offers/"+offerID+"/delivered", map[string]string{"channel": "sse"}).status)
	accepted := driver.retrying(t, http.MethodPost, "/api/v1/offers/"+offerID+"/accept", map[string]any{"version": offer.Data["request_version"]})
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	for _, step := range []string{"en-route", "start", "complete"} {
		res := driver.retrying(t, http.MethodPost, "/api/v1/requests/"+requestID+"/"+step, nil)
		require.Equal(t, http.StatusOK, res.status, res.text)
	}
	assert.Equal(t, "completed", customer.retrying(t, http.MethodGet, "/api/v1/requests/"+requestID, nil).str("status"))

	var requests, offers int
	ctx := context.Background()
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM service_requests WHERE customer_id = $1`, customer.id).Scan(&requests))
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM request_offers WHERE request_id = $1`, requestID).Scan(&offers))
	assert.Equal(t, 1, requests, "retries never create a second request")
	assert.Equal(t, 1, offers)
	assert.Positive(t, network.lostOnTheWay.Load(), "requests were lost on the way")
	assert.Positive(t, network.lostOnTheReply.Load(), "actions ran and their answers were lost")
}

func TestCriterion10And11ExpiredQuoteGetsANewOneWithoutCreatingARequest(t *testing.T) {
	customer := newCustomer(t)
	estimateID, serviceID := estimateFor(t, customer, "taxi")
	ctx := context.Background()
	_, err := env.owner.Exec(ctx, `UPDATE fare_estimates SET expires_at = now() - interval '1 second' WHERE id = $1`, estimateID)
	require.NoError(t, err)

	res := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})

	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "FARE_ESTIMATE_EXPIRED", res.code())
	fresh := res.body["quotes"].(map[string]any)["items"].([]any)[0].(map[string]any)
	assert.NotEqual(t, estimateID, fresh["fare_estimate_id"])
	var count int
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM service_requests WHERE customer_id = $1`, customer.id).Scan(&count))
	assert.Zero(t, count)

	moved := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": fresh["fare_estimate_id"], "pickup": point{-8.70, 13.30}, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "FARE_ESTIMATE_INVALID", moved.code(), "a quote only holds for its own route")

	stranger := newCustomer(t)
	stolen := stranger.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": fresh["fare_estimate_id"], "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "FARE_ESTIMATE_INVALID", stolen.code(), "a quote belongs to who asked for it")
}

func TestTaxiSequentialRejectionMovesToTheNextAndBoostGoesToTheDriver(t *testing.T) {
	clearOnlineProviders(t)
	nearest := newProvider(t, "taxi", point{-8.8391, 13.2341})
	nearestStream := nearest.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	requestID := created.str("id")

	first := nearestStream.waitFor(t, "request.offer", nil)
	assert.True(t, created.body["can_boost"].(bool) || customer.get(t, "/api/v1/requests/"+requestID).body["can_boost"].(bool))
	require.Equal(t, http.StatusNoContent, nearest.post(t, "/api/v1/offers/"+first.Data["offer_id"].(string)+"/reject", nil).status)

	require.Eventually(t, func() bool {
		return customer.get(t, "/api/v1/requests/"+requestID).str("status") == "searching"
	}, 3*time.Second, 20*time.Millisecond)

	boosted := customer.post(t, "/api/v1/requests/"+requestID+"/boost", map[string]int{"percent": 10})
	require.Equal(t, http.StatusOK, boosted.status, boosted.text)
	assert.EqualValues(t, 188_650, boosted.num("fare_cents"))

	second := nearestStream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["offer_id"] != first.Data["offer_id"] })
	assert.EqualValues(t, 154_350+17_150, second.Data["net_cents"], "the rejected driver is offered the raise, all of it net")

	lower := customer.post(t, "/api/v1/requests/"+requestID+"/boost", map[string]int{"percent": 10})
	assert.Equal(t, "BOOST_NOT_HIGHER", lower.code())
	accepted := acceptOffer(t, nearest, second)
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
}

func TestTaxiExpandsTheRadiusAndExpiresWithoutProviders(t *testing.T) {
	clearOnlineProviders(t)
	setPolicy(t, "taxi", "search_ttl_seconds = 3, offer_ttl_seconds = 1")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	stream := customer.listen(t, "")
	stream.waitForStatus(t, "expired")
	assert.Equal(t, "expired", customer.get(t, "/api/v1/requests/"+created.str("id")).str("status"))

	clearOnlineProviders(t)
	farAway := newProvider(t, "taxi", point{pickup.Lat + 0.075, pickup.Lng})
	farStream := farAway.listen(t, "")
	second := newCustomer(t)
	setPolicy(t, "taxi", "search_ttl_seconds = 30")
	requestTaxi(t, second)
	offer := farStream.waitFor(t, "request.offer", nil)
	assert.Greater(t, offer.Data["distance_m"], 8000.0, "found only at the 10 km ring")
}

func TestOfflineProviderReceivesNothingAndCustomerSeesOnlyAggregates(t *testing.T) {
	clearOnlineProviders(t)
	gone := newProvider(t, "taxi", point{-8.8391, 13.2341})
	require.Equal(t, http.StatusOK, gone.post(t, "/api/v1/providers/me/offline", nil).status)
	one := newProvider(t, "taxi", point{-8.8391, 13.2341})
	two := newProvider(t, "taxi", point{-8.8391, 13.2341})
	_, _ = one, two
	customer := newCustomer(t)

	nearby := customer.get(t, fmt.Sprintf("/api/v1/nearby?lat=%f&lng=%f&category=taxi", pickup.Lat, pickup.Lng))
	require.Equal(t, http.StatusOK, nearby.status, nearby.text)
	cells := nearby.body["cells"].([]any)
	require.Len(t, cells, 1)
	cell := cells[0].(map[string]any)
	assert.EqualValues(t, 2, cell["count"])
	assert.NotContains(t, nearby.text, one.id.String())

	outside := customer.get(t, "/api/v1/nearby?lat=-14.9&lng=13.5&category=taxi")
	assert.Equal(t, "OUTSIDE_SERVICE_AREA", outside.code())

	forbidden := one.get(t, fmt.Sprintf("/api/v1/nearby?lat=%f&lng=%f&category=taxi", pickup.Lat, pickup.Lng))
	assert.Equal(t, http.StatusForbidden, forbidden.status)
}

func TestLocationPlausibilityAndCadence(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.8400, 13.2350})

	aged := func(at point, age int) map[string]any {
		position := positionAt(at)
		position["age_s"] = age
		return position
	}
	idle := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{aged(point{-8.8401, 13.2351}, 90)}})
	require.Equal(t, http.StatusOK, idle.status, idle.text)
	assert.EqualValues(t, 30, idle.num("interval_s"))
	assert.EqualValues(t, 150, idle.num("distance_m"))

	slow := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{aged(point{-8.8402, 13.2352}, 60)}, "battery_low": true})
	assert.EqualValues(t, 60, slow.num("interval_s"))

	jump := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{aged(point{-14.9, 13.5}, 50)}})
	assert.EqualValues(t, 0, jump.num("accepted"))
	assert.EqualValues(t, 1, jump.num("rejected"))

	batch := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{
		map[string]any{"lat": -8.8405, "lng": 13.2355, "accuracy_m": 10, "age_s": 0},
		map[string]any{"lat": -8.8403, "lng": 13.2353, "accuracy_m": 10, "age_s": 20},
		map[string]any{"lat": -8.8404, "lng": 13.2354, "accuracy_m": 500, "age_s": 10},
	}})
	assert.EqualValues(t, 2, batch.num("accepted"))
	assert.EqualValues(t, 1, batch.num("rejected"))

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/providers/me/offline", nil).status)
	offline := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.84, 13.24})}})
	assert.Equal(t, false, offline.body["online"])
	assert.Equal(t, false, offline.body["send_gps"])

	stranger := newPerson(t, "provider")
	assert.Equal(t, "PROVIDER_NOT_APPROVED", stranger.post(t, "/api/v1/providers/me/online", nil).code())
}

func TestTowingParallelOffersWithLateAcceptAndOperatorAlert(t *testing.T) {
	clearOnlineProviders(t)
	first, second := newProvider(t, "towing", point{-8.84, 13.24}), newProvider(t, "towing", point{-8.85, 13.24})
	firstStream, secondStream := first.listen(t, ""), second.listen(t, "")
	customer := newCustomer(t)
	estimateID, serviceID := estimateFor(t, customer, "towing")
	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)

	offerA, offerB := firstStream.waitFor(t, "request.offer", nil), secondStream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, second, offerB).status)
	secondStream.waitForStatus(t, "accepted")
	firstStream.waitFor(t, "request.offer_cancelled", nil)
	assert.Equal(t, http.StatusConflict, acceptOffer(t, first, offerA).status, "accepting late answers 409")
	var smsSent int
	require.Eventually(t, func() bool {
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id = $1 AND channel = 'sms' AND template = 'request_assigned'`, customer.id).Scan(&smsSent))
		return smsSent == 1
	}, 5*time.Second, 20*time.Millisecond, "the SMS confirms the assignment even without data")

	clearOnlineProviders(t)
	setPolicy(t, "towing", "search_ttl_seconds = 2, offer_ttl_seconds = 1")
	stranded := newCustomer(t)
	estimateID, serviceID = estimateFor(t, stranded, "towing")
	queued := stranded.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, queued.status, queued.text)
	stream := stranded.listen(t, "")
	alert := stream.waitFor(t, "request.alert", nil)
	assert.Equal(t, "no_provider", alert.Data["reason"])
	view := stranded.get(t, "/api/v1/requests/"+queued.str("id"))
	assert.Equal(t, true, view.body["needs_operator"])
	assert.Equal(t, "searching", view.str("status"), "it waits for the operator instead of expiring")
}

func TestMaintenanceByCoverageWithoutTrackingOnSite(t *testing.T) {
	clearOnlineProviders(t)
	technician := newApprovedProvider(t, "maintenance")
	coverage := technician.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -8.85, "lng": 13.25, "radius_km": 30}, nil)
	require.Equal(t, http.StatusOK, coverage.status, coverage.text)
	assert.Greater(t, coverage.num("cells"), int64(30))

	tooFar := newApprovedProvider(t, "maintenance")
	require.Equal(t, http.StatusOK, tooFar.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -9.2, "lng": 13.6, "radius_km": 5}, nil).status)
	farStream := tooFar.listen(t, "")

	require.Equal(t, http.StatusNoContent, technician.do(t, http.MethodPut, "/api/v1/devices", map[string]string{"token": "tech-" + uuid.NewString(), "platform": "android"}, nil).status)
	stream := technician.listen(t, "")
	customer := newCustomer(t)
	created := requestCategory(t, customer, "maintenance", map[string]any{"details": map[string]any{"problem": "Luz do motor acesa"}})
	assert.Nil(t, created.body["destination"])

	offer := stream.waitFor(t, "request.offer", nil)
	assert.Empty(t, farStream.ofType("request.offer"), "a provider who does not cover the point gets nothing")
	require.Eventually(t, func() bool { return len(env.fcm.forOffer(offer.Data["offer_id"].(string))) > 0 }, 5*time.Second, 20*time.Millisecond, "FCM goes at the same time as SSE")
	var smsCount int
	require.Eventually(t, func() bool {
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id = $1 AND channel = 'sms' AND template = 'offer'`, technician.id).Scan(&smsCount))
		return smsCount == 1
	}, 5*time.Second, 20*time.Millisecond)

	detail := technician.get(t, "/api/v1/offers/"+offer.Data["offer_id"].(string))
	assert.Equal(t, "Luz do motor acesa", detail.body["details"].(map[string]any)["problem"])

	require.Equal(t, http.StatusOK, acceptOffer(t, technician, offer).status)
	id := created.str("id")
	require.Equal(t, http.StatusOK, technician.post(t, "/api/v1/requests/"+id+"/en-route", nil).status)
	onTheWay := technician.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.84, 13.24})}})
	assert.Equal(t, true, onTheWay.body["send_gps"])

	require.Equal(t, http.StatusOK, technician.post(t, "/api/v1/requests/"+id+"/start", nil).status)
	onSite := technician.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.84, 13.2401})}})
	assert.Equal(t, false, onSite.body["send_gps"], "on site the position is not sent")
	assert.Equal(t, "on_site", onSite.str("phase"))
	require.Equal(t, http.StatusOK, technician.post(t, "/api/v1/requests/"+id+"/complete", nil).status)
}

func TestRemovalsQuoteChoiceAndScheduleReachesTheSearch(t *testing.T) {
	clearOnlineProviders(t)
	setPolicy(t, "removals", "search_lead_seconds = 3600")
	cheap, costly := newApprovedProvider(t, "removals"), newApprovedProvider(t, "removals")
	for _, company := range []*person{cheap, costly} {
		require.Equal(t, http.StatusOK, company.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -8.85, "lng": 13.25}, nil).status)
	}
	cheapStream, costlyStream := cheap.listen(t, ""), costly.listen(t, "")
	customer := newCustomer(t)
	customerStream := customer.listen(t, "")

	soon := time.Now().Add(30 * time.Minute).UTC().Truncate(15 * time.Minute).Format(time.RFC3339)
	created := requestCategory(t, customer, "removals", map[string]any{"destination": target, "scheduled_for": soon, "details": map[string]any{"volume_m3": 12}})
	id := created.str("id")

	offerA := cheapStream.waitFor(t, "request.offer", nil)
	offerB := costlyStream.waitFor(t, "request.offer", nil)
	assert.Equal(t, "QUOTE_REQUIRED", acceptOffer(t, cheap, offerA).code())

	require.Equal(t, http.StatusNoContent, costly.post(t, "/api/v1/offers/"+offerB.Data["offer_id"].(string)+"/quote", map[string]any{"amount_cents": 14_000_000}).status)
	require.Equal(t, http.StatusNoContent, cheap.post(t, "/api/v1/offers/"+offerA.Data["offer_id"].(string)+"/quote", map[string]any{"amount_cents": 11_000_000, "note": "Inclui embalagem"}).status)
	customerStream.waitForStatus(t, "awaiting_choice")
	require.Eventually(t, func() bool { return len(customerStream.ofType("request.quote")) == 2 }, 5*time.Second, 20*time.Millisecond)

	view := customer.get(t, "/api/v1/requests/"+id)
	quotes := view.body["quotes"].([]any)
	require.Len(t, quotes, 2)

	stale := customer.post(t, "/api/v1/requests/"+id+"/select-quote", map[string]any{"offer_id": offerA.Data["offer_id"], "version": view.num("version") - 1})
	assert.Equal(t, "REQUEST_CONFLICT", stale.code())
	selected := customer.post(t, "/api/v1/requests/"+id+"/select-quote", map[string]any{"offer_id": offerA.Data["offer_id"], "version": view.num("version")})
	require.Equal(t, http.StatusOK, selected.status, selected.text)
	assert.EqualValues(t, 11_000_000, selected.num("fare_cents"), "the value charged is the accepted quote")
	costlyStream.waitFor(t, "request.offer_cancelled", nil)
	cheapStream.waitForStatus(t, "accepted")
}

func TestExpiredQuoteCannotBeChosen(t *testing.T) {
	clearOnlineProviders(t)
	company := newApprovedProvider(t, "removals")
	require.Equal(t, http.StatusOK, company.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -8.85, "lng": 13.25}, nil).status)
	stream := company.listen(t, "")
	customer := newCustomer(t)
	created := requestCategory(t, customer, "removals", map[string]any{"destination": target})
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusNoContent, company.post(t, "/api/v1/offers/"+offer.Data["offer_id"].(string)+"/quote", map[string]any{"amount_cents": 10_000_000}).status)

	_, err := env.owner.Exec(context.Background(), `UPDATE request_offers SET expires_at = now() - interval '1 second' WHERE id = $1`, offer.Data["offer_id"])
	require.NoError(t, err)
	version := customer.get(t, "/api/v1/requests/"+created.str("id")).num("version")
	res := customer.post(t, "/api/v1/requests/"+created.str("id")+"/select-quote", map[string]any{"offer_id": offer.Data["offer_id"], "version": version})
	assert.Equal(t, "QUOTE_EXPIRED", res.code())
}

func TestScheduledRequestStartsSearchingAtTheLeadTime(t *testing.T) {
	clearOnlineProviders(t)
	at := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Minute)
	setPolicy(t, "taxi", fmt.Sprintf("schedule_slot_minutes = 1, search_lead_seconds = %d", int(time.Until(at).Seconds())-2))
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	scheduled := at.Format(time.RFC3339)
	option := taxiQuote(t, customer, map[string]any{"scheduled_for": scheduled})
	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target, "scheduled_for": scheduled}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, "scheduled", created.str("status"))
	assert.Empty(t, stream.ofType("request.offer"))
	stream.waitFor(t, "request.offer", nil)
}

func TestCancellationReleasesTheProviderForTheNextRequest(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	stream.waitFor(t, "request.offer", nil)

	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, "/api/v1/requests/"+created.str("id")+"/cancel", map[string]string{}).status)
	cancelled := customer.post(t, "/api/v1/requests/"+created.str("id")+"/cancel", map[string]string{"reason": "Mudei de planos"})
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	stream.waitFor(t, "request.offer_cancelled", nil)

	next := newCustomer(t)
	second := requestTaxi(t, next)
	stream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == second.str("id") })
}

func TestOtherPeopleCannotSeeOrActOnARequest(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	offer := stream.waitFor(t, "request.offer", nil)
	intruder := newCustomer(t)
	otherDriver := newApprovedProvider(t, "taxi")

	assert.Equal(t, "REQUEST_NOT_FOUND", intruder.get(t, "/api/v1/requests/"+created.str("id")).code())
	assert.Equal(t, "REQUEST_NOT_FOUND", intruder.post(t, "/api/v1/requests/"+created.str("id")+"/cancel", map[string]string{"reason": "x"}).code())
	assert.Equal(t, "OFFER_NOT_FOUND", acceptOffer(t, otherDriver, offer).code())
	assert.Equal(t, http.StatusForbidden, customer.post(t, "/api/v1/offers/"+offer.Data["offer_id"].(string)+"/accept", map[string]any{"version": 1}).status)
	assert.Equal(t, http.StatusUnauthorized, (&person{client: customerClient, api: env.api.baseURL, token: "nope"}).get(t, "/api/v1/requests/active").status)
}

func TestRequestOutsideTheServiceAreaIsExplained(t *testing.T) {
	customer := newCustomer(t)
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": point{-14.9, 13.5}, "destination": target})
	assert.Equal(t, "OUTSIDE_SERVICE_AREA", res.code())
}

func TestRedisOutageKeepsTheRequestSearchingAndRecovers(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	estimateID, serviceID := estimateFor(t, customer, "taxi")
	var requestID string
	ctx := context.Background()

	pickupAddress, destinationAddress := uuid.New(), uuid.New()
	_, err := env.owner.Exec(ctx, `INSERT INTO addresses (id, lat, lng) VALUES ($1, $2, $3), ($4, $5, $6)`, pickupAddress, pickup.Lat, pickup.Lng, destinationAddress, target.Lat, target.Lng)
	require.NoError(t, err)
	require.NoError(t, env.owner.QueryRow(ctx, `
		INSERT INTO service_requests (id, customer_id, category_id, category_slug, service_id, zone_id, policy_id, status, pickup_address_id, pickup_h3_r8, destination_address_id, currency, idempotency_key, search_deadline_at, next_action_at, fare_estimate_id, fare_cents, commission_cents)
		SELECT gen_random_uuid(), $1, s.category_id, 'taxi', s.id, (SELECT id FROM service_zones LIMIT 1), p.id, 'searching', $3, 1, $4, 'AOA', $5, now() + interval '3 minutes', now() + interval '1 second', $6, 171500, 17150
		FROM services s JOIN dispatch_policies p ON p.category_id = s.category_id AND p.zone_id IS NULL WHERE s.id = $2 RETURNING id`,
		customer.id, serviceID, pickupAddress, destinationAddress, uuid.NewString(), estimateID).Scan(&requestID))

	env.redisProxy.setBlocked(true)
	time.Sleep(1500 * time.Millisecond)
	var status string
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT status FROM service_requests WHERE id = $1`, requestID).Scan(&status))
	assert.Equal(t, "searching", status, "without Redis the matching stops and the request waits")
	env.redisProxy.setBlocked(false)

	reconnected := driver.listen(t, "")
	stream.stop()
	require.Eventually(t, func() bool {
		var offered string
		_ = env.owner.QueryRow(ctx, `SELECT status FROM service_requests WHERE id = $1`, requestID).Scan(&offered)
		return offered == "offered" || offered == "searching" && len(reconnected.ofType("request.offer")) > 0
	}, 20*time.Second, 50*time.Millisecond, "the search resumes after the outage")
}

func TestRabbitOutageKeepsEventsInTheOutbox(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)

	env.rabbitProxy.setBlocked(true)
	created := requestTaxi(t, customer)
	time.Sleep(time.Second)
	var pending int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE aggregate_id = $1 AND published_at IS NULL`, created.str("id")).Scan(&pending))
	assert.Positive(t, pending, "without RabbitMQ the events wait in the outbox")
	assert.Empty(t, stream.ofType("request.offer"))
	env.rabbitProxy.setBlocked(false)

	require.Eventually(t, func() bool {
		return len(stream.ofType("request.offer")) > 0
	}, 40*time.Second, 50*time.Millisecond, "the relay resumes publishing")
}

func TestGracefulShutdownAsksStreamsToReconnect(t *testing.T) {
	ctx := context.Background()
	instance, err := startInstance(ctx)
	require.NoError(t, err)
	customer := newCustomer(t)
	customer.api = instance.baseURL
	stream := customer.listen(t, "")
	require.Eventually(t, func() bool { return len(stream.ofType("control")) > 0 }, 3*time.Second, 10*time.Millisecond)

	started := time.Now()
	instance.stop()
	assert.NoError(t, <-instance.done, "every worker stops in time")
	assert.Less(t, time.Since(started), 4*time.Second)
	require.NoError(t, instance.container.Close())

	reconnect := stream.waitFor(t, "control", func(event sseEvent) bool { return event.Data["action"] == "reconnect" })
	assert.Contains(t, reconnect.Raw, "retry_in_ms")
}

func TestPolicyTableMatchesTheProposal(t *testing.T) {
	type row struct {
		Source  string    `json:"candidate_source"`
		Res     int       `json:"h3_resolution"`
		Radii   []float64 `json:"radii_km"`
		Mode    string    `json:"offer_mode"`
		Batch   int       `json:"batch_size"`
		Offer   int       `json:"offer_ttl_seconds"`
		Search  int       `json:"search_ttl_seconds"`
		Ack     int       `json:"ack_timeout_seconds"`
		GiveUp  int       `json:"ack_giveup_seconds"`
		Initial []string  `json:"initial_channels"`
		Back    []string  `json:"fallback_channels"`
		Rank    string    `json:"ranking"`
		Empty   string    `json:"on_exhausted"`
	}
	expected := map[string]row{
		"taxi":        {"live", 7, []float64{3, 5, 10}, "sequential", 1, 20, 180, 3, 8, []string{"sse"}, []string{"fcm"}, "distance_idle", "suggest_retry"},
		"towing":      {"live", 5, []float64{10, 30, 80}, "parallel", 3, 45, 600, 5, 30, []string{"sse"}, []string{"fcm", "sms"}, "distance", "alert_operator"},
		"maintenance": {"coverage", 6, []float64{30}, "parallel", 5, 600, 7200, 60, 0, []string{"fcm", "sms", "sse"}, []string{}, "base_distance", "operator_queue"},
		"removals":    {"coverage", 6, []float64{50}, "quote", 10, 7200, 86400, 300, 0, []string{"fcm", "sms", "sse"}, []string{}, "customer_choice", "extend_or_queue"},
	}
	for slug, want := range expected {
		var raw string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT to_jsonb(p)::text FROM dispatch_policies p JOIN service_categories c ON c.id = p.category_id WHERE c.slug = $1 AND p.zone_id IS NULL`, slug).Scan(&raw))
		var got row
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, want, got, slug)
	}
}

func TestTariffsAreNeverEdited(t *testing.T) {
	ctx := context.Background()
	_, err := env.owner.Exec(ctx, `UPDATE service_tariffs SET base_fee_cents = 1 WHERE valid_until IS NULL`)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "new version"))
	_, err = env.owner.Exec(ctx, `DELETE FROM service_tariffs`)
	assert.Error(t, err)
}

func TestRouteChangeRepricesAndIsRecorded(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	id := created.str("id")
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)

	env.mapbox.down.Store(true)
	t.Cleanup(func() { env.mapbox.down.Store(false) })
	changed := customer.post(t, "/api/v1/requests/"+id+"/route", map[string]any{"destination": point{-9.10, 13.40}})
	require.Equal(t, http.StatusOK, changed.status, changed.text)
	assert.NotEqual(t, int64(171_500), changed.num("fare_cents"))
	adjustments := changed.body["adjustments"].([]any)
	require.Len(t, adjustments, 1)
	first := adjustments[0].(map[string]any)
	assert.Equal(t, "route_change", first["reason"])
	assert.EqualValues(t, changed.num("fare_cents")-171_500, first["amount_cents"])
	stream.waitFor(t, "request.route", nil)

	assert.Equal(t, "REQUEST_NOT_FOUND", newCustomer(t).post(t, "/api/v1/requests/"+id+"/route", map[string]any{"destination": target}).code())
}

func TestOperatorDispatchesByHandRetriesAndCancels(t *testing.T) {
	clearOnlineProviders(t)
	setPolicy(t, "towing", "search_ttl_seconds = 1, offer_ttl_seconds = 1")
	ctx := context.Background()
	operator := entities.Actor{}
	usecase := env.api.container.Dispatch.Usecase
	stranded := newCustomer(t)
	estimateID, serviceID := estimateFor(t, stranded, "towing")
	queued := stranded.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, queued.status, queued.text)
	id := uuid.MustParse(queued.str("id"))

	require.Eventually(t, func() bool {
		queue, err := usecase.OperatorQueue(ctx, pagination.Params{})
		require.NoError(t, err)
		for i := range queue.Items {
			if queue.Items[i].ID == id {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "the request reaches the operator queue")

	stranger := newApprovedProvider(t, "taxi")
	_, err := usecase.AssignManually(ctx, operator, id, stranger.id)
	assert.ErrorIs(t, err, dispatch.ErrProviderNotEligible, "a provider of another service cannot take it")

	retried, err := usecase.RetrySearch(ctx, operator, id)
	require.NoError(t, err)
	assert.False(t, retried.NeedsOperator)

	tow := newApprovedProvider(t, "towing")
	stream := stranded.listen(t, "")
	assigned, err := usecase.AssignManually(ctx, operator, id, tow.id)
	require.NoError(t, err)
	assert.Equal(t, "accepted", assigned.Status)
	assert.Equal(t, tow.id, assigned.Provider.ID)
	stream.waitForStatus(t, "accepted")

	cancelled, err := usecase.CancelByOperator(ctx, operator, id, "Cliente pediu por telefone")
	require.NoError(t, err)
	assert.Equal(t, "operator", cancelled.CancelledBy)
	stream.waitForStatus(t, "cancelled")
}

func TestReminderReachesBothPartiesBeforeTheDate(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	offers := driver.listen(t, "")
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	offer := offers.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	stream := customer.listen(t, "")

	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET reminder_at = now() - interval '1 second' WHERE id = $1`, created.str("id"))
	require.NoError(t, err)

	stream.waitFor(t, "request.reminder", nil)
	offers.waitFor(t, "request.reminder", nil)
	var pending *time.Time
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT reminder_at FROM service_requests WHERE id = $1`, created.str("id")).Scan(&pending))
	assert.Nil(t, pending, "it fires once")
}

func TestProviderTripSurvivesLosingRedis(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	offers := driver.listen(t, "")
	customer := newCustomer(t)
	requestTaxi(t, customer)
	offer := offers.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)

	ctx := context.Background()
	keys, err := env.redisDirect.Keys(ctx, "live:*").Result()
	require.NoError(t, err)
	require.NoError(t, env.redisDirect.Del(ctx, keys...).Err())

	lost := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.84, 13.236})}})
	assert.Equal(t, false, lost.body["online"], "the app learns it must go online again")

	back := driver.post(t, "/api/v1/providers/me/online", map[string]any{"position": positionAt(point{-8.84, 13.236})})
	require.Equal(t, http.StatusOK, back.status, back.text)
	assert.Equal(t, "en_route", back.str("phase"), "the real state comes back from PostgreSQL")
}

func TestDriversOnlyReceiveOffersOfTheirOwnService(t *testing.T) {
	clearOnlineProviders(t)
	economyID, comfortID := serviceBySlug(t, "taxi"), comfortWithTariff(t)

	economy := newProviderOfService(t, economyID, point{-8.8391, 13.2341})
	comfort := newProviderOfService(t, comfortID, point{-8.8392, 13.2342})
	economyStream, comfortStream := economy.listen(t, ""), comfort.listen(t, "")
	customer := newCustomer(t)
	estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	options := map[string]string{}
	for _, raw := range estimate.body["items"].([]any) {
		item := raw.(map[string]any)
		options[item["service_id"].(string)] = item["fare_estimate_id"].(string)
	}
	require.Len(t, options, 2)
	order := func(serviceID string) string {
		res := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": options[serviceID], "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
		require.Equal(t, http.StatusCreated, res.status, res.text)
		return res.str("id")
	}

	comfortRequest := order(comfortID)
	comfortStream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == comfortRequest })
	economyRequest := order(economyID)
	economyStream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == economyRequest })

	time.Sleep(500 * time.Millisecond)
	assert.Len(t, economyStream.ofType("request.offer"), 1, "Economy never gets a Comfort request")
	assert.Len(t, comfortStream.ofType("request.offer"), 1, "Comfort never gets an Economy request")
}

func TestEveryPlaceOfARequestIsAnAddress(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	stop := map[string]any{"lat": -8.88, "lng": 13.21, "address": "Shopping Fortaleza"}
	estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target, "stops": []any{stop}})
	require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
	option := estimate.body["items"].([]any)[0].(map[string]any)
	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{
		"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"],
		"pickup":      map[string]any{"lat": pickup.Lat, "lng": pickup.Lng, "reference": "Portão azul", "address": "Rua Rainha Ginga, 29"},
		"destination": map[string]any{"lat": target.Lat, "lng": target.Lng, "reference": "Largo"},
		"stops":       []any{stop},
	}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)

	ctx := context.Background()
	var line, reference string
	var lat, lng float64
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT a.line, a.reference, a.lat, a.lng FROM service_requests r JOIN addresses a ON a.id = r.pickup_address_id WHERE r.id = $1`, created.str("id")).Scan(&line, &reference, &lat, &lng))
	assert.Equal(t, "Rua Rainha Ginga, 29", line)
	assert.Equal(t, "Portão azul", reference)
	assert.InDelta(t, pickup.Lat, lat, 1e-9)
	assert.InDelta(t, pickup.Lng, lng, 1e-9)

	var inRange bool
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT ST_DWithin(a.geom, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 1) FROM service_requests r JOIN addresses a ON a.id = r.destination_address_id WHERE r.id = $1`, created.str("id"), target.Lng, target.Lat).Scan(&inRange))
	assert.True(t, inRange, "the address is a PostGIS point too")

	view := customer.get(t, "/api/v1/requests/"+created.str("id"))
	assert.Equal(t, "Portão azul", view.body["pickup"].(map[string]any)["reference"])
	stops := view.body["stops"].([]any)
	require.Len(t, stops, 1)
	assert.Equal(t, "Shopping Fortaleza", stops[0].(map[string]any)["address"])

	changed := customer.post(t, "/api/v1/requests/"+created.str("id")+"/route", map[string]any{"destination": map[string]any{"lat": -9.1, "lng": 13.4, "address": "Novo destino"}})
	require.Equal(t, http.StatusOK, changed.status, changed.text)
	assert.Equal(t, "Novo destino", changed.body["destination"].(map[string]any)["address"])
	assert.Empty(t, changed.body["stops"], "the new route has no stops")
	var keptOld int
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM addresses WHERE reference = 'Largo'`).Scan(&keptOld))
	assert.Positive(t, keptOld, "the old destination stays in the history")
}

func TestUsersKeepTheirAddresses(t *testing.T) {
	customer := newCustomer(t)
	const path = "/api/v1/users/me/addresses"
	assert.Empty(t, customer.get(t, path).body["items"])

	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"kind": "home", "line": "Sem pin"}).status, "latitude and longitude are mandatory")
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"kind": "office", "lat": -8.84, "lng": 13.23}).status)

	home := customer.post(t, path, map[string]any{"kind": "home", "label": "Casa", "lat": -8.84, "lng": 13.23, "line": "Rua Rainha Ginga, 29", "reference": "Portão azul", "neighborhood": "Ingombota", "city": "Luanda", "province": "Luanda", "country_code": "ao"})
	require.Equal(t, http.StatusCreated, home.status, home.text)
	assert.Equal(t, "AO", home.str("country_code"))
	assert.Equal(t, "ADDRESS_KIND_TAKEN", customer.post(t, path, map[string]any{"kind": "home", "lat": -8.85, "lng": 13.24}).code(), "only one home")
	require.Equal(t, http.StatusCreated, customer.post(t, path, map[string]any{"kind": "other", "label": "Ginásio", "lat": -8.86, "lng": 13.25}).status)
	require.Equal(t, http.StatusCreated, customer.post(t, path, map[string]any{"kind": "other", "label": "Escola", "lat": -8.87, "lng": 13.26}).status)
	assert.Len(t, customer.get(t, path).body["items"], 3)

	id := home.str("id")
	updated := customer.do(t, http.MethodPut, path+"/"+id, map[string]any{"kind": "home", "label": "Casa nova", "lat": -8.9, "lng": 13.3, "line": "Rua nova"}, nil)
	require.Equal(t, http.StatusOK, updated.status, updated.text)
	assert.Equal(t, id, updated.str("id"))
	assert.Equal(t, "Rua nova", updated.str("line"))

	stranger := newCustomer(t)
	assert.Equal(t, "ADDRESS_NOT_FOUND", stranger.do(t, http.MethodPut, path+"/"+id, map[string]any{"kind": "home", "lat": -8.9, "lng": 13.3}, nil).code())
	assert.Equal(t, "ADDRESS_NOT_FOUND", stranger.do(t, http.MethodDelete, path+"/"+id, nil, nil).code())

	assert.Equal(t, http.StatusNoContent, customer.do(t, http.MethodDelete, path+"/"+id, nil, nil).status)
	assert.Len(t, customer.get(t, path).body["items"], 2)
}

func TestProviderAddressesLiveInTheAddressTable(t *testing.T) {
	provider := newApprovedProvider(t, "maintenance")
	coverage := provider.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -8.85, "lng": 13.25, "radius_km": 30}, nil)
	require.Equal(t, http.StatusOK, coverage.status, coverage.text)
	again := provider.do(t, http.MethodPut, "/api/v1/providers/me/coverage", map[string]any{"lat": -8.86, "lng": 13.26, "radius_km": 30}, nil)
	require.Equal(t, http.StatusOK, again.status, again.text)

	ctx := context.Background()
	var lat float64
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT a.lat FROM providers p JOIN addresses a ON a.id = p.base_address_id WHERE p.user_id = $1`, provider.id).Scan(&lat))
	assert.InDelta(t, -8.86, lat, 1e-9, "the base is updated in place")

	draft := signUpProvider(t, "Com Morada")
	personal := draft.do(t, http.MethodPut, "/api/v1/providers/me/personal", map[string]any{"birth_date": "1990-05-20", "id_number": "1", "address": map[string]any{"line": "Sem pin"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, personal.status, "latitude and longitude are mandatory")
	saved := draft.do(t, http.MethodPut, "/api/v1/providers/me/personal", map[string]any{"birth_date": "1990-05-20", "id_number": "1", "address": map[string]any{"lat": -8.83, "lng": 13.24, "line": "Rua Rainha Ginga, 29"}}, nil)
	require.Equal(t, http.StatusOK, saved.status, saved.text)
	assert.Equal(t, "Rua Rainha Ginga, 29", saved.body["address"].(map[string]any)["line"])
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT a.lat FROM providers p JOIN addresses a ON a.id = p.home_address_id WHERE p.user_id = $1`, draft.id).Scan(&lat))
	assert.InDelta(t, -8.83, lat, 1e-9)
}
