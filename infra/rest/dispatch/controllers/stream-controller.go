package controllers

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/sse"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

type StreamController struct {
	streamer *sse.Streamer
	dispatch *dispatch.Usecase
}

func NewStreamController(streamer *sse.Streamer, uc *dispatch.Usecase) *StreamController {
	return &StreamController{streamer: streamer, dispatch: uc}
}

// Stream godoc
//
//	@Summary		Real-time events
//	@Description	Server-Sent Events of the signed-in customer or provider. Send Last-Event-ID to replay what was missed. Offers carry expires_in_s, relative to the moment of delivery. When the cursor is older than the mailbox a resync event asks the app to reload through GET /api/v1/requests/active.
//	@Tags			Real time
//	@Produce		text/event-stream
//	@Param			Last-Event-ID	header		string	false	"Id of the last event received"
//	@Success		200				{string}	string	"Event stream"
//	@Failure		401				{object}	shared.ErrorResponse
//	@Failure		403				{object}	shared.ErrorResponse	"Only app accounts have a stream"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/events [get]
func (c *StreamController) Stream(ctx *gin.Context) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	if !role.IsAppAccount() {
		shared.Forbidden(ctx, "APP_ACCOUNT_REQUIRED", "Only customer and provider accounts have an event stream.")
		return
	}
	session := sse.Session{
		UserID: userID.String(), LastEventID: ctx.GetHeader("Last-Event-ID"), TokenExpiresAt: middleware.TokenExpiryFromContext(reqCtx),
		ActiveRequestID: func(ctx context.Context) (string, error) {
			return c.dispatch.ActiveRequestIDFor(ctx, userID, role == enums.RoleProvider)
		},
	}
	if err := c.streamer.Serve(reqCtx, ctx.Writer, session); err != nil && reqCtx.Err() == nil {
		logger.FromContext(reqCtx).Warn("event stream ended", zap.Error(err))
	}
}
