package experience

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func vehicleParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "id", "VEHICLE_NOT_FOUND", "Vehicle not found.")
}

// MyVehicles godoc
//
//	@Summary	My vehicles
//	@Tags		Customer vehicles
//	@Produce	json
//	@Success	200	{object}	dtos.CustomerVehicleList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/vehicles [get]
func (c *Controller) MyVehicles(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	list, err := c.uc.MyVehicles(ctx.Request.Context(), customerID)
	respond(ctx, http.StatusOK, list, err)
}

// AddVehicle godoc
//
//	@Summary	Add a vehicle
//	@Tags		Customer vehicles
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.CustomerVehicleInput	true	"Vehicle"
//	@Success	201		{object}	dtos.CustomerVehicleView
//	@Failure	409		{object}	shared.ErrorResponse	"Same plate"
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/vehicles [post]
func (c *Controller) AddVehicle(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	var in dtos.CustomerVehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.AddVehicle(ctx.Request.Context(), customerID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// UpdateVehicle godoc
//
//	@Summary	Change a vehicle
//	@Tags		Customer vehicles
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string						true	"Vehicle id"
//	@Param		body	body		dtos.CustomerVehicleInput	true	"Vehicle"
//	@Success	200		{object}	dtos.CustomerVehicleView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/vehicles/{id} [put]
func (c *Controller) UpdateVehicle(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := vehicleParam(ctx)
	if !ok {
		return
	}
	var in dtos.CustomerVehicleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.UpdateVehicle(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// RemoveVehicle godoc
//
//	@Summary	Remove a vehicle
//	@Tags		Customer vehicles
//	@Param		id	path	string	true	"Vehicle id"
//	@Success	204
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/vehicles/{id} [delete]
func (c *Controller) RemoveVehicle(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := vehicleParam(ctx)
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.uc.RemoveVehicle(ctx.Request.Context(), customerID, id))
}

// NewPhotoUpload godoc
//
//	@Summary		Get a link to upload a photo
//	@Description	The type and the size are checked against the settings of the backoffice. The app sends the file directly to the storage and then uses the id.
//	@Tags			Photos
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.PhotoUploadInput	true	"File"
//	@Success		201		{object}	dtos.PhotoUploadView
//	@Failure		422		{object}	shared.ErrorResponse	"Type or size not allowed"
//	@Failure		503		{object}	shared.ErrorResponse	"No storage"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/media/photos [post]
func (c *Controller) NewPhotoUpload(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	var in dtos.PhotoUploadInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.NewPhotoUpload(ctx.Request.Context(), userID, in)
	respond(ctx, http.StatusCreated, view, err)
}

// AttachRequestPhotos godoc
//
//	@Summary		Add uploaded photos to a request
//	@Description	Customer of the request. The limit is in the settings of the backoffice, and each photo has to be uploaded with the announced type and size.
//	@Tags			Photos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.AttachPhotosInput	true	"Photos"
//	@Success		200		{object}	dtos.PhotoList
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse	"Over the limit, or not uploaded"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/photos [post]
func (c *Controller) AttachRequestPhotos(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.AttachPhotosInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.AttachRequestPhotos(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// RequestPhotos godoc
//
//	@Summary		Photos of a request
//	@Description	Customer or provider of the request, with temporary links.
//	@Tags			Photos
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.PhotoList
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/photos [get]
func (c *Controller) RequestPhotos(ctx *gin.Context) {
	userID, ok := signedInUser(ctx)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.uc.RequestPhotos(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}
