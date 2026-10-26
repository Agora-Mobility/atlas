// Package router registers the probe routes.
package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/health/controllers"
)

// Register mounts /live, /ready and /health (alias of /ready).
func Register(r gin.IRoutes, ctl *controllers.HealthController) {
	r.GET("/live", ctl.Live)
	r.GET("/ready", ctl.Ready)
	r.GET("/health", ctl.Ready)
}
