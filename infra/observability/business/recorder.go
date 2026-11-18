package business

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
	"github.com/Agora-Mobility/atlas/shared/txhook"
)

// Buckets in kwanzas: from a short taxi ride in Luanda to a removal or a
// workshop repair.
var amountBuckets = []float64{500, 1000, 2000, 3500, 5000, 7500, 10000, 15000, 25000, 50000, 100000, 250000, 500000, 1000000}

// A GPO payment settles in seconds or minutes; a reference can take a day.
var settleBuckets = []float64{5, 10, 20, 30, 60, 120, 300, 600, 1800, 3600, 7200, 21600, 86400, 259200}

// From the request to a provider accepting it, for requests made for now.
var acceptBuckets = []float64{5, 10, 15, 21, 30, 45, 60, 90, 120, 180, 300, 600}

type instruments struct {
	requestsCreated   metric.Int64Counter
	requestsCancelled metric.Int64Counter
	requestFare       metric.Float64Histogram
	acceptDuration    metric.Float64Histogram
	paymentsCreated   metric.Int64Counter
	paymentsSettled   metric.Int64Counter
	paymentAmount     metric.Float64Histogram
	settleDuration    metric.Float64Histogram
	refundAmount      metric.Float64Histogram
	usersRegistered   metric.Int64Counter
	providersReviewed metric.Int64Counter
	bookingChanges    metric.Int64Counter
}

func newInstruments(m metric.Meter) (*instruments, error) {
	var errs []error
	counter := func(name, unit, desc string) metric.Int64Counter {
		c, err := m.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
		errs = append(errs, err)
		return c
	}
	histogram := func(name, unit, desc string, buckets []float64) metric.Float64Histogram {
		h, err := m.Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc), metric.WithExplicitBucketBoundaries(buckets...))
		errs = append(errs, err)
		return h
	}
	in := &instruments{
		requestsCreated:   counter("atlas.business.requests.created", "{request}", "Service requests created."),
		requestsCancelled: counter("atlas.business.requests.cancelled", "{request}", "Service requests cancelled, by who cancelled and whether a provider had accepted."),
		requestFare:       histogram("atlas.business.requests.fare", "{AOA}", "Final fare of completed requests, in currency units.", amountBuckets),
		acceptDuration:    histogram("atlas.business.requests.accept.duration", "s", "Time from an immediate request until a provider accepts it.", acceptBuckets),
		paymentsCreated:   counter("atlas.business.payments.created", "{payment}", "Payments started by customers or confirmed in cash."),
		paymentsSettled:   counter("atlas.business.payments.settled", "{payment}", "Payments that reached a final status: paid, failed, expired or duplicate."),
		paymentAmount:     histogram("atlas.business.payments.amount", "{AOA}", "Amount of paid payments, in currency units. The sum is the gross revenue.", amountBuckets),
		settleDuration:    histogram("atlas.business.payments.settle.duration", "s", "Time from a payment's creation until it is paid or failed.", settleBuckets),
		refundAmount:      histogram("atlas.business.refunds.amount", "{AOA}", "Refunded amounts, in currency units.", amountBuckets),
		usersRegistered:   counter("atlas.business.users.registered", "{user}", "Self-registrations, by role and client app."),
		providersReviewed: counter("atlas.business.providers.reviewed", "{provider}", "Provider reviews by outcome."),
		bookingChanges:    counter("atlas.business.bookings.transitions", "{booking}", "Workshop booking state changes."),
	}
	return in, errors.Join(errs...)
}

// Recorder implements services.IBusinessEvents. Every method only queues the
// emission on the open transaction (see txhook): nothing is counted for a
// change that rolls back, and a panic while emitting is swallowed.
type Recorder struct {
	in *instruments
}

var _ services.IBusinessEvents = (*Recorder)(nil)

// New builds the recorder on mp. Use NewMeterProvider in production and a
// provider with a manual reader in tests.
func New(mp metric.MeterProvider) (*Recorder, error) {
	in, err := newInstruments(mp.Meter(MeterName))
	if err != nil {
		return nil, err
	}
	return &Recorder{in: in}, nil
}

func (r *Recorder) after(ctx context.Context, fn func()) {
	txhook.AfterCommit(ctx, fn)
}

