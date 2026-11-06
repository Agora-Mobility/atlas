package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/fleet"
	"github.com/Agora-Mobility/atlas/usecases/fleet/dtos"
	"github.com/Agora-Mobility/atlas/usecases/providers"
	providerdtos "github.com/Agora-Mobility/atlas/usecases/providers/dtos"
)

type FleetController struct {
	fleet *fleet.Usecase
}

func NewFleetController(uc *fleet.Usecase) *FleetController {
	return &FleetController{fleet: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{fleet.ErrPartnerNotFound, http.StatusNotFound, "PARTNER_NOT_FOUND", "Partner not found."},
	{fleet.ErrDriverNotFound, http.StatusNotFound, "DRIVER_NOT_FOUND", "Driver not found."},
	{fleet.ErrVehicleNotFound, http.StatusNotFound, "VEHICLE_NOT_FOUND", "Vehicle not found."},
	{fleet.ErrWorkshopNotFound, http.StatusNotFound, "WORKSHOP_NOT_FOUND", "Workshop not found."},
	{fleet.ErrWorkshopServiceNotFound, http.StatusNotFound, "WORKSHOP_SERVICE_NOT_FOUND", "Workshop service not found."},
	{fleet.ErrVehicleInUse, http.StatusConflict, "VEHICLE_IN_USE", "Release the vehicle from its driver first."},
	{fleet.ErrDriverBusy, http.StatusConflict, "DRIVER_BUSY", "The driver has an active job."},
	{repository.ErrPlateTaken, http.StatusConflict, "PLATE_TAKEN", "This fleet already has a vehicle with this plate."},
	{repository.ErrVehicleTaken, http.StatusConflict, "VEHICLE_NOT_AVAILABLE", "This vehicle is not available."},
	{providers.ErrPhoneTaken, http.StatusConflict, "PHONE_ALREADY_EXISTS", "Phone already registered."},
	{providers.ErrEmailTaken, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "E-mail already registered."},
	{providers.ErrServiceNotOpen, http.StatusUnprocessableEntity, "SERVICE_NOT_OPEN", "This service does not accept providers."},
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func respondProfile(ctx *gin.Context, status int, view *providerdtos.ProviderView, err error) {
	respond(ctx, status, view, err)
}

func respondEmpty(ctx *gin.Context, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func handleError(ctx *gin.Context, err error) {
	var incomplete *providers.IncompleteError
	if errors.As(err, &incomplete) {
		shared.UnprocessableWithDetails(ctx, incomplete.Code, "Some requirements are not met.", incomplete.Fields)
		return
	}
	for _, mapped := range errorTable {
		if errors.Is(err, mapped.target) {
			shared.RespondError(ctx, mapped.status, mapped.code, mapped.message)
			return
		}
	}
	if validationErr, ok := customerrors.AsValidation(err); ok {
		shared.ValidationFailed(ctx, validationErr)
		return
	}
	shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
}

func denied(ctx *gin.Context) (entities.Actor, uuid.UUID, bool) {
	shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
	return entities.Actor{}, uuid.Nil, false
}

func scope(ctx *gin.Context) (entities.Actor, uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		return denied(ctx)
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	actor := access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent())
	raw := ctx.Param("orgId")
	if raw == "" {
		if access.Scope != enums.OrganizationTypePartner {
			return denied(ctx)
		}
		return actor, access.OrganizationID, true
	}
	if access.Scope != enums.OrganizationTypePlatform {
		return denied(ctx)
	}
	organizationID, err := uuid.Parse(raw)
	if err != nil {
		shared.NotFound(ctx, "PARTNER_NOT_FOUND", "Partner not found.")
		return entities.Actor{}, uuid.Nil, false
	}
	return actor, organizationID, true
}

func idParam(ctx *gin.Context, name, code, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param(name))
	if err != nil {
		shared.NotFound(ctx, code, message)
		return uuid.Nil, false
	}
	return id, true
}

func bindPage(ctx *gin.Context) (pagination.Params, bool) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return page, false
	}
	return page, true
}

