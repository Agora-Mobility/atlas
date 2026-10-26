// Package businessmetrics declares business KPIs exposed on /metrics,
// separate from the technical metrics in observability/prometheus.
package businessmetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "atlas_business"

// ExamplesCreated counts examples created. Replace with real KPIs (requests
// created, providers approved, ...) as modules land.
var ExamplesCreated = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: namespace,
	Name:      "examples_created_total",
	Help:      "Examples created through the API.",
})
