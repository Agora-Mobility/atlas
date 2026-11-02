//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/queue"
)

var stopA, stopB = point{-8.8250, 13.2450}, point{-8.8300, 13.2200}

func pointsOf(points ...point) []map[string]any {
	out := make([]map[string]any, 0, len(points))
	for _, p := range points {
		out = append(out, map[string]any{"lat": p.Lat, "lng": p.Lng})
	}
	return out
}

func taxiQuote(t *testing.T, customer *person, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"category": "taxi", "pickup": pickup, "destination": target}
	for key, value := range extra {
		body[key] = value
	}
	res := customer.post(t, "/api/v1/fare-estimates", body)
	require.Equal(t, http.StatusCreated, res.status, res.text)
	return res.body["items"].([]any)[0].(map[string]any)
}

func askFor(t *testing.T, customer *person, option map[string]any, extra map[string]any) result {
	t.Helper()
	payload := map[string]any{"service_id": option["service_id"], "fare_estimate_id": option["fare_estimate_id"], "pickup": pickup, "destination": target}
	for key, value := range extra {
		payload[key] = value
	}
	return customer.do(t, http.MethodPost, "/api/v1/requests", payload, map[string]string{"Idempotency-Key": uuid.NewString()})
}

func TestARequestWithStopsCannotUseThePriceOfARouteWithout(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	cheap := taxiQuote(t, customer, nil)

	wrong := askFor(t, customer, cheap, map[string]any{"stops": pointsOf(stopA, stopB)})
	assert.Equal(t, "FARE_ESTIMATE_INVALID", wrong.code(), wrong.text)

	priced := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA, stopB)})
	created := askFor(t, customer, priced, map[string]any{"stops": pointsOf(stopA, stopB)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, priced["total_cents"], created.num("fare_cents"))

	other := newCustomer(t)
	fewer := taxiQuote(t, other, map[string]any{"stops": pointsOf(stopA, stopB)})
	assert.Equal(t, "FARE_ESTIMATE_INVALID", askFor(t, other, fewer, map[string]any{"stops": pointsOf(stopA)}).code(), "removing a stop needs a new price")
}

func TestStopFeeAndStopLimitComeFromTheDatabase(t *testing.T) {
	clearOnlineProviders(t)
	admin := staffToken(t)
	customer := newCustomer(t)
	withoutFee := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)})

	var current map[string]any
	list := admin.get(t, "/api/v1/admin/tariffs")
	require.Equal(t, http.StatusOK, list.status, list.text)
	for _, item := range list.body["items"].([]any) {
		tariff := item.(map[string]any)
		if tariff["service_id"] == withoutFee["service_id"] {
			current = tariff
		}
	}
	require.NotNil(t, current)
	publish := func(stopFee int) {
		body := map[string]any{"service_id": current["service_id"], "currency": current["currency"], "base_fee_cents": current["base_fee_cents"], "per_km_fee_cents": current["per_km_fee_cents"],
			"per_minute_fee_cents": current["per_minute_fee_cents"], "minimum_fee_cents": current["minimum_fee_cents"], "free_wait_seconds": current["free_wait_seconds"], "commission_bps": current["commission_bps"], "stop_fee_cents": stopFee}
		res := admin.post(t, "/api/v1/admin/tariffs", body)
		require.Equal(t, http.StatusCreated, res.status, res.text)
	}
	publish(25_000)
	t.Cleanup(func() { publish(0) })
	withFee := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)})
	assert.InDelta(t, withoutFee["total_cents"].(float64)+25_000, withFee["total_cents"].(float64), 1)

	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target, "stops": pointsOf(stopA, stopB, stopA, stopB)})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status, "4 stops are over the limit of 3")
	setPolicy(t, "taxi", "max_stops = 4")
	res = customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target, "stops": pointsOf(stopA, stopB, stopA, stopB)})
	assert.Equal(t, http.StatusCreated, res.status, "the limit is configurable: %s", res.text)
}

func TestAScheduledPriceUsesTheHourOfTheTrip(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	location, err := time.LoadLocation("Africa/Luanda")
	require.NoError(t, err)
	tomorrow := time.Now().In(location).AddDate(0, 0, 1)
	day := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 12, 0, 0, 0, location)
	night := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 23, 0, 0, 0, location)
	admin := staffToken(t)
	var current map[string]any
	option := taxiQuote(t, customer, nil)
	for _, item := range admin.get(t, "/api/v1/admin/tariffs").body["items"].([]any) {
		if tariff := item.(map[string]any); tariff["service_id"] == option["service_id"] {
			current = tariff
		}
	}
	publish := func(nightFee int) {
		res := admin.post(t, "/api/v1/admin/tariffs", map[string]any{"service_id": current["service_id"], "currency": current["currency"], "base_fee_cents": current["base_fee_cents"], "per_km_fee_cents": current["per_km_fee_cents"],
			"per_minute_fee_cents": current["per_minute_fee_cents"], "minimum_fee_cents": current["minimum_fee_cents"], "free_wait_seconds": current["free_wait_seconds"], "commission_bps": current["commission_bps"], "night_fee_cents": nightFee, "night_start_hour": 22, "night_end_hour": 6})
		require.Equal(t, http.StatusCreated, res.status, res.text)
	}
	publish(50_000)
	t.Cleanup(func() { publish(0) })

	atDay := taxiQuote(t, customer, map[string]any{"scheduled_for": day.Format(time.RFC3339)})
	atNight := taxiQuote(t, customer, map[string]any{"scheduled_for": night.Format(time.RFC3339)})
	assert.InDelta(t, atDay["total_cents"].(float64)+50_000, atNight["total_cents"].(float64), 1, "the night fee follows the hour of the trip, not the hour of the request")

	assert.Equal(t, "FARE_ESTIMATE_INVALID", askFor(t, customer, atNight, map[string]any{"scheduled_for": day.Format(time.RFC3339)}).code(), "a price for another hour does not serve")
	assert.Equal(t, "FARE_ESTIMATE_INVALID", askFor(t, customer, atNight, nil).code(), "a price for later does not serve for now")
	created := askFor(t, customer, atNight, map[string]any{"scheduled_for": night.Format(time.RFC3339)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, atNight["total_cents"], created.num("fare_cents"))

	odd := day.Add(7 * time.Minute).Format(time.RFC3339)
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target, "scheduled_for": odd})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status, "12:07 is not a 15 minute slot")
}