func optionalOrganization(ctx *gin.Context) (*uuid.UUID, bool) {
	raw := ctx.Query("organization_id")
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid organization_id.")
		return nil, false
	}
	return &id, true
}

// ListDrivers godoc
//
//	@Summary		List the drivers of a fleet
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			status	query		string	false	"draft, pending, approved, rejected or suspended"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.DriverList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/drivers [get]
//	@Router			/api/v1/partners/{orgId}/fleet/drivers [get]
func (c *FleetController) ListDrivers(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	page, ok := bindPage(ctx)
	if !ok {
		return
	}
	list, err := c.fleet.ListDrivers(ctx.Request.Context(), organizationID, ctx.Query("status"), page)
	respond(ctx, http.StatusOK, list, err)
}

// RegisterDriver godoc
//
//	@Summary		Register a driver in a fleet
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. Creates the account as a draft and sends an SMS code; the driver validates it with /auth/recovery/verify, sets the password with /auth/recovery/confirm and sends the documents in the app. AGORA still reviews and approves. vehicle_id is optional and can be associated later.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string						true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			body	body		dtos.RegisterDriverInput	true	"Driver"
//	@Success		201		{object}	providerdtos.ProviderView
//	@Failure		409		{object}	shared.ErrorResponse	"PHONE_ALREADY_EXISTS or VEHICLE_NOT_AVAILABLE"
//	@Failure		422		{object}	shared.ErrorResponse	"SERVICE_NOT_OPEN or VEHICLE_NOT_SUITABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/drivers [post]
//	@Router			/api/v1/partners/{orgId}/fleet/drivers [post]
func (c *FleetController) RegisterDriver(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	var in dtos.RegisterDriverInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.RegisterDriver(ctx.Request.Context(), actor, organizationID, in)
	respondProfile(ctx, http.StatusCreated, view, err)
}

// GetDriver godoc
//
//	@Summary		Get a driver of a fleet
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet. Document files are not linked here: they are reviewed by AGORA.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string	true	"Driver user id"
//	@Success		200		{object}	providerdtos.ProviderView
//	@Failure		404		{object}	shared.ErrorResponse	"DRIVER_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/drivers/{id} [get]
//	@Router			/api/v1/partners/{orgId}/fleet/drivers/{id} [get]
func (c *FleetController) GetDriver(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	driverID, ok := idParam(ctx, "id", "DRIVER_NOT_FOUND", "Driver not found.")
	if !ok {
		return
	}
	view, err := c.fleet.GetDriver(ctx.Request.Context(), organizationID, driverID)
	respondProfile(ctx, http.StatusOK, view, err)
}

// AssignVehicle godoc
//
//	@Summary		Associate a vehicle with a driver
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. Replaces the current vehicle. The vehicle must suit the service of the driver and be free.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string					true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string					true	"Driver user id"
//	@Param			body	body		dtos.AssignVehicleInput	true	"Vehicle"
//	@Success		200		{object}	providerdtos.ProviderView
//	@Failure		404		{object}	shared.ErrorResponse	"DRIVER_NOT_FOUND or VEHICLE_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"DRIVER_BUSY or VEHICLE_NOT_AVAILABLE"
//	@Failure		422		{object}	shared.ErrorResponse	"VEHICLE_NOT_SUITABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/drivers/{id}/vehicle [put]
//	@Router			/api/v1/partners/{orgId}/fleet/drivers/{id}/vehicle [put]
func (c *FleetController) AssignVehicle(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	driverID, ok := idParam(ctx, "id", "DRIVER_NOT_FOUND", "Driver not found.")
	if !ok {
		return
	}
	var in dtos.AssignVehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.AssignVehicle(ctx.Request.Context(), actor, organizationID, driverID, in.VehicleID)
	respondProfile(ctx, http.StatusOK, view, err)
}

