package providers_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/providers/controllers"
)

type Router struct {
	controller       *controllers.ProviderController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
	registerLimiter  gin.HandlerFunc
}

func NewRouter(
	controller *controllers.ProviderController,
	authenticator *middleware.Authenticator,
	authorizer *middleware.Authorizer,
	clientAuthorizer *middleware.ClientAuthorizer,
	registerLimiter gin.HandlerFunc,
) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer, registerLimiter: registerLimiter}
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()
	api.POST("/providers/register", clients, r.registerLimiter, r.controller.Register)
	api.GET("/providers/services", clients, r.controller.OpenServices)

	me := api.Group("/providers/me", clients, r.authenticator.RequireAuth())
	me.GET("/profile", r.controller.MyProfile)
	me.PUT("/profile", r.controller.SetProfile)
	me.PUT("/vehicle", r.controller.SetVehicle)
	me.POST("/documents/upload-url", r.controller.UploadURL)
	me.POST("/documents", r.controller.RegisterDocument)
	me.GET("/documents/:type/url", r.controller.MyDocumentURL)
	me.POST("/submit", r.controller.Submit)

	can := r.authorizer.RequirePermission
	admin := api.Group("/admin/providers", clients, r.authenticator.RequireAuth())
	admin.GET("", can(entities.PermProvidersRead), r.controller.ListApplications)
	admin.GET("/:id", can(entities.PermProvidersRead), r.controller.GetApplication)
	admin.PUT("/:id", can(entities.PermProvidersApprove), r.controller.ReviewProvider)
	admin.POST("/:id/documents/:type/review", can(entities.PermProvidersApprove), r.controller.ReviewDocument)

	reasons := api.Group("/admin/rejection-reasons", clients, r.authenticator.RequireAuth())
	reasons.GET("", can(entities.PermProvidersRead), r.controller.ListRejectionReasons)
	reasons.POST("", can(entities.PermProvidersApprove), r.controller.CreateRejectionReason)
	reasons.PUT("/:id", can(entities.PermProvidersApprove), r.controller.UpdateRejectionReason)
}
