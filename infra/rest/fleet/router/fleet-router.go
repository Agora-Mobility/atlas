package fleet_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/fleet/controllers"
)

type Router struct {
	controller       *controllers.FleetController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
}

func NewRouter(controller *controllers.FleetController, authenticator *middleware.Authenticator, authorizer *middleware.Authorizer, clientAuthorizer *middleware.ClientAuthorizer) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer}
}

func (r *Router) Register(api *gin.RouterGroup) {
	can := r.authorizer.RequirePermission
	staff := api.Group("", r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth())

	r.routes(staff.Group("/fleet"), can(entities.PermPartnerProvidersRead), can(entities.PermPartnerProvidersManage))
	r.routes(staff.Group("/partners/:orgId/fleet"), can(entities.PermPartnersRead), can(entities.PermPartnersManage))

	staff.GET("/admin/vehicles", can(entities.PermProvidersRead), r.controller.ListAllVehicles)
	staff.GET("/admin/workshops", can(entities.PermPartnersRead), r.controller.ListAllWorkshops)
}

func (r *Router) routes(group *gin.RouterGroup, read, manage gin.HandlerFunc) {
	group.GET("/drivers", read, r.controller.ListDrivers)
	group.POST("/drivers", manage, r.controller.RegisterDriver)
	group.GET("/drivers/:id", read, r.controller.GetDriver)
	group.PUT("/drivers/:id/vehicle", manage, r.controller.AssignVehicle)
	group.DELETE("/drivers/:id/vehicle", manage, r.controller.ReleaseVehicle)

	group.GET("/vehicles", read, r.controller.ListVehicles)
	group.POST("/vehicles", manage, r.controller.CreateVehicle)
	group.GET("/vehicles/:id", read, r.controller.GetVehicle)
	group.PUT("/vehicles/:id", manage, r.controller.UpdateVehicle)
	group.DELETE("/vehicles/:id", manage, r.controller.DeactivateVehicle)

	group.GET("/workshops", read, r.controller.ListWorkshops)
	group.POST("/workshops", manage, r.controller.CreateWorkshop)
	group.GET("/workshops/:id", read, r.controller.GetWorkshop)
	group.PUT("/workshops/:id", manage, r.controller.UpdateWorkshop)
	group.DELETE("/workshops/:id", manage, r.controller.DeactivateWorkshop)
	group.POST("/workshops/:id/services", manage, r.controller.CreateWorkshopService)
	group.PUT("/workshops/:id/services/:serviceId", manage, r.controller.UpdateWorkshopService)
	group.DELETE("/workshops/:id/services/:serviceId", manage, r.controller.DeactivateWorkshopService)
}
