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

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	_ "github.com/Agora-Mobility/atlas/docs/swagger"
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

	connections, err := containers.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	c := containers.New(cfg, version, connections)
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
