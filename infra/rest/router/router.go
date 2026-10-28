// Package router builds the Gin engine: global middlewares, probes, metrics,
// Swagger and every module's routes.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/Agora-Mobility/atlas/docs/swagger" // generated OpenAPI spec
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	healthrouter "github.com/Agora-Mobility/atlas/infra/rest/health/router"
	"github.com/Agora-Mobility/atlas/shared"
)

var probePaths = []string{"/live", "/ready", "/health", "/metrics"}

func New(c *containers.Container) (*gin.Engine, error) {
	cfg := c.Config

	binding.Setup()

	r := gin.New()
	r.HandleMethodNotAllowed = true
	if err := r.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		return nil, err
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

	if !cfg.IsProduction() {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	return r, nil
}
