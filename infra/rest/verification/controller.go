package verification

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
	"github.com/Agora-Mobility/atlas/usecases/verification"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

type Controller struct {
	verification *verification.Usecase
}

func NewController(uc *verification.Usecase) *Controller {
	return &Controller{verification: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{verification.ErrLocked, http.StatusConflict, "VERIFICATION_LOCKED", "The verification cannot change while it is under review."},
	{verification.ErrServiceNotChosen, http.StatusUnprocessableEntity, "SERVICE_NOT_CHOSEN", "Choose the service in the profile first."},
	{verification.ErrServiceNotOpen, http.StatusUnprocessableEntity, "SERVICE_NOT_OPEN", "This service does not accept providers."},
	{verification.ErrProviderNotFound, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider not found."},
	{verification.ErrUploadNotFound, http.StatusNotFound, "UPLOAD_NOT_FOUND", "Upload not found."},
	{verification.ErrFileNotUploaded, http.StatusUnprocessableEntity, "FILE_NOT_UPLOADED", "The file was not uploaded."},
	{verification.ErrUnknownDocument, http.StatusUnprocessableEntity, "UNKNOWN_DOCUMENT_TYPE", "Unknown document type."},
	{verification.ErrDocumentNotInPlan, http.StatusUnprocessableEntity, "DOCUMENT_NOT_IN_PLAN", "This document is not part of this verification."},
	{verification.ErrStepNotInPlan, http.StatusUnprocessableEntity, "STEP_NOT_IN_PLAN", "This step is not part of this verification."},
	{verification.ErrTooManyExtras, http.StatusUnprocessableEntity, "TOO_MANY_EXTRA_PHOTOS", "There are already too many extra photos."},
	{verification.ErrUploadNotReady, http.StatusConflict, "UPLOAD_NOT_READY", "The file is not confirmed yet."},
	{verification.ErrLivenessSession, http.StatusUnprocessableEntity, "LIVENESS_SESSION_INVALID", "Start a new liveness session."},
	{verification.ErrDeclarationRequired, http.StatusUnprocessableEntity, "DECLARATION_REQUIRED", "The declaration must be accepted."},
	{verification.ErrIdentityNotConfirmed, http.StatusUnprocessableEntity, "IDENTITY_NOT_CONFIRMED", "Confirm the identity card data first."},
	{verification.ErrBankNotFound, http.StatusUnprocessableEntity, "BANK_NOT_FOUND", "Bank not found."},
	{verification.ErrInsurerNotFound, http.StatusUnprocessableEntity, "INSURER_NOT_FOUND", "Insurer not found."},
	{verification.ErrManagedByFleet, http.StatusConflict, "MANAGED_BY_FLEET", "The fleet manages this for its drivers."},
	{entities.ErrProfileLocked, http.StatusConflict, "VERIFICATION_LOCKED", "The verification cannot change while it is under review."},
	{services.ErrStorageUnavailable, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "File storage is not available."},
}

var ruleMessages = map[string]string{
	verification.CodeQualityRejected:      "The photo did not pass the quality check.",
	verification.CodeIdentityExpired:      "The identity card has expired.",
	verification.CodeExtractionMax:        "The identity card could not be read. Enter the data by hand.",
	verification.CodeLivenessFailed:       "The liveness check failed.",
	verification.CodeInvalidFileType:      "This file type is not accepted here.",
	verification.CodeFileTooLarge:         "The file is too large.",
	verification.CodeInvalidPlate:         "The plate is not valid.",
	verification.CodeVehicleTooOld:        "The vehicle is too old for this service.",
	verification.CodeCategoryRequired:     "The driving license needs another category for this service.",
	verification.CodeDocumentExpired:      "The document has expired.",
	verification.CodeCertificateTooOld:    "The certificate is too old.",
	verification.CodeInvalidIBAN:          "The IBAN is not valid.",
	verification.CodeIBANBankMismatch:     "The IBAN belongs to another bank.",
	verification.CodeHolderNameMismatch:   "The account holder does not match.",
	verification.CodeInvalidNIF:           "The NIF is not valid.",
	verification.CodeIncomplete:           "Some steps are still missing.",
	verification.CodeImagesRequired:       "Some files are still missing.",
	verification.CodeManualEntryForbidden: "Try the automatic reading first.",
	verification.CodeExtractionNotDone:    "The reading has not finished.",
	verification.CodeUnderAge:             "The provider is under the minimum age.",
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
	var rule *entities.RuleError
	if errors.As(err, &rule) {
		message := ruleMessages[rule.Code]
		if message == "" {
			message = "Some fields are invalid."
		}
		var details map[string]string
		if rule.Field != "" {
			details = map[string]string{rule.Field: rule.Detail}
		} else if rule.Detail != "" {
			details = map[string]string{"reason": rule.Detail}
		}
		shared.UnprocessableWithData(ctx, rule.Code, message, details, rule.Data)
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

func uploadParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "UPLOAD_NOT_FOUND", "Upload not found.")
		return uuid.Nil, false
	}
	return id, true
}

func withBody[T any](ctx *gin.Context, run func(providerID uuid.UUID, in T) (any, error)) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in T
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := run(providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// Overview godoc
//
//	@Summary		Verification progress and steps
//	@Description	Everything the step list needs (PK01, PK15, PK16, PK01-B): status, driver or workshop, progress computed by the server, minutes left, next step and the groups with the status of each step. The first call creates the verification for the service chosen in the profile.
//	@Tags			Provider verification
//	@Produce		json
//	@Success		200	{object}	dtos.VerificationView
//	@Failure		422	{object}	shared.ErrorResponse	"SERVICE_NOT_CHOSEN"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification [get]
func (c *Controller) Overview(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.verification.Overview(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// StartUpload godoc
//
//	@Summary		Start the upload of a document file
//	@Description	Checks the document type, slot, subject, content type and size, and returns a pre-signed PUT to R2 with the headers to send. Then PUT the file and call the confirm endpoint. A new file for the same slot replaces the old one once it is accepted; the old one is kept as superseded. The identity card sides are slots front and back of id_card; the SIAC renewal receipt is the renewal_receipt slot.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string					false	"Repeat safely"
//	@Param			body			body		dtos.StartUploadInput	true	"File"
//	@Success		201				{object}	dtos.StartedUpload
//	@Failure		409				{object}	shared.ErrorResponse	"VERIFICATION_LOCKED"
//	@Failure		422				{object}	shared.ErrorResponse	"INVALID_FILE_TYPE, FILE_TOO_LARGE, INVALID_SLOT or DOCUMENT_NOT_IN_PLAN"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/uploads [post]
func (c *Controller) StartUpload(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.StartUploadInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.verification.StartUpload(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// ConfirmUpload godoc
//
//	@Summary		Confirm an uploaded file
//	@Description	Checks with HeadObject that the file exists and fits the size, and checks the magic bytes. The file then goes to processing: EXIF and GPS are removed and the quality is checked in the queue. Poll GET /uploads/{id} for accepted or quality_failed with the reasons (BI-E4). Confirming twice answers the same file.
//	@Tags			Provider verification
//	@Produce		json
//	@Param			id	path		string	true	"Upload id"
//	@Success		200	{object}	dtos.UploadView
//	@Failure		404	{object}	shared.ErrorResponse	"UPLOAD_NOT_FOUND"
//	@Failure		422	{object}	shared.ErrorResponse	"FILE_NOT_UPLOADED, FILE_TOO_LARGE or INVALID_FILE_TYPE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/uploads/{id}/confirm [post]
func (c *Controller) ConfirmUpload(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	id, ok := uploadParam(ctx)
	if !ok {
		return
	}
	view, err := c.verification.ConfirmUpload(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// Upload godoc
//
//	@Summary		State of an uploaded file
//	@Description	processing, accepted or quality_failed, with each quality check and the reasons (TEXT_NOT_LEGIBLE, SHADOW_ON_DATA, CORNERS_NOT_VISIBLE, GLARE, BLURRY, TOO_DARK).
//	@Tags			Provider verification
//	@Produce		json
//	@Param			id	path		string	true	"Upload id"
//	@Success		200	{object}	dtos.UploadView
//	@Failure		404	{object}	shared.ErrorResponse	"UPLOAD_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/uploads/{id} [get]
func (c *Controller) Upload(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	id, ok := uploadParam(ctx)
	if !ok {
		return
	}
	view, err := c.verification.Upload(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// UploadURL godoc
//
//	@Summary		Short-lived link to see my file
//	@Description	A pre-signed GET of a few minutes, only for the owner. Files are never public.
//	@Tags			Provider verification
//	@Produce		json
//	@Param			id	path		string	true	"Upload id"
//	@Success		200	{object}	dtos.ReadURL
//	@Failure		404	{object}	shared.ErrorResponse	"UPLOAD_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/uploads/{id}/url [get]
func (c *Controller) UploadURL(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	id, ok := uploadParam(ctx)
	if !ok {
		return
	}
	view, err := c.verification.UploadURL(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// StartExtraction godoc
//
//	@Summary		Start reading the identity card (BI04)
//	@Description	Needs the front and the back accepted. The reading runs in the queue: poll GET with the same path. At most three attempts; after that EXTRACTION_MAX_ATTEMPTS and the app moves to manual entry (BI-E6).
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.SubjectInput	false	"Whose card"
//	@Success		202		{object}	dtos.ExtractionView
//	@Failure		422		{object}	shared.ErrorResponse	"DOCUMENT_FILES_MISSING or EXTRACTION_MAX_ATTEMPTS"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/identity-card/extraction [post]
func (c *Controller) StartExtraction(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.SubjectInput
	if ctx.Request.ContentLength > 0 {
		if err := binding.JSON(ctx, &in); err != nil {
			shared.InvalidRequest(ctx, err)
			return
		}
	}
	view, err := c.verification.StartExtraction(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusAccepted, view, err)
}

// Extraction godoc
//
//	@Summary		State of the identity card reading
//	@Description	processing, completed with the fields and their confidence, or failed. manual_entry_allowed tells when to show the manual form.
//	@Tags			Provider verification
//	@Produce		json
//	@Param			subject	query		string	false	"provider or legal_representative"
//	@Success		200		{object}	dtos.ExtractionView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/identity-card/extraction [get]
func (c *Controller) Extraction(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.verification.Extraction(ctx.Request.Context(), providerID, ctx.Query("subject"))
	respond(ctx, http.StatusOK, view, err)
}

// ConfirmIdentity godoc
//
//	@Summary		Confirm or correct the identity card data (BI05, BI-E6)
//	@Description	Every field can be edited. Fields that differ from the reading are kept in manually_edited_fields and go to review; source manual (after three failed readings) also goes to review. An expired card answers IDENTITY_CARD_EXPIRED unless the SIAC renewal receipt was sent (BI-E5). From here on the card name is the reference for the IBAN holder and the logbook.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string				false	"Repeat safely"
//	@Param			body			body		dtos.IdentityInput	true	"Card data"
//	@Success		200				{object}	verification.StepResult
//	@Failure		422				{object}	shared.ErrorResponse	"IDENTITY_CARD_EXPIRED, MANUAL_ENTRY_NOT_ALLOWED or invalid fields"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/identity-card/data [put]
func (c *Controller) ConfirmIdentity(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.IdentityInput) (any, error) {
		return c.verification.ConfirmIdentity(ctx.Request.Context(), providerID, in)
	})
}

// StartLiveness godoc
//
//	@Summary		Start a liveness session (SF01)
//	@Description	Liveness only: the face is not compared with the card photo. The challenge is BLINK.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.SubjectInput	false	"Whose selfie"
//	@Success		201		{object}	dtos.LivenessSessionView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/selfie/session [post]
func (c *Controller) StartLiveness(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	var in dtos.SubjectInput
	if ctx.Request.ContentLength > 0 {
		if err := binding.JSON(ctx, &in); err != nil {
			shared.InvalidRequest(ctx, err)
			return
		}
	}
	view, err := c.verification.StartLiveness(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// SubmitSelfie godoc
//
//	@Summary		Send the selfie of the liveness session (SF02–SF04)
//	@Description	Upload the final photo as slot main of selfie, then send the session and the upload id. accepted, or LIVENESS_FAILED with attempts and max_attempts. On the third failure the selfie goes to manual review and the step counts as sent.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.SelfieInput	true	"Session and photo"
//	@Success		200		{object}	dtos.SelfieView
//	@Failure		422		{object}	shared.ErrorResponse	"LIVENESS_FAILED or LIVENESS_SESSION_INVALID"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/selfie [post]
func (c *Controller) SubmitSelfie(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.SelfieInput) (any, error) {
		return c.verification.SubmitSelfie(ctx.Request.Context(), providerID, in)
	})
}

// SetDrivingLicense godoc
//
//	@Summary		Driving license (PK05)
//	@Description	The photos are the front and back slots of driving_license. The license must not be expired and must have a category the service needs (taxi B, towing and removals C1 or C).
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.DrivingLicenseInput	true	"License"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"DOCUMENT_EXPIRED or DRIVING_CATEGORY_REQUIRED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/driving-license [put]
func (c *Controller) SetDrivingLicense(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.DrivingLicenseInput) (any, error) {
		return c.verification.SetDrivingLicense(ctx.Request.Context(), providerID, in)
	})
}

// SetCriminalRecord godoc
//
//	@Summary		Criminal record (PK12)
//	@Description	The file is the main slot of criminal_record (photo or PDF). Issued at most 90 days ago and not in the future.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CriminalRecordInput	true	"Certificate"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"CERTIFICATE_TOO_OLD"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/criminal-record [put]
func (c *Controller) SetCriminalRecord(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.CriminalRecordInput) (any, error) {
		return c.verification.SetCriminalRecord(ctx.Request.Context(), providerID, in)
	})
}

// RemindCriminalRecord godoc
//
//	@Summary		Remind me tomorrow (PK12)
//	@Description	Schedules a push in 24 hours. The progress stays saved.
//	@Tags			Provider verification
//	@Produce		json
//	@Success		200	{object}	dtos.ReminderView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/criminal-record/reminder [post]
func (c *Controller) RemindCriminalRecord(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.verification.RemindCriminalRecord(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// SetVehicle godoc
//
//	@Summary		Vehicle (PK06)
//	@Description	Angolan plate AA-00-00-AA, accepted without hyphens. The five photos are the front, rear, side, interior and plate slots of vehicle_photo.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.VehicleInput	true	"Vehicle"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"INVALID_PLATE or VEHICLE_TOO_OLD"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/vehicle [put]
func (c *Controller) SetVehicle(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.VehicleInput) (any, error) {
		return c.verification.SetVehicle(ctx.Request.Context(), providerID, in)
	})
}

// SetVehicleRegistration godoc
//
//	@Summary		Logbook (PK13)
//	@Description	The photos are page_1 and page_2 of vehicle_registration. When the owner is not the person on the card, the owner declaration (vehicle_owner_declaration) and the owner's card (id_card with subject vehicle_owner) are needed and the step goes to review.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.VehicleRegistrationInput	true	"Owner"
//	@Success		200		{object}	dtos.VehicleRegistrationView
//	@Failure		422		{object}	shared.ErrorResponse	"IDENTITY_NOT_CONFIRMED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/vehicle/registration [put]
func (c *Controller) SetVehicleRegistration(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.VehicleRegistrationInput) (any, error) {
		return c.verification.SetVehicleRegistration(ctx.Request.Context(), providerID, in)
	})
}

// SetInsurance godoc
//
//	@Summary		Insurance and inspection (PK14, PK14-A)
//	@Description	The certificates are the main slot of vehicle_insurance and vehicle_inspection. Both dates must be in the future. Another insurer goes to review.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.InsuranceInput	true	"Insurance"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"DOCUMENT_EXPIRED or INSURER_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/vehicle/insurance [put]
func (c *Controller) SetInsurance(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.InsuranceInput) (any, error) {
		return c.verification.SetInsurance(ctx.Request.Context(), providerID, in)
	})
}

// SetBankAccount godoc
//
//	@Summary		Bank details (PK07, PK07-A)
//	@Description	AO06 with 21 digits and mod-97. Digits five to eight name the bank: another bank answers IBAN_BANK_MISMATCH with the detected bank. The holder must be the person on the card. The proof is the main slot of bank_account. The IBAN is always masked in answers.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.BankAccountInput	true	"Account"
//	@Success		200		{object}	dtos.BankAccountView
//	@Failure		422		{object}	shared.ErrorResponse	"INVALID_IBAN, IBAN_BANK_MISMATCH or HOLDER_NAME_MISMATCH"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/bank-account [put]
func (c *Controller) SetBankAccount(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.BankAccountInput) (any, error) {
		return c.verification.SetBankAccount(ctx.Request.Context(), providerID, in, enums.VerificationKindDriver)
	})
}

