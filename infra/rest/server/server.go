// Package server runs the HTTP server, the outbox relay and the RabbitMQ
// consumers, and shuts them down in order on SIGINT/SIGTERM.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
)

type Server struct {
	container *containers.Container
	http      *http.Server
}

func New(c *containers.Container, engine *gin.Engine) *Server {
	cfg := c.Config.HTTP
	return &Server{
		container: c,
		http: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Port),
			Handler:           engine,
			ReadTimeout:       cfg.ReadTimeout,
			ReadHeaderTimeout: cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
	}
}

// Start blocks until ctx is canceled (signal) or the HTTP server fails, then
// shuts down: HTTP first (drain requests), then relay and consumers, then
// connections.
func (s *Server) Start(ctx context.Context) error {
	workersCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWorkers()

	var workers sync.WaitGroup
	relay := s.container.OutboxRelay()
	workers.Go(func() { relay.Run(workersCtx) })
	for _, consumer := range s.container.Consumers() {
		workers.Go(func() { consumer.Run(workersCtx) })
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info(ctx, "http server listening", zap.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info(context.WithoutCancel(ctx), "shutdown signal received")
	case err := <-serveErr:
		runErr = fmt.Errorf("http server: %w", err)
	}

	return errors.Join(runErr, s.shutdown(context.WithoutCancel(ctx), stopWorkers, &workers))
}

func (s *Server) shutdown(ctx context.Context, stopWorkers context.CancelFunc, workers *sync.WaitGroup) error {
	timeout := s.container.Config.HTTP.ShutdownTimeout
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var errs []error
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

	s.container.Publisher.Close()
	if err := s.container.Queue.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close rabbitmq: %w", err))
	}
	if err := s.container.Redis.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close redis: %w", err))
	}
	s.container.Pool.Close()

	logger.Info(ctx, "shutdown complete")
	return errors.Join(errs...)
}
