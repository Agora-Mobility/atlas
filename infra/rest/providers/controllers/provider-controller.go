package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/providers"
	"github.com/Agora-Mobility/atlas/usecases/providers/dtos"
)

type ProviderController struct {
	providers *providers.Usecase
}

func NewProviderController(uc *providers.Usecase) *ProviderController {
	return &ProviderController{providers: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{providers.ErrRegistrationNotAllowed, http.StatusForbidden, "REGISTRATION_NOT_ALLOWED", "Provider accounts are created in the provider app."},
	{providers.ErrPhoneTaken, http.StatusConflict, "PHONE_ALREADY_EXISTS", "Phone already registered."},
	{providers.ErrEmailTaken, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "E-mail already registered."},
	{providers.ErrServiceNotOpen, http.StatusUnprocessableEntity, "SERVICE_NOT_OPEN", "This service does not accept providers."},
	{providers.ErrDocumentTypeUnknown, http.StatusUnprocessableEntity, "UNKNOWN_DOCUMENT_TYPE", "Unknown document type."},
	{providers.ErrContentTypeRejected, http.StatusUnprocessableEntity, "FILE_TYPE_NOT_ACCEPTED", "This file type is not accepted for this document."},
	{providers.ErrFileTooLarge, http.StatusUnprocessableEntity, "FILE_TOO_LARGE", "The file is too large."},
	{providers.ErrObjectKeyInvalid, http.StatusUnprocessableEntity, "INVALID_OBJECT_KEY", "The object key does not belong to this document."},
	{providers.ErrObjectNotFound, http.StatusUnprocessableEntity, "FILE_NOT_UPLOADED", "The file was not uploaded."},
	{providers.ErrDocumentNotFound, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "Document not found."},
	{providers.ErrProviderNotFound, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider not found."},
	{providers.ErrProviderBusy, http.StatusConflict, "PROVIDER_BUSY", "The provider has an active job."},
	{providers.ErrReviewNotAllowed, http.StatusConflict, "REVIEW_NOT_ALLOWED", "Documents are reviewed while the application is pending."},
	{providers.ErrManagedByFleet, http.StatusConflict, "MANAGED_BY_FLEET", "The fleet manages this for its drivers."},
	{providers.ErrRejectionReasonUnknown, http.StatusNotFound, "REJECTION_REASON_NOT_FOUND", "Rejection reason not found."},
	{entities.ErrUnknownRejectionReason, http.StatusUnprocessableEntity, "UNKNOWN_REJECTION_REASON", "A reason does not exist or does not apply here."},
	{repository.ErrRejectionReasonCodeTaken, http.StatusConflict, "REJECTION_REASON_CODE_TAKEN", "This scope already has a reason with this code."},
	{entities.ErrProfileLocked, http.StatusConflict, "PROFILE_LOCKED", "The profile cannot change in this state."},
	{entities.ErrInvalidProviderTransition, http.StatusConflict, "INVALID_PROVIDER_TRANSITION", "This change of status is not possible."},
	{entities.ErrReviewReasonRequired, http.StatusUnprocessableEntity, "REASON_REQUIRED", "A reason is required."},
	{services.ErrStorageUnavailable, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "File storage is not available."},
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
	var incomplete *providers.IncompleteError
	if errors.As(err, &incomplete) {
		shared.UnprocessableWithDetails(ctx, incomplete.Code, "Some requirements are not met.", incomplete.Fields)
		return
	}
	for _, mapped := range errorTable {
		if errors.Is(err, mapped.target) {
			shared.RespondError(ctx, mapped.status, mapped.code, mapped.message)
			return
		}
	}
	if validationErr, ok := customerrors.AsValidation(err); ok {
		shared.ValidationFailed(ctx, validationErr)
		return
	}
	shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
}

func currentProvider(ctx *gin.Context) (uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	if role != enums.RoleProvider {
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
		return uuid.Nil, false
	}
	return userID, true
}

func staffActor(ctx *gin.Context) (entities.Actor, bool) {
	reqCtx := ctx.Request.Context()
	if _, ok := middleware.UserIDFromContext(reqCtx); !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return entities.Actor{}, false
	}
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return entities.Actor{}, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	return access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent()), true
}

func providerParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "PROVIDER_NOT_FOUND", "Provider not found.")
		return uuid.Nil, false
	}
	return id, true
}

// Register godoc
//
//	@Summary		Create a provider account with the phone
//	@Description	Only through agora-provider-app: the account type comes from the client, never from the payload. Takes only the phone, creates the account and a draft profile, and sends an SMS code. Calling it again for an unverified number sends a new code. Confirm it with POST /api/v1/auth/phone/verify to get a session. A registered number answers PHONE_ALREADY_EXISTS: the app moves to the sign-in by SMS code. The name and the other data are filled in later in the profile.
//	@Tags			Provider account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RegisterProviderInput	true	"Account"
//	@Success		201		{object}	dtos.RegisteredProvider
//	@Failure		403		{object}	shared.ErrorResponse	"Not the provider app"
//	@Failure		409		{object}	shared.ErrorResponse	"Phone or e-mail already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security		ClientId
//	@Router			/api/v1/providers/register [post]
func (c *ProviderController) Register(ctx *gin.Context) {
	client, ok := middleware.ClientFromContext(ctx.Request.Context())
	if !ok {
		shared.BadRequest(ctx, "UNKNOWN_CLIENT", "Missing or unknown client.")
		return
	}
	var in dtos.RegisterProviderInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.Register(ctx.Request.Context(), client, in)
	respond(ctx, http.StatusCreated, view, err)
}

// OpenServices godoc
//
//	@Summary		Services that accept providers
//	@Description	By category, for example Táxi with Económico, Conforto and XL. A service only appears once its requirements are configured.
//	@Tags			Provider account
//	@Produce		json
//	@Success		200	{object}	dtos.OpenServices
//	@Security		ClientId
//	@Router			/api/v1/providers/services [get]
func (c *ProviderController) OpenServices(ctx *gin.Context) {
	view, err := c.providers.OpenServices(ctx.Request.Context())
	respond(ctx, http.StatusOK, view, err)
}

