package rest

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/containers"
)

func SetupRoutes(api *gin.RouterGroup, c *containers.Container) {
	c.Auth.Router.Register(api)
	c.Catalog.Router.Register(api)
	c.Dispatch.Router.Register(api)
	c.Providers.Router.Register(api)
	c.Verification.Router.Register(api)
	c.Addresses.Router.Register(api)
	c.Fleet.Router.Register(api)
	c.Payments.Router.Register(api)
	c.Experience.Router.Register(api)
	c.Bookings.Router.Register(api)
}
