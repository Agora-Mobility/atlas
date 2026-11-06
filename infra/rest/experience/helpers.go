package experience

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/experience"
)

type mappedError struct {
	target  error
	status  int
	code    string
	message string
}

var errorTable = []mappedError{
	{experience.ErrRequestNotFound, http.StatusNotFound, "REQUEST_NOT_FOUND", "Request not found."},
	{experience.ErrUnknownKind, http.StatusNotFound, "CATALOG_NOT_FOUND", "This catalog does not exist."},
	{experience.ErrKindNotPublic, http.StatusNotFound, "CATALOG_NOT_FOUND", "This catalog does not exist."},
	{entities.ErrCatalogItemNotFound, http.StatusNotFound, "CATALOG_ITEM_NOT_FOUND", "Catalog item not found."},
	{repository.ErrCatalogCodeTaken, http.StatusConflict, "CATALOG_CODE_TAKEN", "This code already exists."},
	{entities.ErrVoucherNotFound, http.StatusNotFound, "VOUCHER_NOT_FOUND", "Voucher not found."},
	{repository.ErrVoucherCodeTaken, http.StatusConflict, "VOUCHER_CODE_TAKEN", "This voucher code already exists."},
	{entities.ErrTicketNotFound, http.StatusNotFound, "TICKET_NOT_FOUND", "Ticket not found."},
	{entities.ErrTicketClosed, http.StatusConflict, "TICKET_CLOSED", "This ticket is closed."},
	{entities.ErrCustomerVehicleNotFound, http.StatusNotFound, "VEHICLE_NOT_FOUND", "Vehicle not found."},
	{repository.ErrCustomerPlateTaken, http.StatusConflict, "PLATE_TAKEN", "This plate is already in your vehicles."},
	{entities.ErrPhotoTypeNotAllowed, http.StatusUnprocessableEntity, "PHOTO_TYPE_NOT_ALLOWED", "This type of file is not allowed."},
	{entities.ErrPhotoTooLarge, http.StatusUnprocessableEntity, "PHOTO_TOO_LARGE", "The file is too large."},
	{entities.ErrPhotoLimit, http.StatusUnprocessableEntity, "PHOTO_LIMIT", "Too many photos."},
	{entities.ErrPhotoNotFound, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found."},
	{entities.ErrPhotoNotUploaded, http.StatusUnprocessableEntity, "PHOTO_NOT_UPLOADED", "The photo was not uploaded or does not match what was announced."},
	{services.ErrStorageUnavailable, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "File storage is not available."},
	{entities.ErrInboxItemNotFound, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "Notification not found."},
	{entities.ErrReferralUnknown, http.StatusNotFound, "REFERRAL_NOT_FOUND", "This invitation code does not exist."},
	{entities.ErrReferralOwnCode, http.StatusUnprocessableEntity, "REFERRAL_OWN_CODE", "You cannot use your own code."},
	{entities.ErrReferralUsed, http.StatusConflict, "REFERRAL_ALREADY_USED", "An invitation code was already used on this account."},
	{entities.ErrReferralTooLate, http.StatusConflict, "REFERRAL_TOO_LATE", "An invitation only counts before the first paid request."},
	{entities.ErrRatingNotAllowed, http.StatusConflict, "RATING_NOT_ALLOWED", "Only a completed request can be rated."},
	{entities.ErrAlreadyRated, http.StatusConflict, "ALREADY_RATED", "This request was already rated."},
	{entities.ErrComplimentNotValid, http.StatusUnprocessableEntity, "COMPLIMENT_NOT_VALID", "A compliment does not exist or does not apply to this service."},
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	if body == nil {
		ctx.Status(status)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
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

func userWithRole(ctx *gin.Context, role enums.Role) (uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	current, _ := middleware.RoleFromContext(reqCtx)
	if current != role {
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
		return uuid.Nil, false
	}
	return userID, true
}

func staffActor(ctx *gin.Context) (entities.Actor, bool) {
	reqCtx := ctx.Request.Context()
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return entities.Actor{}, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	return access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent()), true
}

func idParam(ctx *gin.Context, name, code, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param(name))
	if err != nil {
		shared.NotFound(ctx, code, message)
		return uuid.Nil, false
	}
	return id, true
}

func requestParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "id", "REQUEST_NOT_FOUND", "Request not found.")
}

func pageParams(ctx *gin.Context) (pagination.Params, bool) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return page, false
	}
	return page, true
}
