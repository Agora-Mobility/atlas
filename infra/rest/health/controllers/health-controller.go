// Package controllers exposes the liveness and readiness probes.
package controllers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const checkTimeout = 2 * time.Second

// Check probes one dependency and returns nil when it is healthy.
type Check func(ctx context.Context) error

type HealthController struct {
	version string
	checks  map[string]Check
}

func NewHealthController(version string, checks map[string]Check) *HealthController {
	return &HealthController{version: version, checks: checks}
}

type LiveResponse struct {
	Status  string `json:"status" example:"ok"`
	Version string `json:"version" example:"v1.0.0"`
}

type ReadyResponse struct {
	Status string            `json:"status" example:"ok"`
	Checks map[string]string `json:"checks"`
}

// Live godoc
//
//	@Summary	Liveness probe
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	LiveResponse
//	@Router		/live [get]
func (h *HealthController) Live(c *gin.Context) {
	c.JSON(http.StatusOK, LiveResponse{Status: "ok", Version: h.version})
}

// Ready godoc
//
//	@Summary		Readiness probe
//	@Description	Checks PostgreSQL, Redis and RabbitMQ.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	ReadyResponse
//	@Failure		503	{object}	ReadyResponse
//	@Router			/ready [get]
func (h *HealthController) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), checkTimeout)
	defer cancel()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]string, len(h.checks))
		healthy = true
	)
	for name, check := range h.checks {
		wg.Go(func() {
			status := "ok"
			if err := check(ctx); err != nil {
				status = "error: " + err.Error()
			}
			mu.Lock()
			defer mu.Unlock()
			results[name] = status
			if status != "ok" {
				healthy = false
			}
		})
	}
	wg.Wait()

	if !healthy {
		c.JSON(http.StatusServiceUnavailable, ReadyResponse{Status: "unavailable", Checks: results})
		return
	}
	c.JSON(http.StatusOK, ReadyResponse{Status: "ok", Checks: results})
}
