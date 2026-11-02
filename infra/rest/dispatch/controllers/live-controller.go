package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func signedIn(ctx *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	return userID, true
}

// SendMessage godoc
//
//	@Summary		Send a chat message on a request
//	@Description	Customer or provider of the request, while it has a provider and is not finished. A quick reply replaces the text. The other side gets it by SSE as request.message. Phone numbers are never shared.
//	@Tags			Live request
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.SendMessageInput	true	"Message"
//	@Success		201		{object}	dtos.MessageView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"The chat is closed"
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/messages [post]
func (c *DispatchController) SendMessage(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.SendMessageInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.SendMessage(ctx.Request.Context(), userID, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// ListMessages godoc
//
//	@Summary		Chat of a request
//	@Description	Marks the messages of the other side as read.
//	@Tags			Live request
//	@Produce		json
//	@Param			id		path		string	true	"Request id"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.MessageList
//	@Failure		404		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/messages [get]
func (c *DispatchController) ListMessages(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	view, err := c.dispatch.Messages(ctx.Request.Context(), userID, id, page)
	respond(ctx, http.StatusOK, view, err)
}

// QuickReplies godoc
//
//	@Summary	Quick replies of the chat
//	@Tags		Live request
//	@Produce	json
//	@Success	200	{object}	dtos.QuickReplyList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/quick-replies [get]
func (c *DispatchController) QuickReplies(ctx *gin.Context) {
	role, _ := middleware.RoleFromContext(ctx.Request.Context())
	actor := "customer"
	if role == enums.RoleProvider {
		actor = "provider"
	}
	view, err := c.dispatch.QuickReplies(ctx.Request.Context(), actor)
	respond(ctx, http.StatusOK, view, err)
}

// ShareTrip godoc
//
//	@Summary		Share the trip with a temporary public link
//	@Description	Customer of an active request. The link shows the state, the provider and the position, never a phone number, and it expires.
//	@Tags			Live request
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		201	{object}	dtos.ShareView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse	"Already finished"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/share [post]
func (c *DispatchController) ShareTrip(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.ShareTrip(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusCreated, view, err)
}

// RevokeShares godoc
//
//	@Summary	Stop sharing the trip
//	@Tags		Live request
//	@Param		id	path	string	true	"Request id"
//	@Success	204
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/requests/{id}/share [delete]
func (c *DispatchController) RevokeShares(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.RevokeShares(ctx.Request.Context(), userID, id))
}

// PublicTrip godoc
//
//	@Summary		A shared trip
//	@Description	Public, the token is the permission. Without phone numbers, and with the position only while the trip is active.
//	@Tags			Live request
//	@Produce		json
//	@Param			token	path		string	true	"Token of the link"
//	@Success		200		{object}	dtos.PublicTrip
//	@Failure		404		{object}	shared.ErrorResponse	"Unknown, revoked or expired"
//	@Router			/api/v1/public/trips/{token} [get]
func (c *DispatchController) PublicTrip(ctx *gin.Context) {
	view, err := c.dispatch.PublicTrip(ctx.Request.Context(), ctx.Param("token"))
	respond(ctx, http.StatusOK, view, err)
}

// ETA godoc
//
//	@Summary		Time until the provider arrives
//	@Description	From the last position of the provider, by route. source tells whether it came from Mapbox or from the fallback.
//	@Tags			Live request
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.ETAView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse	"No position yet"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/eta [get]
func (c *DispatchController) ETA(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.ETA(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// Call godoc
//
//	@Summary		Call the other side with a masked number
//	@Description	The number of each side is not shared. Without a configured provider the answer is 503 CALLS_UNAVAILABLE.
//	@Tags			Live request
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.CallView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse
//	@Failure		503	{object}	shared.ErrorResponse	"No provider"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/call [post]
func (c *DispatchController) Call(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.Call(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// RaiseSOS godoc
//
//	@Summary		Raise an SOS on a request
//	@Description	Customer or provider of the request. The operator is alerted and the alert stays in the backoffice. A second press returns the open alert.
//	@Tags			Live request
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Request id"
//	@Param			body	body		dtos.SOSInput	true	"Position and note"
//	@Success		201		{object}	dtos.SOSView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/sos [post]
func (c *DispatchController) RaiseSOS(ctx *gin.Context) {
	userID, ok := signedIn(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.SOSInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, created, err := c.dispatch.RaiseSOS(ctx.Request.Context(), userID, id, in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(ctx, status, view, err)
}

// ListSOS godoc
//
//	@Summary	SOS alerts
//	@Tags		Backoffice
//	@Produce	json
//	@Param		status	query		string	false	"open or resolved"
//	@Param		page	query		int		false	"Page"
//	@Param		size	query		int		false	"Page size"
//	@Success	200		{object}	dtos.SOSList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/sos [get]
func (c *DispatchController) ListSOS(ctx *gin.Context) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	view, err := c.dispatch.ListSOS(ctx.Request.Context(), ctx.Query("status"), page)
	respond(ctx, http.StatusOK, view, err)
}

// ResolveSOS godoc
//
//	@Summary	Close an SOS alert
//	@Tags		Backoffice
//	@Param		id	path	string	true	"Alert id"
//	@Success	204
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/sos/{id}/resolve [post]
func (c *DispatchController) ResolveSOS(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "SOS_NOT_FOUND", "Alert not found.")
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.ResolveSOS(ctx.Request.Context(), actor, id))
}
