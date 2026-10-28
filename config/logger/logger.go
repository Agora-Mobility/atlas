package logger

import (
	"context"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logOutputEnv = "LOG_OUTPUT"
	logLevelEnv  = "LOG_LEVEL"

	defaultServiceName = "atlas"
	defaultEnvironment = "development"
	CorrelationIDField = "correlation_id"
)

type Options struct {
	ServiceName string
	Environment string
	Output      string
	Level       string
}

type contextKey string

const (
	loggerContextKey        contextKey = "logger"
	correlationIDContextKey contextKey = CorrelationIDField
)

var (
	once      sync.Once
	base      *zap.Logger
	initError error
)

func MustInit(opts Options) {
	if err := Init(opts); err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

func Init(opts Options) error {
	once.Do(func() {
		logConfig := zap.Config{
			OutputPaths: []string{normalizeOutput(opts.Output)},
			Level:       zap.NewAtomicLevelAt(parseLevel(opts.Level)),
			Encoding:    "json",
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:        "timestamp",
				LevelKey:       "level",
				MessageKey:     "message",
				CallerKey:      "caller",
				StacktraceKey:  "stacktrace",
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeLevel:    zapcore.LowercaseLevelEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
				LineEnding:     zapcore.DefaultLineEnding,
				EncodeDuration: zapcore.MillisDurationEncoder,
			},
			DisableStacktrace: true,
		}

		logger, err := logConfig.Build()
		if err != nil {
			initError = err
			return
		}

		base = logger.With(
			zap.String("service_name", normalizeServiceName(opts.ServiceName)),
			zap.String("environment", normalizeEnvironment(opts.Environment)),
		)
	})

	return initError
}

func L() *zap.Logger {
	if base == nil {
		MustInit(Options{
			ServiceName: os.Getenv("APP_NAME"),
			Environment: os.Getenv("APP_ENV"),
			Output:      os.Getenv(logOutputEnv),
			Level:       os.Getenv(logLevelEnv),
		})
	}

	return base
}

func Sync() error {
	if base == nil {
		return nil
	}
	return base.Sync()
}

func IntoContext(ctx context.Context, log *zap.Logger) context.Context {
	if ctx == nil || log == nil {
		return ctx
	}

	return context.WithValue(ctx, loggerContextKey, log)
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDContextKey, id)
}

func CorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(correlationIDContextKey).(string)
	return id
}

func FromContext(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return L()
	}

	if log, ok := ctx.Value(loggerContextKey).(*zap.Logger); ok && log != nil {
		return log
	}

	fields := WithContext(ctx)
	if len(fields) == 0 {
		return L()
	}

	return L().With(fields...)
}

func With(fields ...zap.Field) *zap.Logger {
	return L().With(fields...)
}

func Info(message string, tags ...zap.Field) {
	wrapped().Info(message, tags...)
}

func Warn(message string, tags ...zap.Field) {
	wrapped().Warn(message, tags...)
}

func Error(message string, err error, tags ...zap.Field) {
	if err != nil {
		tags = append(tags, zap.NamedError("error", err))
	}
	wrapped().Error(message, tags...)
}

func WithContext(ctx context.Context) []zap.Field {
	fields := []zap.Field{}
	if ctx == nil {
		return fields
	}

	if id := CorrelationID(ctx); id != "" {
		fields = append(fields, zap.String(CorrelationIDField, id))
	}

	return fields
}

func wrapped() *zap.Logger {
	return L().WithOptions(zap.AddCallerSkip(1))
}

func normalizeOutput(output string) string {
	value := strings.ToLower(strings.TrimSpace(output))
	if value == "" {
		return "stdout"
	}
	return value
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func normalizeServiceName(name string) string {
	value := strings.TrimSpace(name)
	if value == "" {
		return defaultServiceName
	}
	return value
}

func normalizeEnvironment(env string) string {
	value := strings.TrimSpace(env)
	if value == "" {
		return defaultEnvironment
	}
	return value
}
