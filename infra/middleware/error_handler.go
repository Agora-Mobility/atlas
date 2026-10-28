package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

// ErrorHandler renders the last error added with c.Error as the standard
// JSON error body. Controllers call c.Error(err) and return.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}

		ctx := c.Request.Context()
		appErr := customerrors.As(c.Errors.Last().Err)
		if appErr.Status() >= 500 {
			logger.FromContext(ctx).Error("request failed", zap.Error(appErr))
		}
		c.JSON(appErr.Status(), customerrors.NewResponse(appErr, logger.CorrelationID(ctx)))
	}
}
