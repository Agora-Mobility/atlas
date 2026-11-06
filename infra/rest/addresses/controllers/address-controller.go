package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/usecases/addresses"
	"github.com/Agora-Mobility/atlas/usecases/addresses/dtos"
)

type AddressController struct {
	addresses *addresses.Usecase
}

func NewAddressController(uc *addresses.Usecase) *AddressController {
	return &AddressController{addresses: uc}
}

func currentUser(ctx *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
	}
	return userID, ok
}

func addressParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "ADDRESS_NOT_FOUND", "Address not found.")
		return uuid.Nil, false
	}
	return id, true
}

func handleError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, addresses.ErrAddressNotFound):
		shared.NotFound(ctx, "ADDRESS_NOT_FOUND", "Address not found.")
	case errors.Is(err, entities.ErrAddressKindTaken):
		shared.Conflict(ctx, "ADDRESS_KIND_TAKEN", "You already have an address of this kind.")
	default:
		if validationErr, ok := customerrors.AsValidation(err); ok {
			shared.ValidationFailed(ctx, validationErr)
			return
		}
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
	}
}

// List godoc
//
//	@Summary	My saved addresses
//	@Tags		Addresses
//	@Produce	json
//	@Success	200	{object}	dtos.UserAddressList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/addresses [get]
func (c *AddressController) List(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	list, err := c.addresses.List(ctx.Request.Context(), userID)
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, list)
}

// Create godoc
//
//	@Summary		Save an address
//	@Description	lat and lng are mandatory; the rest only helps. kind is home, work or other, with one home and one work per user.
//	@Tags			Addresses
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.UserAddressInput	true	"Address"
//	@Success		201		{object}	dtos.UserAddressView
//	@Failure		409		{object}	shared.ErrorResponse	"ADDRESS_KIND_TAKEN"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/addresses [post]
func (c *AddressController) Create(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	var in dtos.UserAddressInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.addresses.Create(ctx.Request.Context(), userID, in)
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, view)
}

// Update godoc
//
//	@Summary	Change a saved address
//	@Tags		Addresses
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string					true	"Address id"
//	@Param		body	body		dtos.UserAddressInput	true	"Address"
//	@Success	200		{object}	dtos.UserAddressView
//	@Failure	404		{object}	shared.ErrorResponse	"ADDRESS_NOT_FOUND"
//	@Failure	409		{object}	shared.ErrorResponse	"ADDRESS_KIND_TAKEN"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/addresses/{id} [put]
func (c *AddressController) Update(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	id, ok := addressParam(ctx)
	if !ok {
		return
	}
	var in dtos.UserAddressInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.addresses.Update(ctx.Request.Context(), userID, id, in)
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, view)
}

// Delete godoc
//
//	@Summary	Delete a saved address
//	@Tags		Addresses
//	@Param		id	path	string	true	"Address id"
//	@Success	204
//	@Failure	404	{object}	shared.ErrorResponse	"ADDRESS_NOT_FOUND"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/addresses/{id} [delete]
func (c *AddressController) Delete(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	id, ok := addressParam(ctx)
	if !ok {
		return
	}
	if err := c.addresses.Delete(ctx.Request.Context(), userID, id); err != nil {
		handleError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}
