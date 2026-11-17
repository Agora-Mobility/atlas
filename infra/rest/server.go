package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/rest/apidocs"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	healthrouter "github.com/Agora-Mobility/atlas/infra/rest/health/router"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/utils"
)

const topologyRetryCeiling = 30 * time.Second

var probePaths = []string{"/live", "/ready", "/health", "/metrics"}

// untracedPaths are kept out of traces: probes are noise, and an SSE stream
// lives for minutes, so its span would swallow every poll it makes.
var untracedPaths = append([]string{"/events"}, probePaths...)

type Server struct {
	container *containers.Container
	http      *http.Server
	// metrics serves /metrics on an internal port when METRICS_PORT is set.
	metrics *http.Server
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
		otelgin.Middleware(cfg.App.Name, otelgin.WithFilter(traced(untracedPaths))),
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
	var metricsServer *http.Server
	if cfg.Telemetry.MetricsPort > 0 {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler())
		metricsServer = &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Telemetry.MetricsPort),
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
	} else {
		r.GET("/metrics", gin.WrapH(metricsHandler()))
	}
	registerAPIDocs(r, cfg)

	registerRoutes(r.Group(basePath))
	c.Dispatch.Router.RegisterStream(r)

	return &Server{
		container: c,
		metrics:   metricsServer,
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

// metricsHandler speaks OpenMetrics when the scraper asks for it, which is
// the format that carries exemplars (the trace ids attached to latencies).
func metricsHandler() http.Handler {
	return promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func traced(skip []string) otelgin.Filter {
	set := make(map[string]struct{}, len(skip))
	for _, p := range skip {
		set[p] = struct{}{}
	}
	return func(r *http.Request) bool {
		_, untraced := set[r.URL.Path]
		return !untraced
	}
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
	consumers := s.container.Consumers()
	workers.Go(func() {
		if ensureTopology(workersCtx, consumers) {
			relay.Run(workersCtx)
		}
	})
	workers.Go(func() { s.container.SSE.RunDoor(workersCtx, s.container.Redis) })
	workers.Go(func() { s.container.Dispatch.Usecase.RunWatcher(workersCtx) })
	workers.Go(func() { s.container.Dispatch.Usecase.RunHeatmapAggregator(workersCtx) })
	workers.Go(func() { s.container.Payments.Usecase.RunReconciler(workersCtx) })
	workers.Go(func() { s.container.Verification.Usecase.RunHousekeeping(workersCtx) })
	for _, consumer := range consumers {
		workers.Go(func() { consumer.Run(workersCtx) })
	}

	if s.metrics != nil {
		go func() {
			logger.Info("metrics server starting", zap.String("addr", s.metrics.Addr))
			if err := s.metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server failed", err)
			}
		}()
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

func ensureTopology(ctx context.Context, consumers []*queue.Consumer) bool {
	for _, consumer := range consumers {
		for attempt := 0; ; attempt++ {
			err := consumer.EnsureTopology()
			if err == nil {
				break
			}
			logger.Warn("queue topology not ready, the relay waits", zap.Error(err))
			if !utils.Sleep(ctx, utils.Backoff(attempt, time.Second, topologyRetryCeiling)) {
				return false
			}
		}
	}
	return true
}

func (s *Server) shutdown(ctx context.Context, stopWorkers context.CancelFunc, workers *sync.WaitGroup) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.container.Config.HTTP.ShutdownTimeout)
	defer cancel()

	var errs []error
	s.container.SSE.Drain()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	if s.metrics != nil {
		_ = s.metrics.Shutdown(shutdownCtx)
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
