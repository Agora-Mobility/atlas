package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/onboarding"
	"github.com/Agora-Mobility/atlas/usecases/onboarding/dtos"
)

type OnboardingController struct {
	onboarding *onboarding.Usecase
}

func NewOnboardingController(uc *onboarding.Usecase) *OnboardingController {
	return &OnboardingController{onboarding: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{onboarding.ErrRegistrationNotAllowed, http.StatusForbidden, "REGISTRATION_NOT_ALLOWED", "Provider accounts are created in the provider app."},
	{onboarding.ErrPhoneTaken, http.StatusConflict, "PHONE_ALREADY_EXISTS", "Phone already registered."},
	{onboarding.ErrEmailTaken, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "E-mail already registered."},
	{onboarding.ErrServiceNotOpen, http.StatusUnprocessableEntity, "SERVICE_NOT_OPEN", "This service does not accept providers."},
	{onboarding.ErrDocumentTypeUnknown, http.StatusUnprocessableEntity, "UNKNOWN_DOCUMENT_TYPE", "Unknown document type."},
	{onboarding.ErrContentTypeRejected, http.StatusUnprocessableEntity, "FILE_TYPE_NOT_ACCEPTED", "This file type is not accepted for this document."},
	{onboarding.ErrFileTooLarge, http.StatusUnprocessableEntity, "FILE_TOO_LARGE", "The file is too large."},
	{onboarding.ErrObjectKeyInvalid, http.StatusUnprocessableEntity, "INVALID_OBJECT_KEY", "The object key does not belong to this document."},
	{onboarding.ErrObjectNotFound, http.StatusUnprocessableEntity, "FILE_NOT_UPLOADED", "The file was not uploaded."},
	{onboarding.ErrDocumentNotFound, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "Document not found."},
	{onboarding.ErrProviderNotFound, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider not found."},
	{onboarding.ErrProviderBusy, http.StatusConflict, "PROVIDER_BUSY", "The provider has an active job."},
	{onboarding.ErrReviewNotAllowed, http.StatusConflict, "REVIEW_NOT_ALLOWED", "Documents are reviewed while the application is pending."},
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
	var incomplete *onboarding.IncompleteError
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
//	@Summary		Create a provider account
//	@Description	Only through agora-provider-app: the account type comes from the client, never from the payload. Creates the account and a draft profile, and sends an SMS code. Confirm it with POST /api/v1/auth/phone/verify to get a session.
//	@Tags			Provider onboarding
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RegisterProviderInput	true	"Account"
//	@Success		201		{object}	dtos.RegisteredProvider
//	@Failure		403		{object}	shared.ErrorResponse	"Not the provider app"
//	@Failure		409		{object}	shared.ErrorResponse	"Phone or e-mail already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security		ClientId
//	@Router			/api/v1/providers/register [post]
func (c *OnboardingController) Register(ctx *gin.Context) {
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
	view, err := c.onboarding.Register(ctx.Request.Context(), client, in)
	respond(ctx, http.StatusCreated, view, err)
}

// OpenServices godoc
//
//	@Summary		Services that accept providers
//	@Description	By category, for example Táxi with Económico, Conforto and XL. A service only appears once its requirements are configured.
//	@Tags			Provider onboarding
//	@Produce		json
//	@Success		200	{object}	dtos.OpenServices
//	@Security		ClientId
//	@Router			/api/v1/providers/onboarding/services [get]
func (c *OnboardingController) OpenServices(ctx *gin.Context) {
	view, err := c.onboarding.OpenServices(ctx.Request.Context())
	respond(ctx, http.StatusOK, view, err)
}

// MyProfile godoc
//
//	@Summary		My provider profile
//	@Description	Status, data, documents, what the requested service requires and what is still missing.
//	@Tags			Provider onboarding
//	@Produce		json
//	@Success		200	{object}	dtos.ProfileView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/profile [get]
func (c *OnboardingController) MyProfile(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.onboarding.MyProfile(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// RequestService godoc
//
//	@Summary		Choose the service to provide
//	@Description	Stored as the requested service. The service a provider really takes comes from the approval.
//	@Tags			Provider onboarding
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RequestServiceInput	true	"Service"
//	@Success		200		{object}	dtos.ProfileView
//	@Failure		409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422		{object}	shared.ErrorResponse	"SERVICE_NOT_OPEN"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/service [put]
func (c *OnboardingController) RequestService(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.RequestServiceInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.onboarding.RequestService(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// SetPersonalData godoc
//
//	@Summary	Save the personal data
//	@Tags		Provider onboarding
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.PersonalDataInput	true	"Personal data"
//	@Success	200		{object}	dtos.ProfileView
//	@Failure	409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/personal [put]
func (c *OnboardingController) SetPersonalData(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.PersonalDataInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.onboarding.SetPersonalData(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// SetVehicle godoc
//
//	@Summary	Save the vehicle
//	@Tags		Provider onboarding
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.VehicleInput	true	"Vehicle"
//	@Success	200		{object}	dtos.ProfileView
//	@Failure	409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/vehicle [put]
func (c *OnboardingController) SetVehicle(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.VehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.onboarding.SetVehicle(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// UploadURL godoc
//
//	@Summary		Get a URL to upload a document
//	@Description	Validates the type and the size. Then PUT the file directly to upload_url with the returned headers, and register it with POST /api/v1/providers/me/documents.
//	@Tags			Provider onboarding
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
func (c *OnboardingController) UploadURL(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.UploadURLInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.onboarding.UploadURL(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// RegisterDocument godoc
//
//	@Summary		Register an uploaded document
//	@Description	Confirms that the file exists and that its content really is an accepted type. A new file replaces the previous one of that type, which is how a rejected document is sent again.
//	@Tags			Provider onboarding
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RegisterDocumentInput	true	"Document"
//	@Success		201		{object}	dtos.ProfileView
//	@Failure		409		{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422		{object}	shared.ErrorResponse	"FILE_NOT_UPLOADED, INVALID_OBJECT_KEY or FILE_TYPE_NOT_ACCEPTED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/documents [post]
func (c *OnboardingController) RegisterDocument(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.RegisterDocumentInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.onboarding.RegisterDocument(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// MyDocumentURL godoc
//
//	@Summary	Short-lived link to one of my documents
//	@Tags		Provider onboarding
//	@Produce	json
//	@Param		type	path		string	true	"Document type"
//	@Success	200		{object}	dtos.DocumentURLOutput
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/documents/{type}/url [get]
func (c *OnboardingController) MyDocumentURL(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.onboarding.MyDocumentURL(ctx.Request.Context(), providerID, ctx.Param("type"))
	respond(ctx, http.StatusOK, view, err)
}

// Submit godoc
//
//	@Summary		Submit the profile for review
//	@Description	Answers 422 PROFILE_INCOMPLETE with the list of what is missing in details.
//	@Tags			Provider onboarding
//	@Produce		json
//	@Success		200	{object}	dtos.ProfileView
//	@Failure		409	{object}	shared.ErrorResponse	"PROFILE_LOCKED"
//	@Failure		422	{object}	shared.ErrorResponse	"PROFILE_INCOMPLETE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/submit [post]
func (c *OnboardingController) Submit(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.onboarding.Submit(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// ListApplications godoc
//
//	@Summary		List provider applications
//	@Description	Requires providers:read. Oldest submission first.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			status	query		string	false	"draft, pending, approved, rejected or suspended"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.ApplicationList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers [get]
func (c *OnboardingController) ListApplications(ctx *gin.Context) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	list, err := c.onboarding.ListApplications(ctx.Request.Context(), ctx.Query("status"), page)
	respond(ctx, http.StatusOK, list, err)
}

// GetApplication godoc
//
//	@Summary		Get a provider application
//	@Description	Requires providers:read. Profile, requested service and documents, each with a short-lived link.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			id	path		string	true	"User id of the provider"
//	@Success		200	{object}	dtos.ProfileView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id} [get]
func (c *OnboardingController) GetApplication(ctx *gin.Context) {
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	view, err := c.onboarding.GetApplication(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, view, err)
}

// ReviewDocument godoc
//
//	@Summary		Approve or reject a document
//	@Description	Requires providers:approve. Rejecting needs a reason.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"User id of the provider"
//	@Param			type	path		string						true	"Document type"
//	@Param			body	body		dtos.ReviewDocumentInput	true	"Decision"
//	@Success		200		{object}	dtos.ProfileView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"REVIEW_NOT_ALLOWED"
//	@Failure		422		{object}	shared.ErrorResponse	"REASON_REQUIRED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id}/documents/{type}/review [post]
func (c *OnboardingController) ReviewDocument(ctx *gin.Context) {
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
	view, err := c.onboarding.ReviewDocument(ctx.Request.Context(), actor, id, ctx.Param("type"), in)
	respond(ctx, http.StatusOK, view, err)
}

// ReviewProvider godoc
//
//	@Summary		Decide on a provider
//	@Description	Requires providers:approve. Never creates a provider: accounts are created in the app. Approving needs service_id, usually the requested service, and every required document approved. Rejecting and suspending need a reason. A suspended provider can be approved again.
//	@Tags			Backoffice providers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"User id of the provider"
//	@Param			body	body		dtos.ReviewProviderInput	true	"Decision"
//	@Success		200		{object}	dtos.ProfileView
//	@Failure		404		{object}	shared.ErrorResponse	"PROVIDER_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"INVALID_PROVIDER_TRANSITION or PROVIDER_BUSY"
//	@Failure		422		{object}	shared.ErrorResponse	"Missing service_id or APPROVAL_REQUIREMENTS_NOT_MET"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id} [put]
func (c *OnboardingController) ReviewProvider(ctx *gin.Context) {
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
	view, err := c.onboarding.ReviewProvider(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}
