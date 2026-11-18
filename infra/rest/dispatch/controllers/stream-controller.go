package controllers

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
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
//	@Description	Server-Sent Events of the signed-in customer or provider. Send Last-Event-ID to replay what was missed. Offers carry expires_in_s, relative to the moment of delivery. When the cursor is older than the mailbox a resync event asks the app to reload through GET /api/v1/requests/active. request.position carries lat, lng, heading and eta_s and, when known, next_in_s (seconds until the next point), speed_mps and points (the earlier points of the batch, oldest first, with dt_ms before the last one) so the app can move the car continuously (ADR 0023).
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

// StaffStream godoc
//
//	@Summary		Real-time alerts of the backoffice
//	@Description	Server-Sent Events for the team, with requests:read. staff.request_alert arrives when a request goes to the operator queue or a customer or provider raises an SOS (reason sos), with request_id, customer_id, category and reason. staff.ticket arrives when a support ticket is opened, with ticket_id, code, user_id, subject and request_id, only for whoever also has requests:manage. Every connected member gets every alert. Send Last-Event-ID to replay what was missed.
//	@Tags			Real time
//	@Produce		text/event-stream
//	@Param			Last-Event-ID	header		string	false	"Id of the last event received"
//	@Success		200				{string}	string	"Event stream"
//	@Failure		401				{object}	shared.ErrorResponse
//	@Failure		403				{object}	shared.ErrorResponse	"Missing requests:read"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/admin/events [get]
func (c *StreamController) StaffStream(ctx *gin.Context) {
	reqCtx := ctx.Request.Context()
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return
	}
	readsTickets := access.Has(entities.PermRequestsManage)
	session := sse.Session{
		UserID: services.StaffAlertsBox, LastEventID: ctx.GetHeader("Last-Event-ID"), TokenExpiresAt: middleware.TokenExpiryFromContext(reqCtx),
		Allow: func(eventType string) bool {
			return eventType != services.StaffTicket || readsTickets
		},
	}
	if err := c.streamer.Serve(reqCtx, ctx.Writer, session); err != nil && reqCtx.Err() == nil {
		logger.FromContext(reqCtx).Warn("staff event stream ended", zap.Error(err))
	}
}
