// Command atlas is the core API of the AGORA platform.
//
// Usage:
//
//	atlas [serve]          start the HTTP API, outbox relay and consumers (default)
//	atlas migrate up|down  apply or roll back database migrations
//	atlas seed             insert development seed data
package main

import (
	"context"
	"os"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
)

func main() {
	config.LoadDotEnv()
	logger.MustInit(os.Getenv("APP_ENV"), os.Getenv("LOG_LEVEL"))
	defer logger.Sync()

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal(ctx, "configuration error", zap.Error(err))
	}

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		logger.Info(ctx, "atlas starting", zap.String("env", cfg.App.Env.String()))
	default:
		logger.Fatal(ctx, "unknown command", zap.String("command", cmd))
	}
}
