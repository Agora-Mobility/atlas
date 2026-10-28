package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
)

func Streaming() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		err := http.NewResponseController(ctx.Writer).SetWriteDeadline(time.Time{})
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			logger.FromContext(ctx.Request.Context()).Warn("could not lift write deadline for stream", zap.Error(err))
		}
		ctx.Next()
	}
}
