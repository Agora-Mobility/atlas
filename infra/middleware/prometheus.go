package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/observability/prometheus"
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
		prometheus.HTTPDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	}
}
