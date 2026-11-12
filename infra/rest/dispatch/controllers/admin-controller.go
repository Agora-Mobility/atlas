package controllers

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"

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

// ListDemandPricing godoc
//
//	@Summary		List the demand pricing of each zone and category
//	@Description	The price rises in an area of the zone when more customers wait than providers are free, by tiers of the ratio (customers + 1) / (free providers + 1), up to the cap. Each customer counts once, whether they quoted, requested or both. Ratios and factors are basis points: 10000 is 1.0.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Success		200	{object}	dtos.DemandPricingList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/demand-pricing [get]
func (c *DispatchController) ListDemandPricing(ctx *gin.Context) {
	list, err := c.dispatch.ListDemandPricing(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// SaveDemandPricing godoc
//
//	@Summary		Set the demand tiers, cap and switch of a zone and category
//	@Description	A tier applies when the smoothed ratio is above above_ratio_bps. Mode off stops measuring and brings every price of the zone back to normal at once; observe measures and records the factor that would apply, without changing prices, to calibrate the tiers; on charges it. Estimates already given keep their price.
//	@Tags			Backoffice dispatch
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.DemandPricingInput	true	"Demand pricing"
//	@Success		200		{object}	dtos.DemandPricingView
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/demand-pricing [put]
func (c *DispatchController) SaveDemandPricing(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.DemandPricingInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.SaveDemandPricing(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusOK, view, err)
}

// DemandAreas godoc
//
//	@Summary		Current demand factor by area
//	@Description	Areas of the zone where demand is above normal now, with the smoothed ratio and the factor quotes use. Areas not listed price at 1.0.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			zone_id		query		string	true	"Zone"
//	@Param			category	query		string	true	"Category slug"
//	@Success		200			{object}	dtos.DemandAreaList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/demand-pricing/areas [get]
func (c *DispatchController) DemandAreas(ctx *gin.Context) {
	zoneID, category, ok := demandScope(ctx)
	if !ok {
		return
	}
	list, err := c.dispatch.DemandAreas(ctx.Request.Context(), zoneID, category)
	respond(ctx, http.StatusOK, list, err)
}

// DemandHistory godoc
//
//	@Summary		Demand factor history
//	@Description	One row per area and minute while demand was above normal. Defaults to the last 24 hours.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			zone_id		query		string	true	"Zone"
//	@Param			category	query		string	true	"Category slug"
//	@Param			from		query		string	false	"From, YYYY-MM-DD or RFC 3339"
//	@Success		200			{object}	dtos.DemandHistory
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/demand-pricing/history [get]
func (c *DispatchController) DemandHistory(ctx *gin.Context) {
	zoneID, category, ok := demandScope(ctx)
	if !ok {
		return
	}
	from, ok := queryTime(ctx, "from")
	if !ok {
		return
	}
	history, err := c.dispatch.DemandHistory(ctx.Request.Context(), zoneID, category, from)
	respond(ctx, http.StatusOK, history, err)
}

func demandScope(ctx *gin.Context) (uuid.UUID, string, bool) {
	zoneID, ok := queryUUID(ctx, "zone_id")
	if !ok {
		return uuid.Nil, "", false
	}
	category := ctx.Query("category")
	if zoneID == nil || category == "" {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "zone_id and category are required.")
		return uuid.Nil, "", false
	}
	return *zoneID, category, true
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
//	@Description	Resolution 8 hexagons for the Google Maps map of the backoffice. Demand counts the requests created in the period, by pickup cell; the period defaults to the last 24 hours and spans at most 31 days. Supply is always the providers online right now, whatever the period.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			zone_id		query		string	false	"Zone id"
//	@Param			category	query		string	false	"Category slug"
//	@Param			from		query		string	false	"Start of the period (RFC 3339)"
//	@Param			to			query		string	false	"End of the period (RFC 3339), default now"
//	@Success		200			{object}	dtos.HeatMap
//	@Failure		400			{object}	shared.ErrorResponse
//	@Failure		422			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/dispatch/heatmap [get]
func (c *DispatchController) HeatMap(ctx *gin.Context) {
	query, ok := heatQuery(ctx)
	if !ok {
		return
	}
	heat, err := c.dispatch.HeatMap(ctx.Request.Context(), query)
	respond(ctx, http.StatusOK, heat, err)
}

// Flows godoc
//
//	@Summary		Origin to destination flows by H3 cell
//	@Description	Requests with a destination created in the period, grouped by pickup and destination resolution 8 cells and sorted by volume. Pairs below min_requests (default 3) and pairs beyond limit (default 500) are left out and counted in hidden, so a single trip is not shown. The period defaults to the last 24 hours and spans at most 31 days.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			zone_id			query		string	false	"Zone id"
//	@Param			category		query		string	false	"Category slug"
//	@Param			from			query		string	false	"Start of the period (RFC 3339)"
//	@Param			to				query		string	false	"End of the period (RFC 3339), default now"
//	@Param			min_requests	query		int		false	"Smallest pair shown, 1 to 1000"
//	@Param			limit			query		int		false	"Most pairs returned, 1 to 2000"
//	@Success		200				{object}	dtos.FlowMap
//	@Failure		400				{object}	shared.ErrorResponse
//	@Failure		422				{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/dispatch/flows [get]
func (c *DispatchController) Flows(ctx *gin.Context) {
	query, ok := heatQuery(ctx)
	if !ok {
		return
	}
	flows := dtos.FlowQuery{HeatQuery: query}
	for _, item := range []struct {
		name string
		into *int
	}{{"min_requests", &flows.MinRequests}, {"limit", &flows.Limit}} {
		raw := ctx.Query(item.name)
		if raw == "" {
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+item.name+".")
			return
		}
		*item.into = parsed
	}
	view, err := c.dispatch.Flows(ctx.Request.Context(), flows)
	respond(ctx, http.StatusOK, view, err)
}

// HeatCells godoc
//
//	@Summary		Heat of the H3 cells inside the map view
//	@Description	Requests per H3 cell for a hexagon layer such as deck.gl H3HexagonLayer, read from the cells the heatmap job aggregates every few minutes in 15 minute windows. The zoom picks the resolution (by default up to 10 is 6, 11 and 12 are 7, 13 and 14 are 8, from 15 is 9). When the view holds more than 20000 cells the next coarser resolution is used, and resolution tells which one answered; only when even resolution 6 does not fit the answer is 400 AREA_TOO_LARGE. value is the smoothed heat (0.6 of the cell plus 0.4 of the mean of its six neighbors); count is the raw number of requests. Windows of the period are summed. The period defaults to the last complete window and spans at most 7 days. h3 is always the hexadecimal index, since 64 bit integers lose precision in JavaScript.
//	@Tags			Backoffice dispatch
//	@Produce		json
//	@Param			bbox		query		string	true	"minLng,minLat,maxLng,maxLat"	example(13.15,-8.95,13.35,-8.75)
//	@Param			zoom		query		int		true	"Map zoom, 0 to 24"
//	@Param			from		query		string	false	"Start of the period (RFC 3339)"
//	@Param			to			query		string	false	"End of the period (RFC 3339)"
//	@Param			metric		query		string	false	"count or value, default value"	Enums(count, value)
//	@Param			category	query		string	false	"Category slug, default all"
//	@Success		200			{object}	dtos.HeatCellsView
//	@Failure		400			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/dispatch/heatmap/cells [get]
func (c *DispatchController) HeatCells(ctx *gin.Context) {
	box, ok := bboxQuery(ctx)
	if !ok {
		return
	}
	zoom, err := strconv.Atoi(ctx.Query("zoom"))
	if err != nil {
		shared.BadRequest(ctx, "INVALID_ZOOM", "zoom is required and must be a whole number between 0 and 24.")
		return
	}
	from, to, ok := reportPeriod(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.HeatCells(ctx.Request.Context(), dtos.HeatCellsQuery{
		Box: box, Zoom: zoom, From: from, To: to, Metric: ctx.Query("metric"), Category: ctx.Query("category"),
	})
	respond(ctx, http.StatusOK, view, err)
}

func bboxQuery(ctx *gin.Context) (entities.GeoBox, bool) {
	parts := strings.Split(ctx.Query("bbox"), ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			break
		}
		values = append(values, value)
	}
	if len(parts) != 4 || len(values) != 4 {
		shared.BadRequest(ctx, "INVALID_BBOX", "bbox is required as minLng,minLat,maxLng,maxLat.")
		return entities.GeoBox{}, false
	}
	return entities.GeoBox{MinLng: values[0], MinLat: values[1], MaxLng: values[2], MaxLat: values[3]}, true
}

func heatQuery(ctx *gin.Context) (dtos.HeatQuery, bool) {
	query := dtos.HeatQuery{Category: ctx.Query("category")}
	if raw := ctx.Query("zone_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid zone_id.")
			return query, false
		}
		query.ZoneID = &parsed
	}
	from, to, ok := reportPeriod(ctx)
	query.From, query.To = from, to
	return query, ok
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