// ReleaseVehicle godoc
//
//	@Summary		Take the vehicle away from a driver
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. The driver goes offline and cannot go online until another vehicle is associated.
//	@Tags			Partner fleet
//	@Param			orgId	path	string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path	string	true	"Driver user id"
//	@Success		204
//	@Failure		404	{object}	shared.ErrorResponse	"DRIVER_NOT_FOUND"
//	@Failure		409	{object}	shared.ErrorResponse	"DRIVER_BUSY"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/drivers/{id}/vehicle [delete]
//	@Router			/api/v1/partners/{orgId}/fleet/drivers/{id}/vehicle [delete]
func (c *FleetController) ReleaseVehicle(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	driverID, ok := idParam(ctx, "id", "DRIVER_NOT_FOUND", "Driver not found.")
	if !ok {
		return
	}
	respondEmpty(ctx, c.fleet.ReleaseVehicle(ctx.Request.Context(), actor, organizationID, driverID))
}

// ListVehicles godoc
//
//	@Summary		List the vehicles of a fleet
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			kind	query		string	false	"car, motorcycle, van, truck or tow_truck"
//	@Param			search	query		string	false	"Plate"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.VehicleList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/vehicles [get]
//	@Router			/api/v1/partners/{orgId}/fleet/vehicles [get]
func (c *FleetController) ListVehicles(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	page, ok := bindPage(ctx)
	if !ok {
		return
	}
	list, err := c.fleet.ListVehicles(ctx.Request.Context(), repository.VehicleFilter{OrganizationID: &organizationID, Kind: ctx.Query("kind"), Search: ctx.Query("search")}, page)
	respond(ctx, http.StatusOK, list, err)
}

