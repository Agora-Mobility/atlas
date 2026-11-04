package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func queryUUID(ctx *gin.Context, name string) (*uuid.UUID, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+name+".")
		return nil, false
	}
	return &id, true
}

func queryTime(ctx *gin.Context, name string) (*time.Time, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return nil, true
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return &parsed, true
		}
	}
	shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+name+": use YYYY-MM-DD or RFC 3339.")
	return nil, false
}

func requestFilter(ctx *gin.Context) (*dtos.AdminRequestFilter, bool) {
	filter := &dtos.AdminRequestFilter{Category: ctx.Query("category")}
	if statuses := ctx.Query("status"); statuses != "" {
		filter.Statuses = strings.Split(statuses, ",")
	}
	var ok bool
	for name, target := range map[string]**uuid.UUID{
		"service_id": &filter.ServiceID, "zone_id": &filter.ZoneID, "organization_id": &filter.OrganizationID, "provider_id": &filter.ProviderID,
	} {
		if *target, ok = queryUUID(ctx, name); !ok {
			return nil, false
		}
	}
	if filter.From, ok = queryTime(ctx, "from"); !ok {
		return nil, false
	}
	if filter.To, ok = queryTime(ctx, "to"); !ok {
		return nil, false
	}
	return filter, true
}

func partnerOrganization(ctx *gin.Context) (uuid.UUID, bool) {
	access, ok := middleware.AccessFromContext(ctx.Request.Context())
	if !ok || access.Scope != enums.OrganizationTypePartner {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return uuid.Nil, false
	}
	return access.OrganizationID, true
}

func (c *DispatchController) listRequests(ctx *gin.Context, filter *dtos.AdminRequestFilter) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	list, err := c.dispatch.ListAdminRequests(ctx.Request.Context(), filter, page)
	respond(ctx, http.StatusOK, list, err)
}

// ListAdminRequests godoc
//
//	@Summary		List rides and service requests
//	@Description	Requires requests:read. Every category is here; filter by category to see one segment, such as taxi, towing, removals or maintenance. Newest first.
//	@Tags			Backoffice requests
//	@Produce		json
//	@Param			status			query		string	false	"One status or several separated by commas"
//	@Param			category		query		string	false	"Category slug"
//	@Param			service_id		query		string	false	"Service"
//	@Param			zone_id			query		string	false	"Zone"
//	@Param			organization_id	query		string	false	"Partner"
//	@Param			provider_id		query		string	false	"Provider"
//	@Param			from			query		string	false	"Created at or after, YYYY-MM-DD or RFC 3339"
//	@Param			to				query		string	false	"Created before, YYYY-MM-DD or RFC 3339"
//	@Param			page			query		int		false	"Page"
//	@Param			size			query		int		false	"Page size"
//	@Success		200				{object}	dtos.AdminRequestList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/requests [get]
func (c *DispatchController) ListAdminRequests(ctx *gin.Context) {
	filter, ok := requestFilter(ctx)
	if !ok {
		return
	}
	c.listRequests(ctx, filter)
}

// SummarizeAdminRequests godoc
//
//	@Summary		Rides and service requests grouped
//	@Description	Requires requests:read. Counts per status and the fare of completed requests, for each group. Amounts in different currencies stay in separate groups.
//	@Tags			Backoffice requests
//	@Produce		json
//	@Param			group_by		query		string	false	"category (default), service, status, zone or organization"
//	@Param			category		query		string	false	"Category slug"
//	@Param			zone_id			query		string	false	"Zone"
//	@Param			organization_id	query		string	false	"Partner"
//	@Param			from			query		string	false	"Created at or after"
//	@Param			to				query		string	false	"Created before"
//	@Success		200				{object}	dtos.RequestSummary
//	@Failure		422				{object}	shared.ErrorResponse	"INVALID_GROUPING"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/requests/summary [get]
func (c *DispatchController) SummarizeAdminRequests(ctx *gin.Context) {
	filter, ok := requestFilter(ctx)
	if !ok {
		return
	}
	summary, err := c.dispatch.SummarizeRequests(ctx.Request.Context(), ctx.Query("group_by"), filter)
	respond(ctx, http.StatusOK, summary, err)
}

// GetAdminRequest godoc
//
//	@Summary		Get a ride or service request with its history
//	@Description	Requires requests:read.
//	@Tags			Backoffice requests
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.AdminRequestDetail
//	@Failure		404	{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/requests/{id} [get]
func (c *DispatchController) GetAdminRequest(ctx *gin.Context) {
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	detail, err := c.dispatch.GetAdminRequest(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, detail, err)
}

// ListPartnerRequests godoc
//
//	@Summary		List the rides of my fleet
//	@Description	Requires partner.requests:read. Only requests served by drivers of the partner of the token, whatever organization_id says.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			status		query		string	false	"One status or several separated by commas"
//	@Param			category	query		string	false	"Category slug"
//	@Param			service_id	query		string	false	"Service"
//	@Param			provider_id	query		string	false	"Driver"
//	@Param			from		query		string	false	"Created at or after"
//	@Param			to			query		string	false	"Created before"
//	@Param			page		query		int		false	"Page"
//	@Param			size		query		int		false	"Page size"
//	@Success		200			{object}	dtos.AdminRequestList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/requests [get]
func (c *DispatchController) ListPartnerRequests(ctx *gin.Context) {
	organizationID, ok := partnerOrganization(ctx)
	if !ok {
		return
	}
	filter, ok := requestFilter(ctx)
	if !ok {
		return
	}
	filter.OrganizationID = &organizationID
	c.listRequests(ctx, filter)
}

// SummarizePartnerRequests godoc
//
//	@Summary		Rides of my fleet grouped
//	@Description	Requires partner.requests:read.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			group_by	query		string	false	"category (default), service, status or zone"
//	@Param			category	query		string	false	"Category slug"
//	@Param			from		query		string	false	"Created at or after"
//	@Param			to			query		string	false	"Created before"
//	@Success		200			{object}	dtos.RequestSummary
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/requests/summary [get]
func (c *DispatchController) SummarizePartnerRequests(ctx *gin.Context) {
	organizationID, ok := partnerOrganization(ctx)
	if !ok {
		return
	}
	filter, ok := requestFilter(ctx)
	if !ok {
		return
	}
	filter.OrganizationID = &organizationID
	summary, err := c.dispatch.SummarizeRequests(ctx.Request.Context(), ctx.Query("group_by"), filter)
	respond(ctx, http.StatusOK, summary, err)
}

// GetPartnerRequest godoc
//
//	@Summary		Get a ride of my fleet
//	@Description	Requires partner.requests:read. The customer's phone is not included.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.AdminRequestDetail
//	@Failure		404	{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/requests/{id} [get]
func (c *DispatchController) GetPartnerRequest(ctx *gin.Context) {
	organizationID, ok := partnerOrganization(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	detail, err := c.dispatch.GetPartnerRequest(ctx.Request.Context(), organizationID, id)
	respond(ctx, http.StatusOK, detail, err)
}
