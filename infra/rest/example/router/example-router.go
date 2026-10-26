// Package router registers the routes of the example module.
package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/example/controllers"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

// Register mounts the example routes. Reads are public; creation requires an
// operator or admin token, to show how auth and role middlewares compose.
func Register(api *gin.RouterGroup, ctl *controllers.ExampleController, jwt *authjwt.Service) {
	g := api.Group("/examples")
	g.GET("", ctl.List)
	g.GET("/:id", ctl.GetByID)
	g.POST("",
		authjwt.RequireAuth(jwt),
		authjwt.RequireRole(enums.RoleOperator, enums.RoleAdmin),
		ctl.Create,
	)
}
