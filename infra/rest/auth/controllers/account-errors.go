package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

func handleAccountError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrCurrentPasswordRequired):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "CURRENT_PASSWORD_REQUIRED", "Confirm with the current password.")
	case errors.Is(err, usecases.ErrConfirmationRequired):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Confirm with the code sent by SMS to the account phone.")
	case errors.Is(err, usecases.ErrInvalidCurrentPassword):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_CURRENT_PASSWORD", "Current password is incorrect.")
	case errors.Is(err, usecases.ErrEmailAlreadyExists):
		shared.Conflict(ctx, "EMAIL_ALREADY_EXISTS", "Email already registered.")
	case errors.Is(err, usecases.ErrPhoneAlreadyExists):
		shared.Conflict(ctx, "PHONE_ALREADY_EXISTS", "Phone already registered.")
	case errors.Is(err, usecases.ErrPhoneUnchanged):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "PHONE_UNCHANGED", "This is already the account phone number.")
	case errors.Is(err, usecases.ErrCodeRecentlySent):
		shared.TooManyRequests(ctx, "CODE_RECENTLY_SENT", "A code was sent recently. Wait before asking again.")
	case errors.Is(err, usecases.ErrAccountDeletionNotAllowed):
		shared.Forbidden(ctx, "ACCOUNT_DELETION_NOT_ALLOWED", "Staff accounts are removed by an admin.")
	case errors.Is(err, usecases.ErrCannotManageSelf):
		shared.Forbidden(ctx, "CANNOT_MANAGE_SELF", "You cannot perform this action on your own account.")
	case errors.Is(err, usecases.ErrReasonRequired):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "REASON_REQUIRED", "Select at least one reason.")
	case errors.Is(err, usecases.ErrUnknownReason):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "UNKNOWN_BLOCK_REASON", "One or more block reasons do not exist or are inactive.")
	case errors.Is(err, repository.ErrNotFound):
		shared.NotFound(ctx, "USER_NOT_FOUND", "User not found.")
	default:
		handleSignInError(ctx, err)
	}
}

func authenticatedUserID(ctx *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
	}
	return userID, ok
}

func requestActor(ctx *gin.Context) (entities.Actor, bool) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return entities.Actor{}, false
	}
	role, _ := middleware.RoleFromContext(ctx.Request.Context())
	return entities.Actor{ID: &userID, Role: role, IPAddress: ctx.ClientIP(), UserAgent: ctx.Request.UserAgent()}, true
}

func userIDParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.BadRequest(ctx, "INVALID_USER_ID", "The user id must be a UUID.")
		return uuid.Nil, false
	}
	return id, true
}
