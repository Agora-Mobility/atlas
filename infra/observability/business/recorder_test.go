package business_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/business"
	"github.com/Agora-Mobility/atlas/shared/txhook"
)

func newRecorder(t *testing.T) (*business.Recorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	rec, err := business.New(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)
	return rec, reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func attrs(set attribute.Set) map[string]string {
	out := map[string]string{}
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func counterPoints(t *testing.T, m metricdata.Metrics) []metricdata.DataPoint[int64] {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "%s is not an int64 counter", m.Name)
	return sum.DataPoints
}

func histogramPoints(t *testing.T, m metricdata.Metrics) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	h, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "%s is not a float64 histogram", m.Name)
	return h.DataPoints
}

var (
	created = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	cents   = int64(450000)
)

func TestPaymentPaidCountsRevenueAndDuration(t *testing.T) {
	rec, reader := newRecorder(t)
	rec.PaymentSettled(context.Background(), services.PaymentEvent{
		PaymentID: uuid.New(), RequestID: uuid.New(), Category: "taxi", Method: "gpo", Kind: "fare", Status: "paid",
		AmountCents: cents, Currency: "AOA", Source: "webhook", CreatedAt: created, At: created.Add(40 * time.Second),
	})

	got := collect(t, reader)
	points := counterPoints(t, got["atlas.business.payments.settled"])
	require.Len(t, points, 1)
	assert.Equal(t, int64(1), points[0].Value)
	assert.Equal(t, map[string]string{
		"category": "taxi", "payment_method": "gpo", "kind": "fare", "gateway": "appypay", "status": "paid", "error_reason": "none",
	}, attrs(points[0].Attributes))

	amount := histogramPoints(t, got["atlas.business.payments.amount"])
	require.Len(t, amount, 1)
	assert.InDelta(t, 4500.0, amount[0].Sum, 0.001, "cents become kwanzas")
	assert.Equal(t, map[string]string{"category": "taxi", "payment_method": "gpo", "kind": "fare", "currency": "AOA"}, attrs(amount[0].Attributes))

	duration := histogramPoints(t, got["atlas.business.payments.settle.duration"])
	require.Len(t, duration, 1)
	assert.InDelta(t, 40.0, duration[0].Sum, 0.001)
}

func TestPaymentFailureIsClassifiedWithoutRevenue(t *testing.T) {
	tests := []struct {
		name     string
		event    services.PaymentEvent
		status   string
		reason   string
		duration bool
	}{
		{"declined when charged", services.PaymentEvent{Status: "failed", Declined: true, FailureCode: "4012"}, "failed", "declined", true},
		{"failed later", services.PaymentEvent{Status: "failed"}, "failed", "charge_failed", true},
		{"expired", services.PaymentEvent{Status: "expired"}, "expired", "expired", false},
		{"duplicate goes to the wallet", services.PaymentEvent{Status: "paid", Duplicate: true}, "duplicate", "none", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reader := newRecorder(t)
			e := tt.event
			e.PaymentID, e.RequestID, e.Category, e.Method, e.Kind, e.AmountCents, e.Currency = uuid.New(), uuid.New(), "towing", "reference", "fare", cents, "AOA"
			e.CreatedAt, e.At = created, created.Add(time.Hour)
			rec.PaymentSettled(context.Background(), e)

			got := collect(t, reader)
			points := counterPoints(t, got["atlas.business.payments.settled"])
			require.Len(t, points, 1)
			labels := attrs(points[0].Attributes)
			assert.Equal(t, tt.status, labels["status"])
			assert.Equal(t, tt.reason, labels["error_reason"])
			assert.Equal(t, "appypay", labels["gateway"])
			assert.NotContains(t, got, "atlas.business.payments.amount", "only paid payments are revenue")
			_, hasDuration := got["atlas.business.payments.settle.duration"]
			assert.Equal(t, tt.duration, hasDuration)
		})
	}
}

func TestCashHasNoSettleDuration(t *testing.T) {
	rec, reader := newRecorder(t)
	rec.PaymentSettled(context.Background(), services.PaymentEvent{
		PaymentID: uuid.New(), Category: "taxi", Method: "cash", Kind: "fare", Status: "paid", AmountCents: cents, Currency: "AOA", CreatedAt: created, At: created,
	})
	got := collect(t, reader)
	assert.Contains(t, got, "atlas.business.payments.amount")
	assert.NotContains(t, got, "atlas.business.payments.settle.duration")
}

