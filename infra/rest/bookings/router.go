package bookings

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

	signedIn.GET("/workshops", r.controller.SearchWorkshops)
	signedIn.GET("/workshops/:id", r.controller.GetWorkshop)
	signedIn.GET("/workshops/:id/reviews", r.controller.WorkshopReviews)
	signedIn.GET("/workshops/:id/availability", r.controller.WorkshopCalendar)
	signedIn.POST("/workshops/:id/favorite", r.controller.AddFavorite)
	signedIn.DELETE("/workshops/:id/favorite", r.controller.RemoveFavorite)

	signedIn.POST("/bookings", r.idempotency.Required(), r.controller.CreateBooking)
	signedIn.GET("/bookings", r.controller.ListBookings)
	signedIn.GET("/bookings/:id", r.controller.GetBooking)
	signedIn.GET("/bookings/:id/calendar", r.controller.BookingCalendarFile)
	signedIn.POST("/bookings/:id/cancel", r.controller.CancelBooking)
	signedIn.POST("/bookings/:id/approve", r.controller.ApproveBooking)
	signedIn.POST("/bookings/:id/extras/:extraId/answer", r.controller.AnswerExtra)
	signedIn.GET("/bookings/:id/messages", r.controller.BookingMessages)
	signedIn.POST("/bookings/:id/messages", r.controller.SendBookingMessage)
	signedIn.POST("/bookings/:id/call", r.controller.CallWorkshop)

	r.partner(signedIn.Group("/fleet"), can(entities.PermPartnerProvidersRead), can(entities.PermPartnerProvidersManage))
	r.partner(signedIn.Group("/partners/:orgId/fleet"), can(entities.PermPartnersRead), can(entities.PermPartnersManage))
}

func (r *Router) partner(group *gin.RouterGroup, read, manage gin.HandlerFunc) {
	group.PUT("/workshops/:id/schedule", manage, r.controller.SetSchedule)
	group.POST("/workshops/:id/closures", manage, r.controller.AddClosure)
	group.DELETE("/workshops/:id/closures/:date", manage, r.controller.RemoveClosure)
	group.GET("/workshops/:id/mechanics", read, r.controller.ListMechanics)
	group.POST("/workshops/:id/mechanics", manage, r.controller.AddMechanic)
	group.DELETE("/workshops/:id/mechanics/:mechanicId", manage, r.controller.RemoveMechanic)
	group.GET("/workshops/:id/bookings", read, r.controller.WorkshopBookings)
	group.GET("/bookings/:id", read, r.controller.PartnerBooking)
	group.POST("/bookings/:id/status", manage, r.controller.MoveBooking)
	group.PUT("/bookings/:id/assignment", manage, r.controller.AssignBooking)
	group.POST("/bookings/:id/extras", manage, r.controller.AddExtra)
	group.POST("/bookings/:id/cancel", manage, r.controller.PartnerCancelBooking)
	group.GET("/bookings/:id/messages", read, r.controller.PartnerMessages)
	group.POST("/bookings/:id/messages", manage, r.controller.PartnerSendMessage)
}