// SetWorkshopBankAccount godoc
//
//	@Summary		Company bank details (OV08)
//	@Description	Same rules as the driver, but the holder must be the company name given with the NIF. A personal account is refused on the holder field.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.BankAccountInput	true	"Account"
//	@Success		200		{object}	dtos.BankAccountView
//	@Failure		422		{object}	shared.ErrorResponse	"INVALID_IBAN, IBAN_BANK_MISMATCH or HOLDER_NAME_MISMATCH"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/bank-account [put]
func (c *Controller) SetWorkshopBankAccount(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.BankAccountInput) (any, error) {
		return c.verification.SetBankAccount(ctx.Request.Context(), providerID, in, enums.VerificationKindWorkshop)
	})
}

// SetBusinessLicense godoc
//
//	@Summary		Business license (OV01)
//	@Description	The file is the main slot of business_license. The validity must be in the future; a notice goes out 30 days before. The repair activity is checked by the team.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.BusinessLicenseInput	true	"License"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"DOCUMENT_EXPIRED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/business-license [put]
func (c *Controller) SetBusinessLicense(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.BusinessLicenseInput) (any, error) {
		return c.verification.SetBusinessLicense(ctx.Request.Context(), providerID, in)
	})
}

// SetCommercialCertificate godoc
//
//	@Summary		Commercial certificate (OV02)
//	@Description	The file is the main slot of commercial_certificate. Issued less than six months ago.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CommercialCertificateInput	true	"Certificate"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"CERTIFICATE_TOO_OLD"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/commercial-certificate [put]
func (c *Controller) SetCommercialCertificate(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.CommercialCertificateInput) (any, error) {
		return c.verification.SetCommercialCertificate(ctx.Request.Context(), providerID, in)
	})
}

