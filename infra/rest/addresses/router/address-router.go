package addresses_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/addresses/controllers"
)

type Router struct {
	controller       *controllers.AddressController
	authenticator    *middleware.Authenticator
	clientAuthorizer *middleware.ClientAuthorizer
}

func NewRouter(controller *controllers.AddressController, authenticator *middleware.Authenticator, clientAuthorizer *middleware.ClientAuthorizer) *Router {
	return &Router{controller: controller, authenticator: authenticator, clientAuthorizer: clientAuthorizer}
}

func (r *Router) Register(api *gin.RouterGroup) {
	addresses := api.Group("/users/me/addresses", r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth())
	addresses.GET("", r.controller.List)
	addresses.POST("", r.controller.Create)
	addresses.PUT("/:id", r.controller.Update)
	addresses.DELETE("/:id", r.controller.Delete)
}
