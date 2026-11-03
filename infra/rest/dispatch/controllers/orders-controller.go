package controllers

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	defaultRecentDestinations = 10
	maxRecentDestinations     = 50
	maxStopPosition           = 100
)

// Receipt godoc
//
//	@Summary		Receipt of a completed request in PDF
//	@Description	Customer of the request. The values are the ones of the breakdown in GET /requests/{id}.
//	@Tags			Dispatch
//	@Produce		application/pdf
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{file}		file
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse	"Not completed"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/receipt [get]
func (c *DispatchController) Receipt(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	document, code, err := c.dispatch.Receipt(ctx.Request.Context(), userID, id)
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.Header("Content-Disposition", `attachment; filename="recibo-`+code+`.pdf"`)
	ctx.Data(http.StatusOK, "application/pdf", document)
}

// RepeatRequest godoc
//
//	@Summary		Repeat a request
//	@Description	Creates a new request with the same route and the current prices. Send {"preview": true} to only get the route and prices, without creating it.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Request id"
//	@Param			body	body		dtos.RepeatInput	false	"Repeat options"
//	@Success		201		{object}	dtos.RepeatView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"Duplicate or estimate already used"
//	@Failure		422		{object}	shared.ErrorResponse	"Service or route not available"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/repeat [post]
func (c *DispatchController) RepeatRequest(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.RepeatInput
	if err := ctx.ShouldBindJSON(&in); err != nil && !errors.Is(err, io.EOF) {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, _, err := c.dispatch.RepeatRequest(ctx.Request.Context(), customerID, id, ctx.GetHeader(middleware.HeaderIdempotencyKey), in)
	respond(ctx, http.StatusCreated, view, err)
}

// ArriveAtStop godoc
//
//	@Summary		Mark the arrival at a stop
//	@Description	Provider of the request, while it is in progress. The first time counts.
//	@Tags			Provider work
//	@Produce		json
//	@Param			id			path		string	true	"Request id"
//	@Param			position	path		int		true	"Position of the stop, from 0"
//	@Success		200			{object}	dtos.RequestView
//	@Failure		404			{object}	shared.ErrorResponse
//	@Failure		409			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/stops/{position}/arrive [post]
func (c *DispatchController) ArriveAtStop(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	position, err := strconv.Atoi(ctx.Param("position"))
	if err != nil || position < 0 || position > maxStopPosition {
		shared.NotFound(ctx, "STOP_NOT_FOUND", "This stop does not exist.")
		return
	}
	view, err := c.dispatch.ArriveAtStop(ctx.Request.Context(), providerID, id, position)
	respond(ctx, http.StatusOK, view, err)
}

// RecentDestinations godoc
//
//	@Summary	Last destinations of the customer, without repeats
//	@Tags		Dispatch
//	@Produce	json
//	@Param		limit	query		int	false	"Up to 50, 10 by default"
//	@Success	200		{object}	dtos.RecentDestinationList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/recent-destinations [get]
func (c *DispatchController) RecentDestinations(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	limit := defaultRecentDestinations
	if raw := ctx.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxRecentDestinations {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "limit must be between 1 and 50.")
			return
		}
		limit = parsed
	}
	view, err := c.dispatch.RecentDestinations(ctx.Request.Context(), customerID, limit)
	respond(ctx, http.StatusOK, view, err)
}
