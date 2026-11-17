// Package middleware holds the Gin middlewares shared by every route.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agora-Mobility/atlas/config/logger"
)

const (
	HeaderCorrelationID = "X-Correlation-ID"
	maxCorrelationIDLen = 128
)

// CorrelationID reuses the incoming X-Correlation-ID (when sane) or creates
// one, stores it in the request context and echoes it in the response.
func CorrelationID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderCorrelationID)
		if id == "" || len(id) > maxCorrelationIDLen {
			id = uuid.NewString()
		}
		// On the span too, so a trace can be found from the id a client reports.
		trace.SpanFromContext(c.Request.Context()).SetAttributes(attribute.String(logger.CorrelationIDField, id))
		ctx := logger.WithCorrelationID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(HeaderCorrelationID, id)
		c.Next()
	}
}
