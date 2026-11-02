package experience

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
)

type Router struct {
	controller       *Controller
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
	idempotency      *middleware.Idempotency
}

func NewRouter(controller *Controller, authenticator *middleware.Authenticator, authorizer *middleware.Authorizer, clientAuthorizer *middleware.ClientAuthorizer, idempotency *middleware.Idempotency) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer, idempotency: idempotency}
}

func (r *Router) Register(api *gin.RouterGroup) {
	can := r.authorizer.RequirePermission
	signedIn := api.Group("", r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth())
	manage := can(entities.PermServicesManage)

	signedIn.GET("/admin/catalog/:kind", manage, r.controller.AdminListItems)
	signedIn.POST("/admin/catalog/:kind", manage, r.controller.AdminCreateItem)
	signedIn.PUT("/admin/catalog/:kind/:id", manage, r.controller.AdminUpdateItem)
	signedIn.GET("/admin/settings", manage, r.controller.AdminSettings)
	signedIn.PUT("/admin/settings/:key", manage, r.controller.AdminSetSetting)

	signedIn.GET("/rating-options", r.controller.RatingOptions)
	signedIn.POST("/requests/:id/rating", r.controller.RateRequest)
	signedIn.GET("/providers/me/ratings", r.controller.MyRatings)
}