// CreateVehicle godoc
//
//	@Summary		Create a vehicle in a fleet
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. Send driver_id to associate it at once. Tow trucks serve towing; vans and trucks serve removals.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string					true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			body	body		dtos.FleetVehicleInput	true	"Vehicle"
//	@Success		201		{object}	dtos.FleetVehicleView
//	@Failure		409		{object}	shared.ErrorResponse	"PLATE_TAKEN or DRIVER_BUSY"
//	@Failure		422		{object}	shared.ErrorResponse	"VEHICLE_NOT_SUITABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/vehicles [post]
//	@Router			/api/v1/partners/{orgId}/fleet/vehicles [post]
func (c *FleetController) CreateVehicle(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	var in dtos.FleetVehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.CreateVehicle(ctx.Request.Context(), actor, organizationID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// GetVehicle godoc
//
//	@Summary		Get a vehicle of a fleet
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string	true	"Vehicle id"
//	@Success		200		{object}	dtos.FleetVehicleView
//	@Failure		404		{object}	shared.ErrorResponse	"VEHICLE_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/vehicles/{id} [get]
//	@Router			/api/v1/partners/{orgId}/fleet/vehicles/{id} [get]
func (c *FleetController) GetVehicle(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	vehicleID, ok := idParam(ctx, "id", "VEHICLE_NOT_FOUND", "Vehicle not found.")
	if !ok {
		return
	}
	view, err := c.fleet.GetVehicle(ctx.Request.Context(), organizationID, vehicleID)
	respond(ctx, http.StatusOK, view, err)
}

// UpdateVehicle godoc
//
//	@Summary		Update a vehicle of a fleet
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string					true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string					true	"Vehicle id"
//	@Param			body	body		dtos.UpdateVehicleInput	true	"Vehicle"
//	@Success		200		{object}	dtos.FleetVehicleView
//	@Failure		404		{object}	shared.ErrorResponse	"VEHICLE_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"PLATE_TAKEN or VEHICLE_IN_USE"
//	@Failure		422		{object}	shared.ErrorResponse	"VEHICLE_NOT_SUITABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/vehicles/{id} [put]
//	@Router			/api/v1/partners/{orgId}/fleet/vehicles/{id} [put]
func (c *FleetController) UpdateVehicle(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	vehicleID, ok := idParam(ctx, "id", "VEHICLE_NOT_FOUND", "Vehicle not found.")
	if !ok {
		return
	}
	var in dtos.UpdateVehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.UpdateVehicle(ctx.Request.Context(), actor, organizationID, vehicleID, in)
	respond(ctx, http.StatusOK, view, err)
}

// DeactivateVehicle godoc
//
//	@Summary		Deactivate a vehicle of a fleet
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. Also releases it from its driver.
//	@Tags			Partner fleet
//	@Param			orgId	path	string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path	string	true	"Vehicle id"
//	@Success		204
//	@Failure		404	{object}	shared.ErrorResponse	"VEHICLE_NOT_FOUND"
//	@Failure		409	{object}	shared.ErrorResponse	"DRIVER_BUSY"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/vehicles/{id} [delete]
//	@Router			/api/v1/partners/{orgId}/fleet/vehicles/{id} [delete]
func (c *FleetController) DeactivateVehicle(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	vehicleID, ok := idParam(ctx, "id", "VEHICLE_NOT_FOUND", "Vehicle not found.")
	if !ok {
		return
	}
	respondEmpty(ctx, c.fleet.DeactivateVehicle(ctx.Request.Context(), actor, organizationID, vehicleID))
}

// ListWorkshops godoc
//
//	@Summary		List the workshops of a partner
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet. Maintenance.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.WorkshopList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops [get]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops [get]
func (c *FleetController) ListWorkshops(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	page, ok := bindPage(ctx)
	if !ok {
		return
	}
	list, err := c.fleet.ListWorkshops(ctx.Request.Context(), &organizationID, page)
	respond(ctx, http.StatusOK, list, err)
}

// CreateWorkshop godoc
//
//	@Summary		Create a workshop for a partner
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet. Maintenance.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string				true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			body	body		dtos.WorkshopInput	true	"Workshop"
//	@Success		201		{object}	dtos.WorkshopView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops [post]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops [post]
func (c *FleetController) CreateWorkshop(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	var in dtos.WorkshopInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.CreateWorkshop(ctx.Request.Context(), actor, organizationID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// GetWorkshop godoc
//
//	@Summary		Get a workshop with its services
//	@Description	Requires partner.providers:read on /fleet, for the partner of the token, or partners:read on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string	true	"Workshop id"
//	@Success		200		{object}	dtos.WorkshopView
//	@Failure		404		{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id} [get]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id} [get]
func (c *FleetController) GetWorkshop(ctx *gin.Context) {
	_, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	view, err := c.fleet.GetWorkshop(ctx.Request.Context(), organizationID, workshopID)
	respond(ctx, http.StatusOK, view, err)
}

// UpdateWorkshop godoc
//
//	@Summary		Update a workshop
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string				true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string				true	"Workshop id"
//	@Param			body	body		dtos.WorkshopInput	true	"Workshop"
//	@Success		200		{object}	dtos.WorkshopView
//	@Failure		404		{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id} [put]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id} [put]
func (c *FleetController) UpdateWorkshop(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	var in dtos.WorkshopInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.UpdateWorkshop(ctx.Request.Context(), actor, organizationID, workshopID, in)
	respond(ctx, http.StatusOK, view, err)
}

// DeactivateWorkshop godoc
//
//	@Summary		Deactivate a workshop
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Param			orgId	path	string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path	string	true	"Workshop id"
//	@Success		204
//	@Failure		404	{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id} [delete]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id} [delete]
func (c *FleetController) DeactivateWorkshop(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	respondEmpty(ctx, c.fleet.DeactivateWorkshop(ctx.Request.Context(), actor, organizationID, workshopID))
}

// CreateWorkshopService godoc
//
//	@Summary		Add a service to a workshop
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string						true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id		path		string						true	"Workshop id"
//	@Param			body	body		dtos.WorkshopServiceInput	true	"Service"
//	@Success		201		{object}	dtos.WorkshopServiceView
//	@Failure		404		{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id}/services [post]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id}/services [post]
func (c *FleetController) CreateWorkshopService(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	var in dtos.WorkshopServiceInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.CreateWorkshopService(ctx.Request.Context(), actor, organizationID, workshopID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// UpdateWorkshopService godoc
//
//	@Summary		Update a service of a workshop
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Accept			json
//	@Produce		json
//	@Param			orgId		path		string						true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id			path		string						true	"Workshop id"
//	@Param			serviceId	path		string						true	"Workshop service id"
//	@Param			body		body		dtos.WorkshopServiceInput	true	"Service"
//	@Success		200			{object}	dtos.WorkshopServiceView
//	@Failure		404			{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND or WORKSHOP_SERVICE_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id}/services/{serviceId} [put]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id}/services/{serviceId} [put]
func (c *FleetController) UpdateWorkshopService(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	serviceID, ok := idParam(ctx, "serviceId", "WORKSHOP_SERVICE_NOT_FOUND", "Workshop service not found.")
	if !ok {
		return
	}
	var in dtos.WorkshopServiceInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.fleet.UpdateWorkshopService(ctx.Request.Context(), actor, organizationID, workshopID, serviceID, in)
	respond(ctx, http.StatusOK, view, err)
}

// DeactivateWorkshopService godoc
//
//	@Summary		Deactivate a service of a workshop
//	@Description	Requires partner.providers:manage on /fleet, for the partner of the token, or partners:manage on /partners/{orgId}/fleet.
//	@Tags			Partner fleet
//	@Param			orgId		path	string	true	"Partner id, only on /partners/{orgId}/fleet"
//	@Param			id			path	string	true	"Workshop id"
//	@Param			serviceId	path	string	true	"Workshop service id"
//	@Success		204
//	@Failure		404	{object}	shared.ErrorResponse	"WORKSHOP_NOT_FOUND or WORKSHOP_SERVICE_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id}/services/{serviceId} [delete]
//	@Router			/api/v1/partners/{orgId}/fleet/workshops/{id}/services/{serviceId} [delete]
func (c *FleetController) DeactivateWorkshopService(ctx *gin.Context) {
	actor, organizationID, ok := scope(ctx)
	if !ok {
		return
	}
	workshopID, ok := idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
	if !ok {
		return
	}
	serviceID, ok := idParam(ctx, "serviceId", "WORKSHOP_SERVICE_NOT_FOUND", "Workshop service not found.")
	if !ok {
		return
	}
	respondEmpty(ctx, c.fleet.DeactivateWorkshopService(ctx.Request.Context(), actor, organizationID, workshopID, serviceID))
}

// ListAllVehicles godoc
//
//	@Summary		List the vehicles of every partner
//	@Description	Requires providers:read.
//	@Tags			Backoffice partners
//	@Produce		json
//	@Param			organization_id	query		string	false	"Partner"
//	@Param			kind			query		string	false	"car, motorcycle, van, truck or tow_truck"
//	@Param			search			query		string	false	"Plate"
//	@Param			page			query		int		false	"Page"
//	@Param			size			query		int		false	"Page size"
//	@Success		200				{object}	dtos.VehicleList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/vehicles [get]
func (c *FleetController) ListAllVehicles(ctx *gin.Context) {
	organizationID, ok := optionalOrganization(ctx)
	if !ok {
		return
	}
	page, ok := bindPage(ctx)
	if !ok {
		return
	}
	list, err := c.fleet.ListVehicles(ctx.Request.Context(), repository.VehicleFilter{OrganizationID: organizationID, Kind: ctx.Query("kind"), Search: ctx.Query("search")}, page)
	respond(ctx, http.StatusOK, list, err)
}

// ListAllWorkshops godoc
//
//	@Summary		List the workshops of every partner
//	@Description	Requires partners:read. To create or change one, use /partners/{orgId}/fleet/workshops.
//	@Tags			Backoffice partners
//	@Produce		json
//	@Param			organization_id	query		string	false	"Partner"
//	@Param			page			query		int		false	"Page"
//	@Param			size			query		int		false	"Page size"
//	@Success		200				{object}	dtos.WorkshopList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/workshops [get]
func (c *FleetController) ListAllWorkshops(ctx *gin.Context) {
	organizationID, ok := optionalOrganization(ctx)
	if !ok {
		return
	}
	page, ok := bindPage(ctx)
	if !ok {
		return
	}
	list, err := c.fleet.ListWorkshops(ctx.Request.Context(), organizationID, page)
	respond(ctx, http.StatusOK, list, err)
}