// SetTaxID godoc
//
//	@Summary		Company NIF (OV03)
//	@Description	Ten digits. A company name that differs from the business license goes to review without blocking.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.TaxIDInput	true	"Company"
//	@Success		200		{object}	verification.StepResult
//	@Failure		422		{object}	shared.ErrorResponse	"INVALID_NIF"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/tax-id [put]
func (c *Controller) SetTaxID(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.TaxIDInput) (any, error) {
		return c.verification.SetTaxID(ctx.Request.Context(), providerID, in)
	})
}

// SetLocation godoc
//
//	@Summary		Address and opening hours (OV06)
//	@Description	Days 0 (Sunday) to 6 (Saturday), times HH:MM with open before close, at least one open day. Bookings only happen inside these hours.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.LocationInput	true	"Location"
//	@Success		200		{object}	dtos.LocationView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/location [put]
func (c *Controller) SetLocation(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.LocationInput) (any, error) {
		return c.verification.SetLocation(ctx.Request.Context(), providerID, in)
	})
}

// SetServices godoc
//
//	@Summary		Services and prices (OV07)
//	@Description	Base price "from", in whole Kz without VAT, above zero. These services show up in the customer search once the workshop is approved.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			body	body	dtos.ServicesInput	true	"Services"
//	@Success		200		{array}	dtos.PricedServiceView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/workshop/services [put]
func (c *Controller) SetServices(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.ServicesInput) (any, error) {
		return c.verification.SetServices(ctx.Request.Context(), providerID, in)
	})
}

