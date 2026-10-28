// Command atlas is the core API of the AGORA platform.
//
// Usage:
//
//	atlas [serve]               start the HTTP API, outbox relay and consumers (default)
//	atlas migrate up|down       apply all migrations or roll back the last one
//	atlas seed                  insert development seed data
//	atlas token <role> [user]   print a development access token (not in production)
//	atlas healthcheck           probe /live on the local server (used by Docker)
//
//	@title						Atlas API
//	@version					1.0
//	@description				Core API of the AGORA platform.
//	@BasePath					/
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				"Bearer <token>"
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/postgres/seeders"
	"github.com/Agora-Mobility/atlas/infra/database/redis"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	"github.com/Agora-Mobility/atlas/infra/rest/router"
	"github.com/Agora-Mobility/atlas/infra/rest/server"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// The healthcheck runs inside the distroless image, which has no shell or
	// curl. It must not depend on the full configuration being valid.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}
	logger.MustInit(logger.Options{
		ServiceName: cfg.App.Name,
		Environment: cfg.App.Env.String(),
		Output:      cfg.Log.Output,
		Level:       cfg.Log.Level,
	})
	ctx := context.Background()

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		err = runServe(ctx, cfg)
	case "migrate":
		direction := "up"
		if len(os.Args) > 2 {
			direction = os.Args[2]
		}
		if err = postgres.Migrate(cfg.Database.URL, direction); err == nil {
			logger.Info("migrations applied", zap.String("direction", direction))
		}
	case "seed":
		if err = runSeed(ctx, cfg); err == nil {
			logger.Info("seed completed")
		}
	case "token":
		err = runToken(cfg, os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}

	if err != nil {
		logger.Error(cmd+" failed", err)
		_ = logger.Sync()
		os.Exit(1)
	}
	_ = logger.Sync()
}

func runServe(ctx context.Context, cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("atlas starting", zap.String("env", cfg.App.Env.String()), zap.String("version", version))

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	rdb, err := redis.NewClient(ctx, cfg.Redis)
	if err != nil {
		pool.Close()
		return err
	}
	mgr, err := queue.NewManager(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return err
	}

	c := containers.New(cfg, version, pool, rdb, mgr)
	engine, err := router.New(c)
	if err != nil {
		_ = mgr.Close()
		_ = rdb.Close()
		pool.Close()
		return err
	}
	return server.New(c, engine).Start(ctx)
}

func runSeed(ctx context.Context, cfg *config.Config) error {
	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	return seeders.Run(ctx, pool)
}

// runToken prints an access token for local testing of protected routes.
func runToken(cfg *config.Config, args []string) error {
	if cfg.IsProduction() {
		return errors.New("token command is disabled in production")
	}
	if len(args) < 1 {
		return errors.New("usage: atlas token <customer|provider|operator|admin> [user-uuid]")
	}
	role := enums.Role(args[0])
	if !role.IsValid() {
		return fmt.Errorf("invalid role %q", args[0])
	}
	userID := uuid.New()
	if len(args) > 1 {
		var err error
		if userID, err = uuid.Parse(args[1]); err != nil {
			return fmt.Errorf("invalid user id: %w", err)
		}
	}

	token, _, err := authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.AccessTTL).Generate(userID, role)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

func healthcheck() int {
	port := 8080
	if v := os.Getenv("HTTP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			return 1
		}
		port = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Always loopback with a numeric port: the target cannot be redirected.
	url := fmt.Sprintf("http://127.0.0.1:%d/live", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) // #nosec G704 -- fixed loopback host, numeric port
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- fixed loopback host, numeric port
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
