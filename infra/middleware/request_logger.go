package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/Agora-Mobility/atlas/config/logger"
)

// RequestLogger logs one structured line per request. Probe endpoints are
// logged at debug level to keep production logs readable.
func RequestLogger(quietPaths ...string) gin.HandlerFunc {
	quiet := make(map[string]struct{}, len(quietPaths))
	for _, p := range quietPaths {
		quiet[p] = struct{}{}
	}

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := zapcore.InfoLevel
		switch {
		case status >= 500:
			level = zapcore.ErrorLevel
		case status >= 400:
			level = zapcore.WarnLevel
		}
		if _, ok := quiet[c.Request.URL.Path]; ok && status < 400 {
			level = zapcore.DebugLevel
		}

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("route", c.FullPath()),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(start)),
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int("bytes", c.Writer.Size()),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}
		logger.FromContext(c.Request.Context()).Log(level, "http request", fields...)
	}
}
