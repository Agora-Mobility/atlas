package experience

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func signedInUser(ctx *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	return userID, true
}

func ticketParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "id", "TICKET_NOT_FOUND", "Ticket not found.")
}

// OpenRequestTicket godoc
//
//	@Summary		Report a problem with a request
//	@Description	Only by the customer or the provider of the request.
//	@Tags			Support
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.OpenTicketInput	true	"Problem"
//	@Success		201		{object}	dtos.TicketDetail
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/tickets [post]
func (c *Controller) OpenRequestTicket(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	requestID, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.OpenTicketInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.OpenTicket(ctx.Request.Context(), userID, &requestID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// OpenTicket godoc
//
//	@Summary	Talk to the support
//	@Tags		Support
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.OpenTicketInput	true	"Subject and message"
//	@Success	201		{object}	dtos.TicketDetail
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/support/tickets [post]
func (c *Controller) OpenTicket(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	var in dtos.OpenTicketInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.OpenTicket(ctx.Request.Context(), userID, nil, in)
	respond(ctx, http.StatusCreated, view, err)
}

// MyTickets godoc
//
//	@Summary	My support tickets
//	@Tags		Support
//	@Produce	json
//	@Param		page	query		int	false	"Page"
//	@Param		size	query		int	false	"Page size"
//	@Success	200		{object}	dtos.TicketList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/support/tickets [get]
func (c *Controller) MyTickets(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	list, err := c.uc.MyTickets(ctx.Request.Context(), userID, page)
	respond(ctx, http.StatusOK, list, err)
}

// MyTicket godoc
//
//	@Summary	A support ticket of mine
//	@Tags		Support
//	@Produce	json
//	@Param		id	path		string	true	"Ticket id"
//	@Success	200	{object}	dtos.TicketDetail
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/support/tickets/{id} [get]
func (c *Controller) MyTicket(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	id, ok := ticketParam(ctx)
	if !ok {
		return
	}
	view, err := c.uc.MyTicket(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// ReplyTicket godoc
//
//	@Summary	Write in a support ticket
//	@Tags		Support
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string					true	"Ticket id"
//	@Param		body	body		dtos.TicketReplyInput	true	"Message"
//	@Success	200		{object}	dtos.TicketDetail
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	409		{object}	shared.ErrorResponse	"Closed"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/support/tickets/{id}/messages [post]
func (c *Controller) ReplyTicket(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	id, ok := ticketParam(ctx)
	if !ok {
		return
	}
	var in dtos.TicketReplyInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.ReplyAsUser(ctx.Request.Context(), userID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AdminTickets godoc
//
//	@Summary	Support tickets
//	@Tags		Backoffice
//	@Produce	json
//	@Param		status	query		string	false	"open, answered or closed"
//	@Param		page	query		int		false	"Page"
//	@Param		size	query		int		false	"Page size"
//	@Success	200		{object}	dtos.TicketList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/support/tickets [get]
func (c *Controller) AdminTickets(ctx *gin.Context) {
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	list, err := c.uc.AdminTickets(ctx.Request.Context(), ctx.Query("status"), page)
	respond(ctx, http.StatusOK, list, err)
}

// AdminTicket godoc
//
//	@Summary	A support ticket
//	@Tags		Backoffice
//	@Produce	json
//	@Param		id	path		string	true	"Ticket id"
//	@Success	200	{object}	dtos.TicketDetail
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/support/tickets/{id} [get]
func (c *Controller) AdminTicket(ctx *gin.Context) {
	id, ok := ticketParam(ctx)
	if !ok {
		return
	}
	view, err := c.uc.AdminTicket(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, view, err)
}

// AdminReplyTicket godoc
//
//	@Summary	Answer a support ticket
//	@Tags		Backoffice
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string					true	"Ticket id"
//	@Param		body	body		dtos.TicketReplyInput	true	"Message"
//	@Success	200		{object}	dtos.TicketDetail
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/support/tickets/{id}/messages [post]
func (c *Controller) AdminReplyTicket(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := ticketParam(ctx)
	if !ok {
		return
	}
	var in dtos.TicketReplyInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.ReplyAsStaff(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AdminCloseTicket godoc
//
//	@Summary	Close a support ticket
//	@Tags		Backoffice
//	@Produce	json
//	@Param		id	path		string	true	"Ticket id"
//	@Success	200	{object}	dtos.TicketDetail
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/support/tickets/{id}/close [post]
func (c *Controller) AdminCloseTicket(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := ticketParam(ctx)
	if !ok {
		return
	}
	view, err := c.uc.CloseTicket(ctx.Request.Context(), actor, id)
	respond(ctx, http.StatusOK, view, err)
}
