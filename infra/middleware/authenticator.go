package middleware

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Authenticator struct {
	tokens *authjwt.Service
}

func NewAuthenticator(tokens *authjwt.Service) *Authenticator {
	return &Authenticator{tokens: tokens}
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

		authenticated := WithAuthenticatedUser(ctx.Request.Context(), userID, claims.SessionID, claims.Role)
		ctx.Request = ctx.Request.WithContext(authenticated)
		ctx.Next()
	}
}

func (a *Authenticator) RequireRole(roles ...enums.Role) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		role, ok := RoleFromContext(ctx.Request.Context())
		if !ok {
			shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
			return
		}
		if !slices.Contains(roles, role) {
			shared.Forbidden(ctx, "INSUFFICIENT_ROLE", "You do not have permission to perform this action.")
			return
		}
		ctx.Next()
	}
}
