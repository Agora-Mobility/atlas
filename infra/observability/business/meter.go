// Package business turns the business events of Atlas into OpenTelemetry
// metrics, span attributes and structured log lines.
//
// Metrics go through the OpenTelemetry Metrics API and are exposed on the same
// /metrics endpoint as the client_golang metrics, through the OpenTelemetry
// Prometheus exporter. The scrape path (Alloy -> remote write -> Prometheus)
// stays as it is. docs/business-observability.md lists every metric.
package business

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// MeterName names the meter of every business instrument.
const MeterName = "github.com/Agora-Mobility/atlas/business"

// NewMeterProvider returns a meter provider whose instruments are served by
// reg (the registry behind /metrics). target_info and the scope labels are
// left out: the series keep the labels the existing metrics have (job,
// instance, server), and the dashboards need nothing else.
func NewMeterProvider(reg prometheus.Registerer) (*sdkmetric.MeterProvider, error) {
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(reg),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}
	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}
