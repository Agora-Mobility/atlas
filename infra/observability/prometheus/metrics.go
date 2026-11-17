// Package prometheus declares the technical metrics exposed on /metrics.
package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "atlas"

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "HTTP requests by method, route template and status.",
	}, []string{"method", "route", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency by method and route template.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})

	EventsPublished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_published_total",
		Help:      "Events published to RabbitMQ by the outbox relay.",
	}, []string{"event_type"})

	EventsConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_consumed_total",
		Help:      "Events handled by consumers, by queue and result (success, retry, dlq, duplicate).",
	}, []string{"queue", "result"})

	OutboxPending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "outbox_pending_events",
		Help:      "Outbox events not yet published.",
	})

	OutboxOldestPendingAge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "outbox_oldest_pending_age_seconds",
		Help:      "Age of the oldest unpublished outbox event; 0 when none is pending.",
	})
)
