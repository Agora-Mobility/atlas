package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared"
)

func Recovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.FromContext(ctx.Request.Context()).Error("panic recovered", zap.Any("panic", r), zap.Stack("stack"))
				shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", fmt.Errorf("panic: %v", r))
			}
		}()
		ctx.Next()
	}
}