func (r *Recorder) RequestTransitioned(ctx context.Context, e services.RequestTransition) {
	r.after(ctx, func() {
		cat := category(e.Category)
		method := oneOf(paymentMethods, e.PaymentMethod)
		actor := oneOf(requestActors, e.ActorType)
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.String("request.id", e.RequestID.String()),
			attribute.String("request.category", cat),
			attribute.String("request.status.from", e.From),
			attribute.String("request.status.to", e.To),
			attribute.String("actor.type", actor),
		)

		fields := []zap.Field{
			zap.String("request_id", e.RequestID.String()), zap.String("category", cat),
			zap.String("from", e.From), zap.String("to", e.To), zap.String("actor_type", actor),
		}
		switch e.To {
		case "created":
			r.in.requestsCreated.Add(ctx, 1, metric.WithAttributes(
				attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("mode", mode(e.Scheduled))))
			emit(ctx, "request_created", append(fields, zap.String("payment_method", method), zap.String("mode", mode(e.Scheduled)))...)
		case "accepted":
			if !e.Scheduled && !e.CreatedAt.IsZero() && e.At.After(e.CreatedAt) {
				r.in.acceptDuration.Record(ctx, e.At.Sub(e.CreatedAt).Seconds(), metric.WithAttributes(attribute.String("category", cat)))
			}
			emit(ctx, "request_accepted", fields...)
		case "completed":
			if e.FareCents != nil {
				fare := float64(*e.FareCents) / 100
				span.SetAttributes(attribute.Int64("request.fare_cents", *e.FareCents), attribute.String("request.currency", e.Currency))
				r.in.requestFare.Record(ctx, fare, metric.WithAttributes(
					attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("currency", currency(e.Currency))))
				fields = append(fields, zap.Int64("fare_cents", *e.FareCents), zap.String("currency", e.Currency))
			}
			emit(ctx, "request_completed", append(fields, zap.String("payment_method", method))...)
		case "cancelled":
			st := stage(e.From)
			r.in.requestsCancelled.Add(ctx, 1, metric.WithAttributes(
				attribute.String("category", cat), attribute.String("actor_type", actor), attribute.String("stage", st)))
			emit(ctx, "request_cancelled", append(fields, zap.String("stage", st))...)
		case "expired":
			emit(ctx, "request_expired", fields...)
		}
	})
}

func paymentAttrs(e services.PaymentEvent) (cat, method, kind, gw string) {
	method = oneOf(paymentMethods, e.Method)
	return category(e.Category), method, oneOf(paymentKinds, e.Kind), gateway(method)
}

func paymentFields(e services.PaymentEvent, cat, method, kind, gw string) []zap.Field {
	return []zap.Field{
		zap.String("payment_id", e.PaymentID.String()), zap.String("request_id", e.RequestID.String()),
		zap.String("category", cat), zap.String("payment_method", method), zap.String("kind", kind), zap.String("gateway", gw),
		zap.Int64("amount_cents", e.AmountCents), zap.String("currency", e.Currency),
	}
}

func setPaymentSpan(ctx context.Context, e services.PaymentEvent, method, kind, gw, status string) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("payment.id", e.PaymentID.String()),
		attribute.String("request.id", e.RequestID.String()),
		attribute.String("payment.method", method),
		attribute.String("payment.kind", kind),
		attribute.String("payment.gateway", gw),
		attribute.String("payment.status", status),
		attribute.Int64("payment.amount_cents", e.AmountCents),
		attribute.String("payment.currency", e.Currency),
	)
}

func (r *Recorder) PaymentCreated(ctx context.Context, e services.PaymentEvent) {
	r.after(ctx, func() {
		cat, method, kind, gw := paymentAttrs(e)
		setPaymentSpan(ctx, e, method, kind, gw, "pending")
		r.in.paymentsCreated.Add(ctx, 1, metric.WithAttributes(
			attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("kind", kind), attribute.String("gateway", gw)))
		emit(ctx, "payment_created", paymentFields(e, cat, method, kind, gw)...)
	})
}

