//	@title			Atlas API
//	@version		1.0
//	@description	Core API of the AGORA platform.
//	@BasePath		/

//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Access token (Bearer) returned by /api/v1/auth/login.

// @securityDefinitions.apikey	ClientId
// @in							header
// @name						X-Client-Id
// @description				Client application: agora-customer-app, agora-provider-app, agora-partner-panel or agora-backoffice. Required on every /api/v1 endpoint.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/observability/prometheus"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
	"github.com/Agora-Mobility/atlas/infra/rest"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("atlas: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger.MustInit(logger.Options{
		ServiceName: cfg.App.Name,
		Environment: cfg.App.Env.String(),
		Output:      cfg.Log.Output,
		Level:       cfg.Log.Level,
	})
	defer func() {
		_ = logger.Sync()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, tracing.Options{
		ServiceName: cfg.App.Name,
		Version:     version,
		Environment: cfg.App.Env.String(),
		Endpoint:    cfg.Telemetry.OTLPEndpoint,
		SampleRatio: cfg.Telemetry.TraceSampleRatio,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Flush the spans still buffered; a collector that is down must not
		// hold the shutdown.
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(flushCtx); err != nil {
			logger.Error("failed to flush traces", err)
		}
	}()
	if cfg.Telemetry.TracingEnabled() {
		logger.Info("tracing enabled")
	}

	if cfg.Database.MigrateOnStart {
		if err := postgres.Migrate(cfg.Database.URL, "up"); err != nil {
			return err
		}
		logger.Info("migrations applied")
	}

	connections, err := containers.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	if err := prometheus.RegisterPool(connections.Pool); err != nil {
		return err
	}
	c, err := containers.New(cfg, version, connections)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil {
			logger.Error("failed to close connections", err)
		}
	}()

	srv, err := rest.NewServer(c, "/api/v1", func(api *gin.RouterGroup) {
		rest.SetupRoutes(api, c)
	})
	if err != nil {
		return err
	}

	return srv.Start(ctx)
}
