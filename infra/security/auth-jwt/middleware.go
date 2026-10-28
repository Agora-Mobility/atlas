package authjwt

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const claimsKey = "auth.claims"

// RequireAuth rejects requests without a valid Bearer token and stores the
// claims in the Gin context.
func RequireAuth(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, found := strings.CutPrefix(header, "Bearer ")
		if !found || token == "" {
			shared.Unauthorized(c, shared.CodeUnauthorized, "Authentication required.")
			return
		}
		claims, err := svc.Verify(token)
		if err != nil {
			shared.Unauthorized(c, "INVALID_TOKEN", "Invalid or expired access token.")
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}

// RequireRole allows only the given roles. Use it after RequireAuth.
func RequireRole(roles ...enums.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := ClaimsFrom(c)
		if !ok {
			shared.Unauthorized(c, shared.CodeUnauthorized, "Authentication required.")
			return
		}
		for _, r := range roles {
			if claims.Role == r {
				c.Next()
				return
			}
		}
		shared.Forbidden(c, "INSUFFICIENT_ROLE", "You do not have permission to perform this action.")
	}
}

// ClaimsFrom returns the claims stored by RequireAuth.
func ClaimsFrom(c *gin.Context) (*Claims, bool) {
	v, ok := c.Get(claimsKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*Claims)
	return claims, ok
}
