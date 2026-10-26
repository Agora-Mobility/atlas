// Package logger wraps zap with a process-wide logger and context helpers so
// every log line carries the request correlation_id.
package logger

import (
	"context"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey int

const (
	loggerKey ctxKey = iota
	correlationIDKey
)

// CorrelationIDField is the log field and header-independent name of the
// correlation identifier.
const CorrelationIDField = "correlation_id"

// skipWrapper makes the package-level helpers report their caller's line.
var skipWrapper = zap.AddCallerSkip(1)

var (
	base = zap.NewNop()
	mu   sync.RWMutex
)

// MustInit builds the global logger. Production uses JSON output; any other
// environment uses a colored console encoder. It panics on invalid setup.
func MustInit(env, level string) {
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		lvl = zapcore.InfoLevel
	}

	var cfg zap.Config
	if env == "production" || env == "staging" {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.TimeKey = "timestamp"
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)

	l, err := cfg.Build()
	if err != nil {
		panic("logger: " + err.Error())
	}

	mu.Lock()
	base = l
	mu.Unlock()
}

// L returns the global logger without context fields.
func L() *zap.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return base
}

// Sync flushes buffered log entries. Call it before the process exits.
func Sync() {
	_ = L().Sync()
}

// WithContext stores a logger in ctx so downstream code logs with its fields.
func WithContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// WithCorrelationID stores the correlation id in ctx.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// CorrelationID returns the correlation id stored in ctx, or "".
func CorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(correlationIDKey).(string)
	return id
}

// FromContext returns the logger stored in ctx (or the global one) enriched
// with the correlation id when present.
func FromContext(ctx context.Context) *zap.Logger {
	l := L()
	if ctx == nil {
		return l
	}
	if cl, ok := ctx.Value(loggerKey).(*zap.Logger); ok && cl != nil {
		l = cl
	}
	if id := CorrelationID(ctx); id != "" {
		l = l.With(zap.String(CorrelationIDField, id))
	}
	return l
}

func Debug(ctx context.Context, msg string, fields ...zap.Field) {
	FromContext(ctx).WithOptions(skipWrapper).Debug(msg, fields...)
}

func Info(ctx context.Context, msg string, fields ...zap.Field) {
	FromContext(ctx).WithOptions(skipWrapper).Info(msg, fields...)
}

func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	FromContext(ctx).WithOptions(skipWrapper).Warn(msg, fields...)
}

func Error(ctx context.Context, msg string, fields ...zap.Field) {
	FromContext(ctx).WithOptions(skipWrapper).Error(msg, fields...)
}

// Fatal logs and exits the process. Use only during startup.
func Fatal(ctx context.Context, msg string, fields ...zap.Field) {
	FromContext(ctx).WithOptions(skipWrapper).Fatal(msg, fields...)
}
