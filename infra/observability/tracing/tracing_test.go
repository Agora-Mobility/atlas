package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newProvider(t *testing.T, ratio float64) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(NewSampler(ratio)), sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, recorder
}

func TestSamplerStartsTracesOnlyAtTheEdges(t *testing.T) {
	tp, _ := newProvider(t, 1)
	tracer := tp.Tracer("test")

	cases := map[trace.SpanKind]bool{
		trace.SpanKindServer:   true,
		trace.SpanKindConsumer: true,
		trace.SpanKindInternal: false,
		trace.SpanKindClient:   false,
		trace.SpanKindProducer: false,
	}
	for kind, want := range cases {
		_, span := tracer.Start(context.Background(), "root", trace.WithSpanKind(kind))
		if got := span.SpanContext().IsSampled(); got != want {
			t.Errorf("root %s sampled = %v, want %v", kind, got, want)
		}
		span.End()
	}
}

func TestSamplerFollowsTheParent(t *testing.T) {
	tp, _ := newProvider(t, 1)
	tracer := tp.Tracer("test")

	ctx, parent := tracer.Start(context.Background(), "request", trace.WithSpanKind(trace.SpanKindServer))
	defer parent.End()
	_, child := tracer.Start(ctx, "query", trace.WithSpanKind(trace.SpanKindClient))
	defer child.End()

	if !child.SpanContext().IsSampled() {
		t.Fatal("a client span inside a sampled request must be sampled")
	}
	if child.SpanContext().TraceID() != parent.SpanContext().TraceID() {
		t.Fatal("child must stay in the parent's trace")
	}
}

func TestNewSamplerClampsInvalidRatios(t *testing.T) {
	for _, ratio := range []float64{0, -1, 2} {
		tp, _ := newProvider(t, ratio)
		_, span := tp.Tracer("test").Start(context.Background(), "root", trace.WithSpanKind(trace.SpanKindServer))
		if !span.SpanContext().IsSampled() {
			t.Errorf("ratio %v should fall back to sampling everything", ratio)
		}
		span.End()
	}
}

func TestInjectExtractCarriesTheTraceAcrossTheQueue(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp, _ := newProvider(t, 1)
	tracer := tp.Tracer("test")

	if got := Inject(context.Background()); got != nil {
		t.Fatalf("no span in context should inject nothing, got %v", got)
	}

	ctx, producer := tracer.Start(context.Background(), "request", trace.WithSpanKind(trace.SpanKindServer))
	carrier := Inject(ctx)
	producer.End()
	if carrier["traceparent"] == "" {
		t.Fatalf("traceparent missing from carrier %v", carrier)
	}

	consumerCtx, consumer := tracer.Start(Extract(context.Background(), carrier), "process", trace.WithSpanKind(trace.SpanKindConsumer))
	defer consumer.End()
	if consumer.SpanContext().TraceID() != producer.SpanContext().TraceID() {
		t.Fatal("consumer span must continue the producer's trace")
	}
	if traceID, spanID := TraceIDs(consumerCtx); traceID == "" || spanID == "" {
		t.Fatal("TraceIDs must return the ids of the active span")
	}
	if SampledTraceID(consumerCtx) == "" {
		t.Fatal("SampledTraceID must return the id of a sampled span")
	}
}

func TestSampledTraceIDIgnoresUnsampledSpans(t *testing.T) {
	tp, _ := newProvider(t, 1)
	ctx, span := tp.Tracer("test").Start(context.Background(), "tick") // internal root: dropped
	defer span.End()
	if got := SampledTraceID(ctx); got != "" {
		t.Fatalf("an unsampled span must not produce an exemplar, got %q", got)
	}
}

func TestHTTPClientDoesNotLeakTraceHeadersToThirdParties(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp, recorder := newProvider(t, 1)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(trace.NewNoopTracerProvider()) }) //nolint:staticcheck // reset global for other tests

	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("traceparent")
	}))
	defer srv.Close()

	ctx, parent := tp.Tracer("test").Start(context.Background(), "request", trace.WithSpanKind(trace.SpanKindServer))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	resp, err := HTTPClient(time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	parent.End()

	if header != "" {
		t.Fatalf("third party received traceparent %q", header)
	}
	if n := len(recorder.Ended()); n < 2 {
		t.Fatalf("want the request span and its client span, got %d spans", n)
	}
}

func TestInitWithoutEndpointKeepsTracingOff(t *testing.T) {
	shutdown, err := Init(context.Background(), Options{ServiceName: "atlas"})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