func TestAnEstimateServesOnlyOneRequest(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)

	var wg sync.WaitGroup
	statuses := make([]int, 5)
	for i := range statuses {
		wg.Go(func() { statuses[i] = askFor(t, customer, option, nil).status })
	}
	wg.Wait()
	created := 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			created++
		} else {
			assert.Equal(t, http.StatusConflict, status)
		}
	}
	assert.Equal(t, 1, created, "five keys, one estimate: %v", statuses)

	var active int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM service_requests WHERE customer_id = $1 AND status NOT IN ('cancelled', 'expired')`, customer.id).Scan(&active))
	assert.Equal(t, 1, active)

	second := taxiQuote(t, customer, nil)
	assert.Equal(t, "DUPLICATE_REQUEST", askFor(t, customer, second, nil).code(), "a new price for the same trip does not start a second search")

	stopped := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)})
	require.Equal(t, http.StatusCreated, askFor(t, customer, stopped, map[string]any{"stops": pointsOf(stopA)}).status, "another trip can be active at the same time")
}

func TestThePassengerOfAnotherPersonIsKnownToTheDriverAndGetsTheDetails(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	const phone = "+244923456001"
	created := askFor(t, customer, option, map[string]any{"passenger": map[string]string{"name": "Maria Neto", "phone": phone}})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)

	seen := driver.get(t, "/api/v1/requests/"+created.str("id"))
	assert.Equal(t, "Maria Neto", seen.str("passenger_name"))
	assert.Equal(t, phone, seen.str("passenger_phone"))

	require.Eventually(t, func() bool { return len(env.sms.messagesTo(phone)) > 0 }, 15*time.Second, 50*time.Millisecond)
	text := env.sms.messagesTo(phone)[0]
	assert.Contains(t, text, "táxi")
	assert.Contains(t, text, "Pessoa Teste", "the driver")
	assert.Contains(t, text, "LD-45-21-AF", "the plate")
	match := regexp.MustCompile(`/v/([A-Za-z0-9_-]{20,})`).FindStringSubmatch(text)
	require.Len(t, match, 2, "the tracking link: %s", text)
	public := (&person{client: customerClient, api: env.api.baseURL}).get(t, "/api/v1/public/trips/"+match[1])
	require.Equal(t, http.StatusOK, public.status, public.text)
	assert.Equal(t, "accepted", public.str("status"))
	assert.NotContains(t, public.text, phone, "the link shows no phone")
}

func TestServerFieldsInDetailsAreNotKept(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	created := askFor(t, customer, option, map[string]any{"details": map[string]any{"volume_m3": 999, "note": "ok"}})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	details := created.body["details"].(map[string]any)
	assert.NotContains(t, details, "volume_m3")
	assert.Equal(t, "ok", details["note"])
}

func TestEventsPublishedBeforeTheBindingAreParkedAndAfterItArrive(t *testing.T) {
	ctx := context.Background()
	container := env.api.container
	name := "topology.test." + uuid.NewString()[:8]
	consumer := queue.NewConsumer(container.Queue, container.Deduplicator, queue.ConsumerConfig{
		Queue: name, BindingKeys: []string{name + ".*.v1"}, Prefetch: 1, MaxRetries: 1, RetryDelay: time.Second, Workers: 1,
		Handler: nil,
	})
	publish := func(kind string) {
		id := uuid.New()
		body, err := json.Marshal(events.Envelope{EventID: id, EventType: kind, Version: 1, OccurredAt: time.Now(), Payload: json.RawMessage(`{}`)})
		require.NoError(t, err)
		require.NoError(t, container.Publisher.Publish(ctx, queue.Message{ID: id.String(), Type: kind, RoutingKey: name + "." + kind + ".v1", Body: body}))
	}
	channel, err := container.Queue.Channel()
	require.NoError(t, err)
	defer func() { _ = channel.Close() }()

	publish("before")
	require.NoError(t, consumer.EnsureTopology())
	publish("after")

	require.Eventually(t, func() bool {
		state, err := channel.QueueDeclarePassive(name, true, false, false, false, map[string]any{"x-queue-type": "quorum"})
		return err == nil && state.Messages == 1
	}, 10*time.Second, 50*time.Millisecond, "only what comes after the binding reaches the queue, so the relay starts after it")
}

func TestADriverCannotConfirmCashOnARequestPaidDigitally(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	option := taxiQuote(t, customer, nil)
	created := askFor(t, customer, option, map[string]any{"payment_method": "gpo"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	for _, step := range []string{"en-route", "arrive", "start", "complete"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}

	res := driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil)
	assert.Equal(t, "CASH_NOT_EXPECTED", res.code(), "this would erase the debt of the customer: %s", res.text)
	assert.Equal(t, "PAYMENT_OUTSTANDING", askFor(t, customer, taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA)}), map[string]any{"stops": pointsOf(stopA)}).code(), "the debt stays")
}