func TestUnknownLabelValuesBecomeOther(t *testing.T) {
	rec, reader := newRecorder(t)
	rec.PaymentCreated(context.Background(), services.PaymentEvent{
		PaymentID: uuid.New(), Category: strings.Repeat("x", 40), Method: "crypto", Kind: "bribe", Currency: "AOA",
	})

	points := counterPoints(t, collect(t, reader)["atlas.business.payments.created"])
	require.Len(t, points, 1)
	assert.Equal(t, map[string]string{"category": "other", "payment_method": "other", "kind": "other", "gateway": "other"}, attrs(points[0].Attributes))
}

func TestNoLabelCarriesAnIdentifier(t *testing.T) {
	rec, reader := newRecorder(t)
	ctx := context.Background()
	id := uuid.New()
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: id, Category: "taxi", To: "created", ActorType: "customer", PaymentMethod: "cash"})
	rec.PaymentCreated(ctx, services.PaymentEvent{PaymentID: id, RequestID: id, Category: "taxi", Method: "cash", Kind: "fare"})
	rec.UserRegistered(ctx, services.UserRegistration{UserID: id, Role: "customer", Channel: "customer_app"})
	rec.ProviderReviewed(ctx, services.ProviderReview{ProviderID: id, Outcome: "approved"})
	rec.BookingTransitioned(ctx, services.BookingTransition{BookingID: id, RequestID: id, To: "confirmed", ActorType: "system"})

	for name, m := range collect(t, reader) {
		sum, ok := m.Data.(metricdata.Sum[int64])
		if !ok {
			continue
		}
		for _, p := range sum.DataPoints {
			for _, v := range attrs(p.Attributes) {
				assert.NotEqual(t, id.String(), v, "%s carries an id as a label", name)
			}
		}
	}
}

func TestRequestTransitions(t *testing.T) {
	rec, reader := newRecorder(t)
	ctx := context.Background()
	fare := int64(250000)
	id := uuid.New()

	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: id, Category: "taxi", To: "created", ActorType: "customer", PaymentMethod: "cash"})
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: id, Category: "taxi", From: "offered", To: "accepted", ActorType: "provider", CreatedAt: created, At: created.Add(18 * time.Second)})
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: id, Category: "taxi", From: "in_progress", To: "completed", ActorType: "provider", PaymentMethod: "cash", FareCents: &fare, Currency: "AOA"})
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: uuid.New(), Category: "taxi", From: "provider_en_route", To: "cancelled", ActorType: "customer"})
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: uuid.New(), Category: "taxi", From: "searching", To: "cancelled", ActorType: "system"})
	// A scheduled request is accepted long before it starts: not a measure
	// of how fast providers answer.
	rec.RequestTransitioned(ctx, services.RequestTransition{RequestID: uuid.New(), Category: "taxi", From: "scheduled", To: "accepted", Scheduled: true, CreatedAt: created, At: created.Add(48 * time.Hour)})

	got := collect(t, reader)
	createdPoints := counterPoints(t, got["atlas.business.requests.created"])
	require.Len(t, createdPoints, 1)
	assert.Equal(t, map[string]string{"category": "taxi", "payment_method": "cash", "mode": "now"}, attrs(createdPoints[0].Attributes))

	accept := histogramPoints(t, got["atlas.business.requests.accept.duration"])
	require.Len(t, accept, 1)
	assert.Equal(t, uint64(1), accept[0].Count)
	assert.InDelta(t, 18.0, accept[0].Sum, 0.001)

	fares := histogramPoints(t, got["atlas.business.requests.fare"])
	require.Len(t, fares, 1)
	assert.InDelta(t, 2500.0, fares[0].Sum, 0.001)

	cancelled := counterPoints(t, got["atlas.business.requests.cancelled"])
	stages := []string{}
	for _, p := range cancelled {
		a := attrs(p.Attributes)
		stages = append(stages, a["actor_type"]+"/"+a["stage"])
	}
	sort.Strings(stages)
	assert.Equal(t, []string{"customer/after_accept", "system/before_accept"}, stages)
}

