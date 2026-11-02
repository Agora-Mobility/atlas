package payments_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/payments/controllers"
)

type Router struct {
	controller       *controllers.PaymentController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
}

func NewRouter(controller *controllers.PaymentController, authenticator *middleware.Authenticator, authorizer *middleware.Authorizer, clientAuthorizer *middleware.ClientAuthorizer) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer}
}

func (r *Router) Register(api *gin.RouterGroup) {
	api.POST("/webhooks/appypay", r.controller.Webhook)

	can := r.authorizer.RequirePermission
	signedIn := api.Group("", r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth())
	signedIn.GET("/payment-methods", r.controller.ListMethods)
	signedIn.PUT("/requests/:id/payment-method", r.controller.ChangeMethod)
	signedIn.GET("/requests/:id/payments", r.controller.ListOfRequest)
	signedIn.POST("/requests/:id/payments", r.controller.Pay)
	signedIn.POST("/requests/:id/payments/cash", r.controller.ConfirmCash)
	signedIn.POST("/requests/:id/tips", r.controller.Tip)

	signedIn.GET("/wallet", r.controller.Wallet)
	signedIn.GET("/providers/me/ledger", r.controller.MyLedger)
	signedIn.GET("/admin/ledger/summary", can(entities.PermFinanceRead), r.controller.LedgerSummary)
	signedIn.GET("/admin/providers/:id/ledger", can(entities.PermFinanceRead), r.controller.ProviderLedger)
	signedIn.POST("/admin/providers/:id/settlements", can(entities.PermRefundsApprove), r.controller.Settle)
	signedIn.PUT("/admin/payments/:id/gateway-fee", can(entities.PermRefundsApprove), r.controller.SetGatewayFee)

	signedIn.GET("/admin/payment-methods", can(entities.PermServicesManage), r.controller.AdminMethods)
	signedIn.PUT("/admin/payment-methods", can(entities.PermServicesManage), r.controller.SetMethod)
	signedIn.GET("/admin/payments", can(entities.PermFinanceRead), r.controller.AdminList)
	signedIn.GET("/admin/payments/:id", can(entities.PermFinanceRead), r.controller.AdminGet)
	signedIn.POST("/admin/payments/:id/refund", can(entities.PermRefundsApprove), r.controller.Refund)
}
