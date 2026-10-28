// Package router builds the Gin engine: global middlewares, probes, metrics,
// Swagger and every module's routes.
package router

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/Agora-Mobility/atlas/docs/swagger" // generated OpenAPI spec
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	healthrouter "github.com/Agora-Mobility/atlas/infra/rest/health/router"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
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
		middleware.ErrorHandler(),
	)

	r.NoRoute(func(ctx *gin.Context) { _ = ctx.Error(customerrors.NotFound("route")) })
	r.NoMethod(func(ctx *gin.Context) {
		_ = ctx.Error(customerrors.New(customerrors.CodeBadRequest, "method not allowed", nil))
	})

	healthrouter.Register(r, c.Health.Controller)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if !cfg.IsProduction() {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	return r, nil
}
