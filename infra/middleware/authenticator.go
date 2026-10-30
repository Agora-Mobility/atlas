package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/tenant"
)

type Authenticator struct {
	tokens  *authjwt.Service
	revoked services.ISessionRevocationStore
}

func NewAuthenticator(tokens *authjwt.Service, revoked services.ISessionRevocationStore) *Authenticator {
	return &Authenticator{tokens: tokens, revoked: revoked}
}

func (a *Authenticator) RequireAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, found := strings.CutPrefix(ctx.GetHeader("Authorization"), "Bearer ")
		if !found || token == "" {
			shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
			return
		}

		claims, err := a.tokens.Verify(token)
		if err != nil {
			shared.Unauthorized(ctx, "INVALID_TOKEN", "Invalid or expired access token.")
			return
		}
		userID, err := claims.UserID()
		if err != nil {
			shared.Unauthorized(ctx, "INVALID_TOKEN", "Invalid or expired access token.")
			return
		}

		client, ok := ClientFromContext(ctx.Request.Context())
		if !ok {
			shared.BadRequest(ctx, "MISSING_CLIENT", "The X-Client-Id header is required.")
			return
		}
		if claims.ClientID != client.ClientID || !client.Allows(claims.Role) {
			shared.Forbidden(ctx, "TOKEN_CLIENT_MISMATCH", "Token was not issued for this client.")
			return
		}
		if claims.Role == enums.RoleStaff {
			if scope, ok := client.StaffScope(); !ok || scope != claims.Scope {
				shared.Forbidden(ctx, "TOKEN_CLIENT_MISMATCH", "Token was not issued for this client.")
				return
			}
		}

		// Fail closed: without the revocation list a logged-out or blocked
		// session could still be used until its token expires.
		revoked, err := a.revoked.IsSessionRevoked(ctx.Request.Context(), claims.SessionID)
		if err != nil {
			logger.FromContext(ctx.Request.Context()).Error("session revocation check failed", zap.Error(err))
			shared.ServiceUnavailable(ctx, "AUTH_UNAVAILABLE", "Authentication is temporarily unavailable.")
			return
		}
		if revoked {
			shared.Unauthorized(ctx, "SESSION_REVOKED", "Session ended. Please sign in again.")
			return
		}

		authenticated := WithAuthenticatedUser(ctx.Request.Context(), userID, claims.SessionID, claims.Role)
		if claims.MembershipID != nil && claims.OrganizationID != nil {
			authenticated = WithTokenMembership(authenticated, TokenMembership{
				MembershipID: *claims.MembershipID, OrganizationID: *claims.OrganizationID, Scope: claims.Scope,
			})
		}
		ctx.Request = ctx.Request.WithContext(authenticated)
		ctx.Next()
	}
}

type Authorizer struct {
	resolver services.IAccessResolver
}

func NewAuthorizer(resolver services.IAccessResolver) *Authorizer {
	return &Authorizer{resolver: resolver}
}

func (a *Authorizer) RequireMembership() gin.HandlerFunc {
	return a.RequirePermission()
}

func (a *Authorizer) RequirePermission(permissions ...entities.Permission) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		reqCtx := ctx.Request.Context()
		membership, ok := TokenMembershipFromContext(reqCtx)
		if !ok {
			logPermissionDenied(ctx, permissions, "no membership")
			shared.Forbidden(ctx, CodePermissionDenied, "You do not have permission to perform this action.")
			return
		}
		reqCtx = tenant.WithOrganization(reqCtx, membership.OrganizationID)
		if userID, ok := UserIDFromContext(reqCtx); ok {
			reqCtx = tenant.WithUser(reqCtx, userID)
		}

		access, err := a.resolver.Resolve(reqCtx, membership.MembershipID)
		if err != nil {
			logger.FromContext(reqCtx).Error("access resolution failed", zap.Error(err))
			shared.ServiceUnavailable(ctx, "AUTH_UNAVAILABLE", "Authentication is temporarily unavailable.")
			return
		}
		if access == nil || !access.IsActive() || access.OrganizationID != membership.OrganizationID {
			shared.Unauthorized(ctx, "SESSION_REVOKED", "Session ended. Please sign in again.")
			return
		}
		if !access.HasAll(permissions) {
			logPermissionDenied(ctx, permissions, "missing permission")
			shared.Forbidden(ctx, CodePermissionDenied, "You do not have permission to perform this action.")
			return
		}

		ctx.Request = ctx.Request.WithContext(WithAccess(reqCtx, access))
		ctx.Next()
	}
}

const CodePermissionDenied = "PERMISSION_DENIED"

func logPermissionDenied(ctx *gin.Context, permissions []entities.Permission, reason string) {
	required := make([]string, 0, len(permissions))
	for _, p := range permissions {
		required = append(required, string(p))
	}
	logger.FromContext(ctx.Request.Context()).Warn("permission denied",
		zap.String("path", ctx.FullPath()), zap.Strings("required", required), zap.String("reason", reason))
}
