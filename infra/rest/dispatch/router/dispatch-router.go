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
	root.GET("/admin/events", middleware.Streaming(), r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth(),
		r.authorizer.RequirePermission(entities.PermRequestsRead), r.stream.StaffStream)
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()
	signedIn := api.Group("", clients, r.authenticator.RequireAuth())
	retryable := r.idempotency.Optional()

	signedIn.GET("/nearby", r.controller.Nearby)
	signedIn.GET("/nearby/drivers", r.controller.NearbyDrivers)
	signedIn.POST("/fare-estimates", r.controller.CreateFareEstimates)
	signedIn.POST("/vouchers/preview", r.controller.PreviewVoucher)
	signedIn.PUT("/devices", r.controller.RegisterDevice)
	signedIn.DELETE("/devices/:token", r.controller.UnregisterDevice)

	providers := signedIn.Group("/providers/me")
	providers.POST("/online", retryable, r.controller.GoOnline)
	providers.POST("/offline", retryable, r.controller.GoOffline)
	providers.POST("/locations", r.controller.ReportLocations)
	providers.PUT("/coverage", retryable, r.controller.SetCoverage)
	providers.GET("/demand", r.controller.ProviderDemand)
	providers.GET("/requests", r.controller.ListProviderRequests)
	providers.GET("/offers", r.controller.ListProviderOffers)

	requests := signedIn.Group("/requests")
	signedIn.GET("/cancellation-reasons", r.controller.CancellationReasons)
	signedIn.GET("/quick-replies", r.controller.QuickReplies)
	api.GET("/public/trips/:token", r.controller.PublicTrip)
	signedIn.GET("/users/me/recent-destinations", r.controller.RecentDestinations)
	requests.POST("", r.idempotency.Required(), r.controller.CreateRequest)
	requests.GET("", r.controller.ListRequests)
	requests.GET("/active", r.controller.ActiveRequest)
	requests.GET("/:id", r.controller.GetRequest)
	requests.POST("/:id/cancel", retryable, r.controller.CancelRequest)
	requests.GET("/:id/cancellation", r.controller.CancellationPreview)
	requests.GET("/:id/receipt", r.controller.Receipt)
	requests.GET("/:id/messages", r.controller.ListMessages)
	requests.POST("/:id/messages", retryable, r.controller.SendMessage)
	requests.POST("/:id/share", r.controller.ShareTrip)
	requests.DELETE("/:id/share", r.controller.RevokeShares)
	requests.POST("/:id/push-token", r.controller.RegisterRidePushToken)
	requests.GET("/:id/eta", r.controller.ETA)
	requests.POST("/:id/call", r.controller.Call)
	requests.POST("/:id/sos", retryable, r.controller.RaiseSOS)
	requests.POST("/:id/repeat", r.controller.RepeatRequest)
	requests.PUT("/:id/team", retryable, r.controller.SetTeam)
	signedIn.GET("/removals/options", r.controller.RemovalOptions)
	signedIn.GET("/removals/availability", r.controller.RemovalCalendar)
	signedIn.PUT("/fleet/requests/:id/team", r.authorizer.RequirePermission(entities.PermPartnerProvidersManage), r.controller.SetTeamAsPartner)
	requests.POST("/:id/stops/:position/arrive", retryable, r.controller.ArriveAtStop)
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

	demand := admin.Group("/demand-pricing", can(entities.PermServicesManage))
	demand.GET("", r.controller.ListDemandPricing)
	demand.PUT("", r.controller.SaveDemandPricing)
	demand.GET("/areas", r.controller.DemandAreas)
	demand.GET("/history", r.controller.DemandHistory)

	admin.GET("/sos", can(entities.PermRequestsRead), r.controller.ListSOS)
	admin.POST("/sos/:id/resolve", can(entities.PermRequestsManage), r.controller.ResolveSOS)
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
	dispatchOps.GET("/heatmap/cells", r.controller.HeatCells)
	dispatchOps.GET("/flows", r.controller.Flows)
	dispatchOps.GET("/requests/:id", r.controller.OperatorGetRequest)
	dispatchOps.POST("/requests/:id/assign", r.controller.AssignManually)
	dispatchOps.POST("/requests/:id/retry", r.controller.RetrySearch)
	dispatchOps.POST("/requests/:id/cancel", r.controller.OperatorCancel)
}