// Review godoc
//
//	@Summary		Review before submitting (PK16, PK08, OV09)
//	@Description	Summary by section with the step to edit, the documents sent out of the total, the masked IBAN, the declaration to accept and the missing steps.
//	@Tags			Provider verification
//	@Produce		json
//	@Success		200	{object}	dtos.ReviewView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/review [get]
func (c *Controller) Review(ctx *gin.Context) {
	providerID, ok := currentProvider(ctx)
	if !ok {
		return
	}
	view, err := c.verification.Review(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// Submit godoc
//
//	@Summary		Submit the verification
//	@Description	Needs the declaration and every step. Moves to review, locks the changes (409 VERIFICATION_LOCKED), tells the backoffice and sends a push to the provider. Submitting again answers the same review.
//	@Tags			Provider verification
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string				false	"Repeat safely"
//	@Param			body			body		dtos.SubmitInput	true	"Declaration"
//	@Success		200				{object}	dtos.ReviewView
//	@Failure		409				{object}	shared.ErrorResponse	"VERIFICATION_LOCKED"
//	@Failure		422				{object}	shared.ErrorResponse	"DECLARATION_REQUIRED or VERIFICATION_INCOMPLETE with data.missing_items"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/verification/submit [post]
func (c *Controller) Submit(ctx *gin.Context) {
	withBody(ctx, func(providerID uuid.UUID, in dtos.SubmitInput) (any, error) {
		return c.verification.Submit(ctx.Request.Context(), providerID, in)
	})
}

// Insurers godoc
//
//	@Summary	Insurers
//	@Tags		Provider verification
//	@Produce	json
//	@Param		search	query		string	false	"Part of the name"
//	@Success	200		{object}	dtos.CatalogList[dtos.InsurerView]
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/catalogs/insurers [get]
func (c *Controller) Insurers(ctx *gin.Context) {
	view, err := c.verification.Insurers(ctx.Request.Context(), ctx.Query("search"))
	respond(ctx, http.StatusOK, view, err)
}

// Banks godoc
//
//	@Summary		Banks
//	@Description	Search by name, short name or the four-digit IBAN code.
//	@Tags			Provider verification
//	@Produce		json
//	@Param			search	query		string	false	"Name, short name or code"
//	@Success		200		{object}	dtos.CatalogList[dtos.BankView]
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/catalogs/banks [get]
func (c *Controller) Banks(ctx *gin.Context) {
	view, err := c.verification.Banks(ctx.Request.Context(), ctx.Query("search"))
	respond(ctx, http.StatusOK, view, err)
}

// WorkshopServices godoc
//
//	@Summary	Workshop services
//	@Tags		Provider verification
//	@Produce	json
//	@Success	200	{object}	dtos.CatalogList[dtos.WorkshopServiceView]
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/catalogs/workshop-services [get]
func (c *Controller) WorkshopServices(ctx *gin.Context) {
	view, err := c.verification.WorkshopServices(ctx.Request.Context())
	respond(ctx, http.StatusOK, view, err)
}

// DrivingCategories godoc
//
//	@Summary	Driving license categories
//	@Tags		Provider verification
//	@Produce	json
//	@Success	200	{object}	dtos.CatalogList[dtos.OptionView]
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/catalogs/driving-categories [get]
func (c *Controller) DrivingCategories(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, c.verification.DrivingCategories())
}

// CompanyTypes godoc
//
//	@Summary	Company types
//	@Tags		Provider verification
//	@Produce	json
//	@Success	200	{object}	dtos.CatalogList[dtos.OptionView]
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/catalogs/company-types [get]
func (c *Controller) CompanyTypes(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, c.verification.CompanyTypes())
}

// AdminView godoc
//
//	@Summary		Verification of a provider (backoffice)
//	@Description	Progress, review and every document with its data, flags and short-lived links to the files. Every call is recorded in the audit log.
//	@Tags			Backoffice providers
//	@Produce		json
//	@Param			id	path		string	true	"User id of the provider"
//	@Success		200	{object}	verification.AdminVerificationView
//	@Failure		403	{object}	shared.ErrorResponse	"Missing providers.read"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id}/verification [get]
func (c *Controller) AdminView(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "PROVIDER_NOT_FOUND", "Provider not found.")
		return
	}
	view, err := c.verification.AdminView(ctx.Request.Context(), actor, id)
	respond(ctx, http.StatusOK, view, err)
}