func TestEmitsOnlyAfterCommit(t *testing.T) {
	rec, reader := newRecorder(t)
	ctx, hooks := txhook.WithHooks(context.Background())
	rec.PaymentCreated(ctx, services.PaymentEvent{PaymentID: uuid.New(), Category: "taxi", Method: "gpo", Kind: "fare"})

	assert.NotContains(t, collect(t, reader), "atlas.business.payments.created", "nothing before the commit")
	hooks.Run()
	assert.Contains(t, collect(t, reader), "atlas.business.payments.created")
}

func TestRolledBackChangeEmitsNothing(t *testing.T) {
	rec, reader := newRecorder(t)
	ctx, _ := txhook.WithHooks(context.Background())
	rec.PaymentSettled(ctx, services.PaymentEvent{PaymentID: uuid.New(), Status: "paid", AmountCents: cents})
	// The transaction manager never runs the hooks of a rollback.
	assert.Empty(t, collect(t, reader))
}

func TestSpanAttributesAndSteps(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	ctx, span := tp.Tracer("test").Start(context.Background(), "POST /payments")

	rec, _ := newRecorder(t)
	rec.PaymentSettled(ctx, services.PaymentEvent{
		PaymentID: uuid.New(), RequestID: uuid.New(), Category: "taxi", Method: "gpo", Kind: "fare", Status: "failed", Declined: true, FailureCode: "4012", AmountCents: cents, Currency: "AOA",
	})
	span.End()

	ended := spans.Ended()
	require.Len(t, ended, 1)
	got := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	assert.Equal(t, "gpo", got["payment.method"])
	assert.Equal(t, "failed", got["payment.status"])
	assert.Equal(t, "declined", got["payment.error_reason"])
	assert.Equal(t, "4012", got["payment.failure_code"])
	assert.Equal(t, "450000", got["payment.amount_cents"])
}

func TestStartStepRecordsTheError(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	rec, _ := newRecorder(t)
	_, end := rec.StartStep(context.Background(), "payment.charge")
	end(errors.New("gateway down"))

	ended := spans.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "payment.charge", ended[0].Name())
	assert.Equal(t, codes.Error, ended[0].Status().Code)
}

func TestPrometheusNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	mp, err := business.NewMeterProvider(reg)
	require.NoError(t, err)
	rec, err := business.New(mp)
	require.NoError(t, err)

	ctx := context.Background()
	fare := int64(100000)
	rec.RequestTransitioned(ctx, services.RequestTransition{Category: "taxi", To: "created", PaymentMethod: "cash"})
	rec.RequestTransitioned(ctx, services.RequestTransition{Category: "taxi", From: "offered", To: "accepted", CreatedAt: created, At: created.Add(time.Second)})
	rec.RequestTransitioned(ctx, services.RequestTransition{Category: "taxi", To: "completed", FareCents: &fare, Currency: "AOA"})
	rec.RequestTransitioned(ctx, services.RequestTransition{Category: "taxi", To: "cancelled"})
	rec.PaymentCreated(ctx, services.PaymentEvent{Method: "gpo"})
	rec.PaymentSettled(ctx, services.PaymentEvent{Method: "gpo", Status: "paid", AmountCents: 100, CreatedAt: created, At: created})
	rec.PaymentRefunded(ctx, services.RefundEvent{Method: "gpo", AmountCents: 100, Source: "admin"})
	rec.UserRegistered(ctx, services.UserRegistration{Role: "customer"})
	rec.ProviderReviewed(ctx, services.ProviderReview{Outcome: "approved"})
	rec.BookingTransitioned(ctx, services.BookingTransition{To: "confirmed"})

	families, err := reg.Gather()
	require.NoError(t, err)
	names := []string{}
	for _, f := range families {
		names = append(names, f.GetName())
	}
	assert.ElementsMatch(t, []string{
		"atlas_business_requests_created_total",
		"atlas_business_requests_cancelled_total",
		"atlas_business_requests_fare",
		"atlas_business_requests_accept_duration_seconds",
		"atlas_business_payments_created_total",
		"atlas_business_payments_settled_total",
		"atlas_business_payments_amount",
		"atlas_business_payments_settle_duration_seconds",
		"atlas_business_refunds_amount",
		"atlas_business_users_registered_total",
		"atlas_business_providers_reviewed_total",
		"atlas_business_bookings_transitions_total",
	}, names)
}
