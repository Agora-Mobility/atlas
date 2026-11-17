package logger

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func fieldMap(fields []zap.Field) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.Key] = f.String
	}
	return out
}

func TestWithContextAddsTraceIDs(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(WithCorrelationID(context.Background(), "corr-1"), sc)

	got := fieldMap(WithContext(ctx))

	if got[CorrelationIDField] != "corr-1" {
		t.Errorf("correlation_id = %q", got[CorrelationIDField])
	}
	if got[TraceIDField] != traceID.String() {
		t.Errorf("trace_id = %q, want %q", got[TraceIDField], traceID)
	}
	if got[SpanIDField] != spanID.String() {
		t.Errorf("span_id = %q, want %q", got[SpanIDField], spanID)
	}
}

func TestWithContextOmitsTraceIDsWithoutSpan(t *testing.T) {
	got := fieldMap(WithContext(context.Background()))
	if _, ok := got[TraceIDField]; ok {
		t.Error("trace_id must be absent when the context carries no span")
	}
}
