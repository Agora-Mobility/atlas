package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/apidocs"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	healthrouter "github.com/Agora-Mobility/atlas/infra/rest/health/router"
	"github.com/Agora-Mobility/atlas/shared"
)

var probePaths = []string{"/live", "/ready", "/health", "/metrics"}

type Server struct {
	container *containers.Container
	http      *http.Server
}

func NewServer(c *containers.Container, basePath string, registerRoutes func(api *gin.RouterGroup)) (*Server, error) {
	cfg := c.Config
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	binding.Setup()

	r := gin.New()
	r.HandleMethodNotAllowed = true
	if err := r.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	r.Use(
		middleware.Recovery(),
		middleware.CorrelationID(),
		middleware.SecurityHeaders(cfg.IsProduction()),
		middleware.CORS(cfg.HTTP.CORSAllowedOrigins),
		middleware.RequestLogger(probePaths...),
		middleware.Prometheus(probePaths...),
		middleware.RateLimiter(c.Redis, cfg.HTTP.RateLimitRequests, cfg.HTTP.RateLimitWindow, probePaths...),
	)
	r.NoRoute(func(ctx *gin.Context) { shared.NotFound(ctx, shared.CodeNotFound, "Route not found.") })
	r.NoMethod(func(ctx *gin.Context) {
		shared.RespondError(ctx, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.")
	})

	healthrouter.Register(r, c.Health.Controller)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	registerAPIDocs(r, cfg)

	registerRoutes(r.Group(basePath))
	c.Dispatch.Router.RegisterStream(r)

	return &Server{
		container: c,
		http: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.HTTP.Port),
			Handler:           r,
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			ReadHeaderTimeout: cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
		},
	}, nil
}

func registerAPIDocs(r gin.IRoutes, cfg *config.Config) {
	if cfg.IsDevelopment() {
		apidocs.Register(r)
	}
}

func (s *Server) Start(ctx context.Context) error {
	workersCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWorkers()

	var workers sync.WaitGroup
	relay := s.container.OutboxRelay()
	workers.Go(func() { relay.Run(workersCtx) })
	workers.Go(func() { s.container.SSE.RunDoor(workersCtx, s.container.Redis) })
	workers.Go(func() { s.container.Dispatch.Usecase.RunWatcher(workersCtx) })
	for _, consumer := range s.container.Consumers() {
		workers.Go(func() { consumer.Run(workersCtx) })
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", zap.String("addr", s.http.Addr), zap.String("version", s.container.Version))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serveErr:
		runErr = fmt.Errorf("http server: %w", err)
	}

	return errors.Join(runErr, s.shutdown(context.WithoutCancel(ctx), stopWorkers, &workers))
}

func (s *Server) shutdown(ctx context.Context, stopWorkers context.CancelFunc, workers *sync.WaitGroup) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.container.Config.HTTP.ShutdownTimeout)
	defer cancel()

	var errs []error
	s.container.SSE.Drain()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}

	stopWorkers()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		errs = append(errs, errors.New("timed out waiting for relay and consumers"))
	}

	logger.Info("server stopped")
	return errors.Join(errs...)
}
