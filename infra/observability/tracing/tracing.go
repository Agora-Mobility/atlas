// Package tracing configures OpenTelemetry tracing and the helpers that carry
// a trace across HTTP, the outbox and RabbitMQ.
//
// Tracing is off unless OTEL_EXPORTER_OTLP_ENDPOINT is set: the global
// provider stays a no-op and every helper keeps working.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName names the tracer of the spans Atlas creates itself.
const InstrumentationName = "github.com/Agora-Mobility/atlas"

type Options struct {
	ServiceName string
	Version     string
	Environment string
	// Endpoint is the OTLP gRPC collector, for example http://alloy:4317.
	Endpoint string
	// SampleRatio is the share of root spans (HTTP requests and consumed
	// events) exported. The collector applies tail sampling on top of it.
	SampleRatio float64
}

// Init installs the global propagator and, when an endpoint is configured,
// the tracer provider. The returned function flushes pending spans.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if strings.TrimSpace(opts.Endpoint) == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(opts.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			attribute.String("service.name", opts.ServiceName),
			attribute.String("service.version", opts.Version),
			attribute.String("deployment.environment.name", opts.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(NewSampler(opts.SampleRatio)),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxQueueSize(8192),
		),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

// NewSampler follows the parent's decision and only starts new traces at the
// edges of the system: HTTP requests and consumed events. Background loops
// (watcher, relay, reconciler) poll every second; without this filter each
// tick would become a trace of its own and drown the useful ones.
func NewSampler(ratio float64) sdktrace.Sampler {
	if ratio <= 0 || ratio > 1 {
		ratio = 1
	}
	return sdktrace.ParentBased(edgeSampler{ratio: sdktrace.TraceIDRatioBased(ratio)})
}

type edgeSampler struct {
	ratio sdktrace.Sampler
}

func (s edgeSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if p.Kind != trace.SpanKindServer && p.Kind != trace.SpanKindConsumer {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.Drop,
			Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
		}
	}
	return s.ratio.ShouldSample(p)
}

func (s edgeSampler) Description() string {
	return "EdgeSampler{" + s.ratio.Description() + "}"
}

// Tracer returns the tracer for spans Atlas creates itself.
func Tracer() trace.Tracer {
	return otel.Tracer(InstrumentationName)
}

// Inject serializes the trace context of ctx, to be stored with an event and
// restored by Extract when the event is consumed. It returns nil when ctx
// carries no trace.
func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	return carrier
}

// Extract restores a trace context serialized by Inject.
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	if len(carrier) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}

// TraceIDs returns the trace and span ids of the span in ctx, or empty
// strings when there is none.
func TraceIDs(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// SampledTraceID returns the trace id of ctx only when the trace is exported,
// so metric exemplars never point at a trace that does not exist.
func SampledTraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return ""
	}
	return sc.TraceID().String()
}

// noPropagation keeps trace headers inside Atlas: third parties get a client
// span on our side but never see our trace ids.
var noPropagation = propagation.NewCompositeTextMapPropagator()

// HTTPClient returns an http.Client whose calls appear as client spans in the
// trace of the request that made them.
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport,
			otelhttp.WithPropagators(noPropagation),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Host
			}),
		),
	}
}
