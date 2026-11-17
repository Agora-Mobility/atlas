package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	promclient "github.com/prometheus/client_golang/prometheus"

	"github.com/Agora-Mobility/atlas/infra/observability/prometheus"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
)

// Prometheus records request count and latency per route template, so
// /examples/:id is one series and not one per id.
func Prometheus(skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := skip[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		prometheus.HTTPRequests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		observe(prometheus.HTTPDuration.WithLabelValues(method, route), time.Since(start).Seconds(),
			tracing.SampledTraceID(c.Request.Context()))
	}
}

// observe attaches the trace id as an exemplar, so a latency spike in Grafana
// links straight to one of the requests that caused it.
func observe(o promclient.Observer, value float64, traceID string) {
	if traceID != "" {
		if eo, ok := o.(promclient.ExemplarObserver); ok {
			eo.ObserveWithExemplar(value, promclient.Labels{"trace_id": traceID})
			return
		}
	}
	o.Observe(value)
}
