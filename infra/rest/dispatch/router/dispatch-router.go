package dispatch_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/dispatch/controllers"
)

type Router struct {
	controller       *controllers.DispatchController
	stream           *controllers.StreamController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
	idempotency      *middleware.Idempotency
}

func NewRouter(
	controller *controllers.DispatchController,
	stream *controllers.StreamController,
	authenticator *middleware.Authenticator,
	authorizer *middleware.Authorizer,
	clientAuthorizer *middleware.ClientAuthorizer,
	idempotency *middleware.Idempotency,
) *Router {
	return &Router{
		controller: controller, stream: stream, authenticator: authenticator, authorizer: authorizer,
		clientAuthorizer: clientAuthorizer, idempotency: idempotency,
	}
}

func (r *Router) RegisterStream(root gin.IRoutes) {
	root.GET("/events", middleware.Streaming(), r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth(), r.stream.Stream)
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()
	signedIn := api.Group("", clients, r.authenticator.RequireAuth())
	retryable := r.idempotency.Optional()

	signedIn.GET("/nearby", r.controller.Nearby)
	signedIn.POST("/fare-estimates", r.controller.CreateFareEstimates)
	signedIn.POST("/vouchers/preview", r.controller.PreviewVoucher)
	signedIn.PUT("/devices", r.controller.RegisterDevice)
	signedIn.DELETE("/devices/:token", r.controller.UnregisterDevice)

	providers := signedIn.Group("/providers/me")
	providers.POST("/online", retryable, r.controller.GoOnline)
	providers.POST("/offline", retryable, r.controller.GoOffline)
	providers.POST("/locations", r.controller.ReportLocations)
	providers.PUT("/coverage", retryable, r.controller.SetCoverage)

	requests := signedIn.Group("/requests")
	signedIn.GET("/cancellation-reasons", r.controller.CancellationReasons)
	requests.POST("", r.idempotency.Required(), r.controller.CreateRequest)
	requests.GET("", r.controller.ListRequests)
	requests.GET("/active", r.controller.ActiveRequest)
	requests.GET("/:id", r.controller.GetRequest)
	requests.POST("/:id/cancel", retryable, r.controller.CancelRequest)
	requests.GET("/:id/cancellation", r.controller.CancellationPreview)
	requests.POST("/:id/boost", retryable, r.controller.BoostRequest)
	requests.POST("/:id/route", retryable, r.controller.ChangeRoute)
	requests.POST("/:id/select-quote", retryable, r.controller.SelectQuote)
	requests.POST("/:id/en-route", retryable, r.controller.StartRoute)
	requests.POST("/:id/arrive", retryable, r.controller.Arrive)
	requests.POST("/:id/start", retryable, r.controller.StartJob)
	requests.POST("/:id/complete", retryable, r.controller.CompleteJob)

	offers := signedIn.Group("/offers")
	offers.GET("/:id", r.controller.GetOffer)
	offers.POST("/:id/delivered", retryable, r.controller.OfferDelivered)
	offers.POST("/:id/accept", retryable, r.controller.AcceptOffer)
	offers.POST("/:id/reject", retryable, r.controller.RejectOffer)
	offers.POST("/:id/quote", retryable, r.controller.QuoteOffer)

	can := r.authorizer.RequirePermission
	admin := api.Group("/admin", clients, r.authenticator.RequireAuth())

	zones := admin.Group("/zones", can(entities.PermServicesManage))
	zones.GET("", r.controller.ListZones)
	zones.POST("", r.controller.CreateZone)
	zones.PUT("/:id", r.controller.UpdateZone)

	policies := admin.Group("/dispatch-policies", can(entities.PermServicesManage))
	policies.GET("", r.controller.ListPolicies)
	policies.POST("", r.controller.CreatePolicy)
	policies.PUT("/:id", r.controller.UpdatePolicy)

	tariffs := admin.Group("/tariffs", can(entities.PermServicesManage))
	tariffs.GET("", r.controller.ListTariffs)
	tariffs.POST("", r.controller.PublishTariff)

	admin.GET("/cancellation-rules", can(entities.PermServicesManage), r.controller.ListCancellationRules)
	admin.PUT("/cancellation-rules/:serviceId", can(entities.PermServicesManage), r.controller.SetCancellationRule)
	admin.GET("/reports/cancellations", can(entities.PermRequestsRead), r.controller.CancellationReport)
	admin.GET("/requests", can(entities.PermRequestsRead), r.controller.ListAdminRequests)
	admin.GET("/requests/summary", can(entities.PermRequestsRead), r.controller.SummarizeAdminRequests)
	admin.GET("/requests/:id", can(entities.PermRequestsRead), r.controller.GetAdminRequest)

	partnerRequests := api.Group("/fleet/requests", clients, r.authenticator.RequireAuth(), can(entities.PermPartnerRequestsRead))
	partnerRequests.GET("", r.controller.ListPartnerRequests)
	partnerRequests.GET("/summary", r.controller.SummarizePartnerRequests)
	partnerRequests.GET("/:id", r.controller.GetPartnerRequest)

	dispatchOps := admin.Group("/dispatch", can(entities.PermRequestsManage))
	dispatchOps.GET("/queue", r.controller.OperatorQueue)
	dispatchOps.GET("/heatmap", r.controller.HeatMap)
	dispatchOps.GET("/requests/:id", r.controller.OperatorGetRequest)
	dispatchOps.POST("/requests/:id/assign", r.controller.AssignManually)
	dispatchOps.POST("/requests/:id/retry", r.controller.RetrySearch)
	dispatchOps.POST("/requests/:id/cancel", r.controller.OperatorCancel)
}
