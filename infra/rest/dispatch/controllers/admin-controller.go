package controllers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

type providerRequestAction func(ctx context.Context, providerID, requestID uuid.UUID) (*dtos.RequestView, error)

func staffActor(ctx *gin.Context) (entities.Actor, bool) {
	reqCtx := ctx.Request.Context()
	if _, ok := middleware.UserIDFromContext(reqCtx); !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return entities.Actor{}, false
	}
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return entities.Actor{}, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	return access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent()), true
}

// ListZones godoc
//
//	@Summary	List the service zones
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Success	200	{object}	dtos.ZoneList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/zones [get]
func (c *DispatchController) ListZones(ctx *gin.Context) {
	list, err := c.dispatch.ListZones(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// CreateZone godoc
//
//	@Summary		Create a service zone
//	@Description	Requires services:manage. The geometry is a GeoJSON Polygon or MultiPolygon. Each zone carries its currency and time zone.
//	@Tags			Backoffice dispatch
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.ZoneInput	true	"Zone"
//	@Success		201		{object}	dtos.ZoneView
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/zones [post]
func (c *DispatchController) CreateZone(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.ZoneInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.CreateZone(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusCreated, view, err)
}

// UpdateZone godoc
//
//	@Summary	Replace a service zone
//	@Tags		Backoffice dispatch
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string			true	"Zone id"
//	@Param		body	body		dtos.ZoneInput	true	"Zone"
//	@Success	200		{object}	dtos.ZoneView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/zones/{id} [put]
func (c *DispatchController) UpdateZone(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "ZONE_NOT_FOUND", "Zone not found.")
	if !ok {
		return
	}
	var in dtos.ZoneInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.UpdateZone(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// ListPolicies godoc
//
//	@Summary	List the dispatch policies
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Success	200	{object}	dtos.PolicyList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch-policies [get]
func (c *DispatchController) ListPolicies(ctx *gin.Context) {
	list, err := c.dispatch.ListPolicies(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// CreatePolicy godoc
//
//	@Summary		Create a policy for a category, optionally for a zone
//	@Description	The resolution and radii are refused when they would need too many H3 cells per search.
//	@Tags			Backoffice dispatch
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.PolicyInput	true	"Policy"
//	@Success		201		{object}	dtos.PolicyView
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/dispatch-policies [post]
func (c *DispatchController) CreatePolicy(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.PolicyInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.CreatePolicy(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusCreated, view, err)
}

// UpdatePolicy godoc
//
//	@Summary	Change a policy
//	@Tags		Backoffice dispatch
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Policy id"
//	@Param		body	body		dtos.PolicyInput	true	"Policy"
//	@Success	200		{object}	dtos.PolicyView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch-policies/{id} [put]
func (c *DispatchController) UpdatePolicy(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "POLICY_NOT_FOUND", "Policy not found.")
	if !ok {
		return
	}
	var in dtos.PolicyInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.UpdatePolicy(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// ListTariffs godoc
//
//	@Summary	List the current tariffs
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Success	200	{object}	dtos.TariffList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/tariffs [get]
func (c *DispatchController) ListTariffs(ctx *gin.Context) {
	list, err := c.dispatch.ListTariffs(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// PublishTariff godoc
//
//	@Summary		Publish a new tariff version
//	@Description	Tariffs are never edited: this closes the current version and creates another, so the history explains every price. Values are integers in the smallest unit of the currency.
//	@Tags			Backoffice dispatch
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.TariffInput	true	"Tariff"
//	@Success		201		{object}	dtos.TariffView
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/tariffs [post]
func (c *DispatchController) PublishTariff(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.TariffInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.PublishTariff(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusCreated, view, err)
}

// OperatorQueue godoc
//
//	@Summary	Requests waiting for an operator
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Param		page	query		int	false	"Page"
//	@Param		size	query		int	false	"Page size"
//	@Success	200		{object}	dtos.RequestList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch/queue [get]
func (c *DispatchController) OperatorQueue(ctx *gin.Context) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	list, err := c.dispatch.OperatorQueue(ctx.Request.Context(), page)
	respond(ctx, http.StatusOK, list, err)
}

// HeatMap godoc
//
//	@Summary		Supply and demand by H3 cell
//	@Description	Resolution 8 hexagons for the Mapbox map of the backoffice. Demand covers the last 24 hours.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			zone_id	query		string	false	"Zone id"
//	@Success		200		{object}	dtos.HeatMap
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/dispatch/heatmap [get]
func (c *DispatchController) HeatMap(ctx *gin.Context) {
	var zoneID *uuid.UUID
	if raw := ctx.Query("zone_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid zone_id.")
			return
		}
		zoneID = &parsed
	}
	heat, err := c.dispatch.HeatMap(ctx.Request.Context(), zoneID)
	respond(ctx, http.StatusOK, heat, err)
}

// OperatorGetRequest godoc
//
//	@Summary	Get any request
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch/requests/{id} [get]
func (c *DispatchController) OperatorGetRequest(ctx *gin.Context) {
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.GetRequestAsOperator(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, view, err)
}

// AssignManually godoc
//
//	@Summary	Dispatch a request by hand
//	@Tags		Backoffice dispatch
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Request id"
//	@Param		body	body		dtos.AssignInput	true	"Provider"
//	@Success	200		{object}	dtos.RequestView
//	@Failure	409		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch/requests/{id}/assign [post]
func (c *DispatchController) AssignManually(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.AssignInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.AssignManually(ctx.Request.Context(), actor, id, in.ProviderID)
	respond(ctx, http.StatusOK, view, err)
}

// RetrySearch godoc
//
//	@Summary	Search again for a queued request
//	@Tags		Backoffice dispatch
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	409	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch/requests/{id}/retry [post]
func (c *DispatchController) RetrySearch(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.RetrySearch(ctx.Request.Context(), actor, id)
	respond(ctx, http.StatusOK, view, err)
}

// OperatorCancel godoc
//
//	@Summary	Cancel a request as an operator
//	@Tags		Backoffice dispatch
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string						true	"Request id"
//	@Param		body	body		dtos.OperatorCancelInput	true	"Reason"
//	@Success	200		{object}	dtos.RequestView
//	@Failure	409		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/dispatch/requests/{id}/cancel [post]
func (c *DispatchController) OperatorCancel(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.OperatorCancelInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.CancelByOperator(ctx.Request.Context(), actor, id, in.Reason)
	respond(ctx, http.StatusOK, view, err)
}
