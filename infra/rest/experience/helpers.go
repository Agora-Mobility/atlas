package experience

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
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
