package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

func handleAccessError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrMemberNotFound):
		shared.NotFound(ctx, "MEMBER_NOT_FOUND", "Member not found.")
	case errors.Is(err, usecases.ErrOrganizationNotFound):
		shared.NotFound(ctx, "ORGANIZATION_NOT_FOUND", "Organization not found.")
	case errors.Is(err, usecases.ErrRoleNotFound):
		shared.NotFound(ctx, "ROLE_NOT_FOUND", "Role not found.")
	case errors.Is(err, usecases.ErrRoleNotAssignable):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "ROLE_NOT_ASSIGNABLE", "The role belongs to another scope and cannot be assigned here.")
	case errors.Is(err, usecases.ErrRolesRequired):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "ROLES_REQUIRED", "Select at least one role.")
	case errors.Is(err, usecases.ErrPrivilegeEscalation):
		shared.Forbidden(ctx, "PRIVILEGE_ESCALATION", "You cannot grant or manage permissions you do not have.")
	case errors.Is(err, usecases.ErrCannotManageSelf):
		shared.Forbidden(ctx, "CANNOT_MANAGE_SELF", "You cannot perform this action on your own account.")
	case errors.Is(err, usecases.ErrLastOwner):
		shared.Conflict(ctx, "LAST_OWNER", "The organization must keep at least one active owner.")
	case errors.Is(err, usecases.ErrInvalidMemberState):
		shared.Conflict(ctx, "INVALID_MEMBER_STATE", "The member is not in a state that allows this action.")
	case errors.Is(err, usecases.ErrAlreadyMember):
		shared.Conflict(ctx, "ALREADY_MEMBER", "This person is already a member of the organization.")
	case errors.Is(err, usecases.ErrIdentityInUse):
		shared.Conflict(ctx, "IDENTITY_IN_USE", "The e-mail or phone belongs to a customer or provider account.")
	case errors.Is(err, usecases.ErrIdentityConflict):
		shared.Conflict(ctx, "IDENTITY_CONFLICT", "The e-mail and the phone belong to different people.")
	case errors.Is(err, usecases.ErrNIFAlreadyExists):
		shared.Conflict(ctx, "NIF_ALREADY_EXISTS", "Another organization already has this NIF.")
	case errors.Is(err, usecases.ErrInvalidInvitation):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_INVITATION", "The invitation is invalid or has expired.")
	case errors.Is(err, usecases.ErrInvalidMFAToken):
		shared.Unauthorized(ctx, "INVALID_MFA_TOKEN", "The MFA step expired. Sign in again.")
	case errors.Is(err, usecases.ErrInvalidMFACode):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_MFA_CODE", "The authenticator code is incorrect.")
	case errors.Is(err, usecases.ErrMFASetupRequired):
		shared.Conflict(ctx, "MFA_SETUP_REQUIRED", "Set up the authenticator app first.")
	case errors.Is(err, usecases.ErrMFAAlreadyConfigured):
		shared.Conflict(ctx, "MFA_ALREADY_CONFIGURED", "The authenticator app is already set up.")
	case errors.Is(err, usecases.ErrMFANotConfigured):
		shared.Conflict(ctx, "MFA_NOT_CONFIGURED", "The authenticator app is not set up.")
	case errors.Is(err, usecases.ErrMFAResetNotAllowed):
		shared.Forbidden(ctx, "MFA_RESET_NOT_ALLOWED", "This person belongs to other organizations: ask AGORA to reset the authenticator.")
	case errors.Is(err, usecases.ErrUnknownPermission):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "UNKNOWN_PERMISSION", "One or more permissions do not exist in this scope.")
	case errors.Is(err, usecases.ErrSystemRole):
		shared.Conflict(ctx, "SYSTEM_ROLE_IMMUTABLE", "System roles cannot be changed or deleted.")
	case errors.Is(err, usecases.ErrRoleInUse):
		shared.Conflict(ctx, "ROLE_IN_USE", "The role is still assigned to members.")
	case errors.Is(err, usecases.ErrRoleCodeTaken):
		shared.Conflict(ctx, "ROLE_CODE_TAKEN", "Another role already uses this code.")
	case errors.Is(err, usecases.ErrCodeRecentlySent):
		shared.TooManyRequests(ctx, "CODE_RECENTLY_SENT", "A code was sent recently. Wait before asking again.")
	default:
		handleAccountError(ctx, err)
	}
}

func staffActor(ctx *gin.Context) (usecases.StaffActor, bool) {
	actor, ok := requestActor(ctx)
	if !ok {
		return usecases.StaffActor{}, false
	}
	access, ok := middleware.AccessFromContext(ctx.Request.Context())
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return usecases.StaffActor{}, false
	}
	actor.OrganizationID = &access.OrganizationID
	return usecases.StaffActor{Access: access, Audit: actor}, true
}
