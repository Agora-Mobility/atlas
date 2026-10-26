package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

// Recovery turns a panic into a logged 500 with the standard error body.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				ctx := c.Request.Context()
				logger.Error(ctx, "panic recovered", zap.Any("panic", r), zap.Stack("stack"))
				appErr := customerrors.Internal(fmt.Errorf("panic: %v", r))
				c.AbortWithStatusJSON(http.StatusInternalServerError,
					customerrors.NewResponse(appErr, logger.CorrelationID(ctx)))
			}
		}()
		c.Next()
	}
}
