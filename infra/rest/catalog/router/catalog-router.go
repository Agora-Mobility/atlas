package catalog_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/catalog/controllers"
)

type Router struct {
	controller       *controllers.CatalogController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
}

func NewRouter(
	controller *controllers.CatalogController,
	authenticator *middleware.Authenticator,
	authorizer *middleware.Authorizer,
	clientAuthorizer *middleware.ClientAuthorizer,
) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer}
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()

	// The app shows the catalog before sign-in, so only the client is checked.
	api.GET("/catalog", clients, r.controller.Catalog)

	can := r.authorizer.RequirePermission
	admin := api.Group("/admin", clients, r.authenticator.RequireAuth())

	categories := admin.Group("/categories", can(entities.PermCategoriesManage))
	categories.GET("", r.controller.ListCategories)
	categories.POST("", r.controller.CreateCategory)
	categories.GET("/:id", r.controller.GetCategory)
	categories.PATCH("/:id", r.controller.UpdateCategory)

	services := admin.Group("/services", can(entities.PermServicesManage))
	services.GET("", r.controller.ListServices)
	services.POST("", r.controller.CreateService)
	services.GET("/:id", r.controller.GetService)
	services.PATCH("/:id", r.controller.UpdateService)
}
