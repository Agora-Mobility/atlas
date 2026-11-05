package verification

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
)

type Limiters struct {
	Uploads gin.HandlerFunc
	Checks  gin.HandlerFunc
}

type Router struct {
	controller       *Controller
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
	idempotency      *middleware.Idempotency
	limiters         Limiters
}

func NewRouter(controller *Controller, authenticator *middleware.Authenticator, authorizer *middleware.Authorizer, clientAuthorizer *middleware.ClientAuthorizer, idempotency *middleware.Idempotency, limiters Limiters) *Router {
	return &Router{controller: controller, authenticator: authenticator, authorizer: authorizer, clientAuthorizer: clientAuthorizer, idempotency: idempotency, limiters: limiters}
}

func (r *Router) Register(api *gin.RouterGroup) {
	signedIn := []gin.HandlerFunc{r.clientAuthorizer.RequireClient(), r.authenticator.RequireAuth()}
	repeatable := r.idempotency.Optional()

	me := api.Group("/providers/me/verification", signedIn...)
	me.GET("", r.controller.Overview)
	me.POST("/uploads", r.limiters.Uploads, repeatable, r.controller.StartUpload)
	me.POST("/uploads/:id/confirm", r.limiters.Uploads, r.controller.ConfirmUpload)
	me.GET("/uploads/:id", r.controller.Upload)
	me.GET("/uploads/:id/url", r.controller.UploadURL)
	me.POST("/identity-card/extraction", r.limiters.Checks, repeatable, r.controller.StartExtraction)
	me.GET("/identity-card/extraction", r.controller.Extraction)
	me.PUT("/identity-card/data", repeatable, r.controller.ConfirmIdentity)
	me.POST("/selfie/session", r.limiters.Checks, r.controller.StartLiveness)
	me.POST("/selfie", r.limiters.Checks, repeatable, r.controller.SubmitSelfie)
	me.PUT("/driving-license", repeatable, r.controller.SetDrivingLicense)
	me.PUT("/criminal-record", repeatable, r.controller.SetCriminalRecord)
	me.POST("/criminal-record/reminder", repeatable, r.controller.RemindCriminalRecord)
	me.PUT("/vehicle", repeatable, r.controller.SetVehicle)
	me.PUT("/vehicle/registration", repeatable, r.controller.SetVehicleRegistration)
	me.PUT("/vehicle/insurance", repeatable, r.controller.SetInsurance)
	me.PUT("/bank-account", repeatable, r.controller.SetBankAccount)
	me.PUT("/workshop/business-license", repeatable, r.controller.SetBusinessLicense)
	me.PUT("/workshop/commercial-certificate", repeatable, r.controller.SetCommercialCertificate)
	me.PUT("/workshop/tax-id", repeatable, r.controller.SetTaxID)
	me.PUT("/workshop/location", repeatable, r.controller.SetLocation)
	me.PUT("/workshop/services", repeatable, r.controller.SetServices)
	me.PUT("/workshop/bank-account", repeatable, r.controller.SetWorkshopBankAccount)
	me.GET("/review", r.controller.Review)
	me.POST("/submit", repeatable, r.controller.Submit)

	catalogs := api.Group("/providers/catalogs", signedIn...)
	catalogs.GET("/insurers", r.controller.Insurers)
	catalogs.GET("/banks", r.controller.Banks)
	catalogs.GET("/workshop-services", r.controller.WorkshopServices)
	catalogs.GET("/driving-categories", r.controller.DrivingCategories)
	catalogs.GET("/company-types", r.controller.CompanyTypes)

	admin := api.Group("/admin/providers", signedIn...)
	admin.GET("/:id/verification", r.authorizer.RequirePermission(entities.PermProvidersRead), r.controller.AdminView)
}
