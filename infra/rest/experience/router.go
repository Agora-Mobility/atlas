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

	signedIn.GET("/vouchers", r.controller.PublicVouchers)
	signedIn.GET("/admin/vouchers", manage, r.controller.AdminVouchers)
	signedIn.POST("/admin/vouchers", manage, r.controller.AdminCreateVoucher)
	signedIn.PUT("/admin/vouchers/:id", manage, r.controller.AdminUpdateVoucher)

	signedIn.POST("/requests/:id/tickets", r.controller.OpenRequestTicket)
	signedIn.GET("/support/tickets", r.controller.MyTickets)
	signedIn.POST("/support/tickets", r.controller.OpenTicket)
	signedIn.GET("/support/tickets/:id", r.controller.MyTicket)
	signedIn.POST("/support/tickets/:id/messages", r.controller.ReplyTicket)
	support := can(entities.PermRequestsManage)
	signedIn.GET("/admin/support/tickets", support, r.controller.AdminTickets)
	signedIn.GET("/admin/support/tickets/:id", support, r.controller.AdminTicket)
	signedIn.POST("/admin/support/tickets/:id/messages", support, r.controller.AdminReplyTicket)
	signedIn.POST("/admin/support/tickets/:id/close", support, r.controller.AdminCloseTicket)

	signedIn.GET("/users/me/vehicles", r.controller.MyVehicles)
	signedIn.POST("/users/me/vehicles", r.controller.AddVehicle)
	signedIn.PUT("/users/me/vehicles/:id", r.controller.UpdateVehicle)
	signedIn.DELETE("/users/me/vehicles/:id", r.controller.RemoveVehicle)
	signedIn.POST("/media/photos", r.controller.NewPhotoUpload)
	signedIn.GET("/requests/:id/photos", r.controller.RequestPhotos)
	signedIn.POST("/requests/:id/photos", r.controller.AttachRequestPhotos)

	signedIn.GET("/rating-options", r.controller.RatingOptions)
	signedIn.POST("/requests/:id/rating", r.controller.RateRequest)
	signedIn.GET("/providers/me/ratings", r.controller.MyRatings)
}
