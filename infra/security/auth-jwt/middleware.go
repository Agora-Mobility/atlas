package authjwt

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/shared/customerrors"
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
			_ = c.Error(customerrors.Unauthorized("missing bearer token"))
			c.Abort()
			return
		}
		claims, err := svc.Verify(token)
		if err != nil {
			_ = c.Error(customerrors.New(customerrors.CodeUnauthorized, "invalid or expired token", err))
			c.Abort()
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
			_ = c.Error(customerrors.Unauthorized("authentication required"))
			c.Abort()
			return
		}
		for _, r := range roles {
			if claims.Role == r {
				c.Next()
				return
			}
		}
		_ = c.Error(customerrors.Forbidden("insufficient role"))
		c.Abort()
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
