// Command atlas is the core API of the AGORA platform.
//
// Usage:
//
//	atlas [serve]          start the HTTP API, outbox relay and consumers (default)
//	atlas migrate up|down  apply or roll back database migrations
//	atlas seed             insert development seed data
//	atlas healthcheck      probe /live on the local server (used by Docker)
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// The healthcheck runs inside the distroless image, which has no shell or
	// curl. It must not depend on the full configuration being valid.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

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
		logger.Info(ctx, "atlas starting", zap.String("env", cfg.App.Env.String()), zap.String("version", version))
	default:
		logger.Fatal(ctx, "unknown command", zap.String("command", cmd))
	}
}

func healthcheck() int {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%s/live", port), nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