// MyProfile godoc
//
//	@Summary		My provider profile
//	@Description	Status, data, documents, what the requested service requires and what is still missing.
//	@Tags			Provider account
//	@Produce		json
//	@Success		200	{object}	dtos.ProviderView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/profile [get]
func (c *ProviderController) MyProfile(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.providers.Me(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// SetProfile godoc
//
//	@Summary		Choose the service and save the personal data
//	@Description	One step for the service and the personal data. A provider offers a single service: choosing again replaces the requested one. service_id comes from GET /api/v1/providers/services; the service a provider really takes comes from the approval. A fleet driver keeps the service the fleet chose. id_number is the BI: 9 digits, 2 letters and 3 digits. email is optional and must not belong to another account; sending it empty removes it. Only in draft or rejected.
//	@Tags			Provider account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.ProfileInput	true	"Service and personal data"
//	@Success		200		{object}	dtos.ProviderView
//	@Failure		409		{object}	shared.ErrorResponse	"PROFILE_LOCKED, MANAGED_BY_FLEET or EMAIL_ALREADY_EXISTS"
//	@Failure		422		{object}	shared.ErrorResponse	"SERVICE_NOT_OPEN or invalid fields"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/profile [put]
func (c *ProviderController) SetProfile(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.ProfileInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.SetProfile(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// SetVehicle godoc
//
//	@Summary	Save the vehicle
//	@Tags		Provider account
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.VehicleInput	true	"Vehicle"
//	@Success	200		{object}	dtos.ProviderView
//	@Failure	409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/vehicle [put]
func (c *ProviderController) SetVehicle(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.VehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.SetVehicle(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// UploadURL godoc
//
//	@Summary		Get a URL to upload a document
//	@Description	Validates the type and the size. Then PUT the file directly to upload_url with the returned headers, and register it with POST /api/v1/providers/me/documents.
//	@Tags			Provider account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.UploadURLInput	true	"File"
//	@Success		200		{object}	dtos.UploadURLOutput
//	@Failure		409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422		{object}	shared.ErrorResponse	"FILE_TYPE_NOT_ACCEPTED, FILE_TOO_LARGE or UNKNOWN_DOCUMENT_TYPE"
//	@Failure		503		{object}	shared.ErrorResponse	"STORAGE_UNAVAILABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/documents/upload-url [post]
func (c *ProviderController) UploadURL(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.UploadURLInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.UploadURL(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// RegisterDocument godoc
//
//	@Summary		Register an uploaded document
//	@Description	Confirms that the file exists and that its content really is an accepted type. A new file replaces the previous one of that type, which is how a rejected document is sent again.
//	@Tags			Provider account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RegisterDocumentInput	true	"Document"
//	@Success		201		{object}	dtos.ProviderView
//	@Failure		409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422		{object}	shared.ErrorResponse	"FILE_NOT_UPLOADED, INVALID_OBJECT_KEY or FILE_TYPE_NOT_ACCEPTED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/documents [post]
func (c *ProviderController) RegisterDocument(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.RegisterDocumentInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.RegisterDocument(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// MyDocumentURL godoc
//
//	@Summary	Short-lived link to one of my documents
//	@Tags		Provider account
//	@Produce	json
//	@Param		type	path		string	true	"Document type"
//	@Success	200		{object}	dtos.DocumentURLOutput
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/documents/{type}/url [get]
func (c *ProviderController) MyDocumentURL(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.providers.MyDocumentURL(ctx.Request.Context(), providerID, ctx.Param("type"))
	respond(ctx, http.StatusOK, view, err)
}

// Submit godoc
//
//	@Summary		Submit the profile for review
//	@Description	Answers 422 PROFILE_INCOMPLETE with the list of what is missing in details.
//	@Tags			Provider account
//	@Produce		json
//	@Success		200	{object}	dtos.ProviderView
//	@Failure		409	{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422	{object}	shared.ErrorResponse	"PROFILE_INCOMPLETE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/submit [post]
func (c *ProviderController) Submit(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.providers.Submit(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// ListApplications godoc
//
//	@Summary		List provider applications
//	@Description	Requires providers:read. Oldest submission first.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			status			query		string	false	"draft, pending, approved, rejected or suspended"
//	@Param			organization_id	query		string	false	"Partner of the provider"
//	@Param			service_id		query		string	false	"Active or requested service"
//	@Param			category		query		string	false	"Category slug, such as taxi or towing"
//	@Param			search			query		string	false	"Name, phone or plate"
//	@Param			page			query		int		false	"Page"
//	@Param			size			query		int		false	"Page size"
//	@Success		200				{object}	dtos.ApplicationList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers [get]
func (c *ProviderController) ListApplications(ctx *gin.Context) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	organizationID, ok := optionalUUID(ctx, "organization_id")
	if !ok {
		return
	}
	serviceID, ok := optionalUUID(ctx, "service_id")
	if !ok {
		return
	}
	filter := dtos.ApplicationFilter{Status: ctx.Query("status"), OrganizationID: organizationID, ServiceID: serviceID, Category: ctx.Query("category"), Search: ctx.Query("search")}
	list, err := c.providers.ListApplications(ctx.Request.Context(), filter, page)
	respond(ctx, http.StatusOK, list, err)
}

// GetApplication godoc
//
//	@Summary		Get a provider application
//	@Description	Requires providers:read. Profile, requested service and documents, each with a short-lived link.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			id	path		string	true	"User id of the provider"
//	@Success		200	{object}	dtos.ProviderView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id} [get]
func (c *ProviderController) GetApplication(ctx *gin.Context) {
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	view, err := c.providers.GetApplication(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, view, err)
}

// ReviewDocument godoc
//
//	@Summary		Approve or reject a document
//	@Description	Requires providers:approve. Rejecting needs reason_ids from GET /admin/rejection-reasons?scope=document. A reason with requires_note also needs note.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"User id of the provider"
//	@Param			type	path		string						true	"Document type"
//	@Param			body	body		dtos.ReviewDocumentInput	true	"Decision"
//	@Success		200		{object}	dtos.ProviderView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"REVIEW_NOT_ALLOWED"
//	@Failure		422		{object}	shared.ErrorResponse	"REASON_REQUIRED or UNKNOWN_REJECTION_REASON"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id}/documents/{type}/review [post]
func (c *ProviderController) ReviewDocument(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	var in dtos.ReviewDocumentInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.ReviewDocument(ctx.Request.Context(), actor, id, ctx.Param("type"), in)
	respond(ctx, http.StatusOK, view, err)
}

// ReviewProvider godoc
//
//	@Summary		Decide on a provider
//	@Description	Requires providers:approve. Never creates a provider: accounts are created in the app. Approving needs service_id, usually the requested service, and every required document approved. Rejecting needs reason_ids with scope provider and suspending needs reason_ids with scope suspension. A reason with requires_note also needs note. A suspended provider can be approved again.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"User id of the provider"
//	@Param			body	body		dtos.ReviewProviderInput	true	"Decision"
//	@Success		200		{object}	dtos.ProviderView
//	@Failure		404		{object}	shared.ErrorResponse	"PROVIDER_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"INVALID_PROVIDER_TRANSITION or PROVIDER_BUSY"
//	@Failure		422		{object}	shared.ErrorResponse	"Missing service_id or APPROVAL_REQUIREMENTS_NOT_MET"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id} [put]
func (c *ProviderController) ReviewProvider(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	var in dtos.ReviewProviderInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.ReviewProvider(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

func optionalUUID(ctx *gin.Context, name string) (*uuid.UUID, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+name+".")
		return nil, false
	}
	return &id, true
}

// ListRejectionReasons godoc
//
//	@Summary		List rejection reasons
//	@Description	Requires providers:read. Fills the reason selector of a review. Scopes: document, provider and suspension.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			scope				query		string	false	"document, provider or suspension"
//	@Param			include_inactive	query		bool	false	"Also return inactive reasons"
//	@Success		200					{object}	dtos.RejectionReasonList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/rejection-reasons [get]
func (c *ProviderController) ListRejectionReasons(ctx *gin.Context) {
	list, err := c.providers.ListRejectionReasons(ctx.Request.Context(), ctx.Query("scope"), ctx.Query("include_inactive") == "true")
	respond(ctx, http.StatusOK, list, err)
}

// CreateRejectionReason godoc
//
//	@Summary		Create a rejection reason
//	@Description	Requires providers:approve.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CreateRejectionReasonInput	true	"Reason"
//	@Success		201		{object}	dtos.RejectionReasonView
//	@Failure		409		{object}	shared.ErrorResponse	"REJECTION_REASON_CODE_TAKEN"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/rejection-reasons [post]
func (c *ProviderController) CreateRejectionReason(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.CreateRejectionReasonInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.CreateRejectionReason(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusCreated, view, err)
}

// UpdateRejectionReason godoc
//
//	@Summary		Update a rejection reason
//	@Description	Requires providers:approve. Set is_active to false to retire a reason; past reviews keep it.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string							true	"Reason id"
//	@Param			body	body		dtos.UpdateRejectionReasonInput	true	"Reason"
//	@Success		200		{object}	dtos.RejectionReasonView
//	@Failure		404		{object}	shared.ErrorResponse	"REJECTION_REASON_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/rejection-reasons/{id} [put]
func (c *ProviderController) UpdateRejectionReason(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "REJECTION_REASON_NOT_FOUND", "Rejection reason not found.")
		return
	}
	var in dtos.UpdateRejectionReasonInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.providers.UpdateRejectionReason(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}