func (r *Recorder) PaymentSettled(ctx context.Context, e services.PaymentEvent) {
	r.after(ctx, func() {
		cat, method, kind, gw := paymentAttrs(e)
		status := e.Status
		if e.Duplicate {
			status = "duplicate"
		}
		status = oneOf(settledStates, status)
		reason := failureReason(status, e.Declined)
		source := oneOf(paymentSources, e.Source)
		setPaymentSpan(ctx, e, method, kind, gw, status)
		if reason != "none" {
			span := trace.SpanFromContext(ctx)
			span.SetAttributes(attribute.String("payment.error_reason", reason), attribute.String("payment.failure_code", e.FailureCode))
		}

		r.in.paymentsSettled.Add(ctx, 1, metric.WithAttributes(
			attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("kind", kind),
			attribute.String("gateway", gw), attribute.String("status", status), attribute.String("error_reason", reason)))
		if status == "paid" {
			r.in.paymentAmount.Record(ctx, float64(e.AmountCents)/100, metric.WithAttributes(
				attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("kind", kind), attribute.String("currency", currency(e.Currency))))
		}
		// Cash and wallet payments are created already paid: only a gateway
		// payment has a wait worth measuring.
		if gw == "appypay" && (status == "paid" || status == "failed") && !e.CreatedAt.IsZero() && !e.At.Before(e.CreatedAt) {
			r.in.settleDuration.Record(ctx, e.At.Sub(e.CreatedAt).Seconds(), metric.WithAttributes(
				attribute.String("payment_method", method), attribute.String("status", status)))
		}

		fields := append(paymentFields(e, cat, method, kind, gw), zap.String("status", status), zap.String("source", source))
		if reason != "none" {
			fields = append(fields, zap.String("error_reason", reason), zap.String("failure_code", e.FailureCode))
		}
		emit(ctx, "payment_"+status, fields...)
	})
}

func (r *Recorder) PaymentRefunded(ctx context.Context, e services.RefundEvent) {
	r.after(ctx, func() {
		cat, method, kind := category(e.Category), oneOf(paymentMethods, e.Method), oneOf(paymentKinds, e.Kind)
		source := oneOf(refundSources, e.Source)
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("payment.id", e.PaymentID.String()),
			attribute.Int64("refund.amount_cents", e.AmountCents),
			attribute.String("refund.source", source),
		)
		r.in.refundAmount.Record(ctx, float64(e.AmountCents)/100, metric.WithAttributes(
			attribute.String("category", cat), attribute.String("payment_method", method), attribute.String("kind", kind),
			attribute.String("source", source), attribute.String("currency", currency(e.Currency))))
		emit(ctx, "payment_refunded",
			zap.String("payment_id", e.PaymentID.String()), zap.String("request_id", e.RequestID.String()),
			zap.String("category", cat), zap.String("payment_method", method), zap.String("kind", kind),
			zap.Int64("amount_cents", e.AmountCents), zap.String("currency", e.Currency), zap.String("source", source), zap.Bool("full", e.Full))
	})
}

func (r *Recorder) UserRegistered(ctx context.Context, e services.UserRegistration) {
	r.after(ctx, func() {
		role, channel := oneOf(roles, e.Role), oneOf(channels, e.Channel)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("user.id", e.UserID.String()), attribute.String("user.role", role))
		r.in.usersRegistered.Add(ctx, 1, metric.WithAttributes(attribute.String("role", role), attribute.String("channel", channel)))
		emit(ctx, "user_registered", zap.String("user_id", e.UserID.String()), zap.String("role", role), zap.String("channel", channel))
	})
}

func (r *Recorder) ProviderReviewed(ctx context.Context, e services.ProviderReview) {
	r.after(ctx, func() {
		outcome := oneOf(reviewOutcomes, e.Outcome)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("provider.id", e.ProviderID.String()), attribute.String("provider.review", outcome))
		r.in.providersReviewed.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
		emit(ctx, "provider_reviewed", zap.String("provider_id", e.ProviderID.String()), zap.String("outcome", outcome))
	})
}

func (r *Recorder) BookingTransitioned(ctx context.Context, e services.BookingTransition) {
	r.after(ctx, func() {
		status, actor := oneOf(bookingStates, e.To), oneOf(bookingActors, e.ActorType)
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("booking.id", e.BookingID.String()),
			attribute.String("booking.status.from", e.From),
			attribute.String("booking.status.to", status),
		)
		r.in.bookingChanges.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status), attribute.String("actor_type", actor)))
		if status == "confirmed" || status == "cancelled" || status == "completed" {
			emit(ctx, "booking_"+status, zap.String("booking_id", e.BookingID.String()), zap.String("request_id", e.RequestID.String()),
				zap.String("from", e.From), zap.String("actor_type", actor))
		}
	})
}

func (r *Recorder) StartStep(ctx context.Context, name string) (context.Context, func(error)) {
	ctx, span := tracing.Tracer().Start(ctx, name)
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// emit writes one structured line per business event. The event name is a
// JSON field, not a Loki label: query it with `| json | event="..."`.
// logger.FromContext adds trace_id and span_id when there is a span.
func emit(ctx context.Context, event string, fields ...zap.Field) {
	logger.FromContext(ctx).Info("business event", append([]zap.Field{zap.String("event", event)}, fields...)...)
}
