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

var (
	FirstOfferDelivered = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "first_offer_delivered_seconds",
		Help:    "Time from the request until its first offer is acknowledged.",
		Buckets: []float64{0.5, 1, 2, 3, 5, 8, 13, 21, 34, 60, 120},
	}, []string{"category"})
	OfferAckSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "offer_ack_seconds",
		Help:    "Time from sending an offer until the app acknowledges it.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 21, 34},
	}, []string{"category", "channel"})
	OfferOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "offer_outcomes_total",
		Help: "Offers by the way they ended.",
	}, []string{"category", "outcome"})
	OfferChannelSends = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "offer_channel_sends_total",
		Help: "Offer sends by channel.",
	}, []string{"category", "channel", "result"})
	RequestOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "request_transitions_total",
		Help: "Request state changes by category and target state.",
	}, []string{"category", "status"})
	SSEConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace, Name: "sse_connections",
		Help: "Open SSE streams on this instance.",
	})
	SSEReconnects = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "sse_reconnects_total",
		Help: "SSE connections by the way they started.",
	}, []string{"mode"})
	SSEBytes = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "sse_bytes_total",
		Help: "Bytes written to SSE streams.",
	})
	CandidateSearchSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "candidate_search_seconds",
		Help:    "Duration of a candidate search.",
		Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12),
	}, []string{"source"})
	CandidateSearchCells = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "candidate_search_cells",
		Help:    "H3 cells read by a candidate search.",
		Buckets: []float64{1, 7, 19, 37, 61, 91, 127, 217, 271, 400},
	}, []string{"source"})
	RoutingCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "routing_calls_total",
		Help: "Routing provider calls by API and result.",
	}, []string{"api", "result"})
	FareQuotes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "fare_quotes_total",
		Help: "Fare estimates by route source and whether they became a request.",
	}, []string{"category", "route_source", "stage"})
	FareBoosts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "fare_boosts_total",
		Help: "Uses of the raise offer action.",
	}, []string{"category"})
	ProviderPositions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "provider_positions_total",
		Help: "Provider positions by the result of the plausibility checks.",
	}, []string{"result"})
)
