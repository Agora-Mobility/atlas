package experience

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/experience"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

type Controller struct {
	uc *experience.Usecase
}

func NewController(uc *experience.Usecase) *Controller {
	return &Controller{uc: uc}
}

// AdminListItems godoc
//
//	@Summary		List a catalog of the backoffice
//	@Description	kind is one of compliment, tip_preset, quick_reply, towing_vehicle, towing_incident, removal_item, removal_vehicle, removal_extra, removal_window, removal_property, help_topic or workshop_category.
//	@Tags			Backoffice
//	@Produce		json
//	@Param			kind		path		string	true	"Catalog"
//	@Param			category	query		string	false	"Category slug"
//	@Param			property	query		string	false	"Only the removal items of this removal_property code; an item without properties fits every type"
//	@Success		200			{object}	dtos.CatalogItemList
//	@Failure		404			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/catalog/{kind} [get]
func (c *Controller) AdminListItems(ctx *gin.Context) {
	list, err := c.uc.ListItems(ctx.Request.Context(), ctx.Param("kind"), ctx.Query("category"), ctx.Query("property"), true)
	respond(ctx, http.StatusOK, list, err)
}

// AdminCreateItem godoc
//
//	@Summary		Add an item to a catalog
//	@Description	Prices are integer cents. attrs holds the numbers of each kind, such as included_km on towing_vehicle, and they are validated. A removal_item may list in attrs.properties the removal_property codes it belongs to, such as ["home", "office"]; without the list it fits every type.
//	@Tags			Backoffice
//	@Accept			json
//	@Produce		json
//	@Param			kind	path		string					true	"Catalog"
//	@Param			body	body		dtos.CatalogItemInput	true	"Item"
//	@Success		201		{object}	dtos.CatalogItemView
//	@Failure		409		{object}	shared.ErrorResponse	"The code exists"
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/catalog/{kind} [post]
func (c *Controller) AdminCreateItem(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.CatalogItemInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.CreateItem(ctx.Request.Context(), actor, ctx.Param("kind"), in)
	respond(ctx, http.StatusCreated, view, err)
}

// AdminUpdateItem godoc
//
//	@Summary	Change an item of a catalog
//	@Tags		Backoffice
//	@Accept		json
//	@Produce	json
//	@Param		kind	path		string					true	"Catalog"
//	@Param		id		path		string					true	"Item id"
//	@Param		body	body		dtos.CatalogItemInput	true	"Item"
//	@Success	200		{object}	dtos.CatalogItemView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/catalog/{kind}/{id} [put]
func (c *Controller) AdminUpdateItem(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "id", "CATALOG_ITEM_NOT_FOUND", "Catalog item not found.")
	if !ok {
		return
	}
	var in dtos.CatalogItemInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.UpdateItem(ctx.Request.Context(), actor, ctx.Param("kind"), id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AdminSettings godoc
//
//	@Summary	Settings of the platform
//	@Tags		Backoffice
//	@Produce	json
//	@Success	200	{object}	dtos.SettingList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/settings [get]
func (c *Controller) AdminSettings(ctx *gin.Context) {
	list, err := c.uc.Settings(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// AdminSetSetting godoc
//
//	@Summary	Change a setting of the platform
//	@Tags		Backoffice
//	@Accept		json
//	@Produce	json
//	@Param		key		path		string				true	"Key"
//	@Param		body	body		dtos.SettingInput	true	"Value"
//	@Success	200		{object}	dtos.SettingList
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/settings/{key} [put]
func (c *Controller) AdminSetSetting(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.SettingInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	list, err := c.uc.SetSetting(ctx.Request.Context(), actor, ctx.Param("key"), in)
	respond(ctx, http.StatusOK, list, err)
}

// RatingOptions godoc
//
//	@Summary	Compliments and suggested tips of a service
//	@Tags		Ratings
//	@Produce	json
//	@Param		category	query		string	true	"Category slug"
//	@Success	200			{object}	dtos.RatingOptions
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/rating-options [get]
func (c *Controller) RatingOptions(ctx *gin.Context) {
	if ctx.Query("category") == "" {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "category is required.")
		return
	}
	view, err := c.uc.RatingOptions(ctx.Request.Context(), ctx.Query("category"))
	respond(ctx, http.StatusOK, view, err)
}

// RateRequest godoc
//
//	@Summary		Rate a completed request
//	@Description	Customer of the request, once. The stars count for the average of the provider.
//	@Tags			Ratings
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Request id"
//	@Param			body	body		dtos.RateInput	true	"Rating"
//	@Success		201		{object}	dtos.RatingView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"Not completed or already rated"
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/rating [post]
func (c *Controller) RateRequest(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.RateInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.RateRequest(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// MyRatings godoc
//
//	@Summary	Ratings the provider received
//	@Tags		Provider work
//	@Produce	json
//	@Param		page	query		int	false	"Page"
//	@Param		size	query		int	false	"Page size"
//	@Success	200		{object}	dtos.RatingList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/ratings [get]
func (c *Controller) MyRatings(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	list, err := c.uc.ProviderRatings(ctx.Request.Context(), providerID, page)
	respond(ctx, http.StatusOK, list, err)
}

// TowingOptions godoc
//
//	@Summary		What the towing form needs
//	@Description	Vehicle types with their price, incidents, and the nearest partner workshop as the default destination when lat and lng are given.
//	@Tags			Towing
//	@Produce		json
//	@Param			lat	query		number	false	"Latitude"
//	@Param			lng	query		number	false	"Longitude"
//	@Success		200	{object}	dtos.TowingOptions
//	@Failure		422	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/towing/options [get]
func (c *Controller) TowingOptions(ctx *gin.Context) {
	var point *entities.GeoPoint
	if ctx.Query("lat") != "" || ctx.Query("lng") != "" {
		lat, latErr := strconv.ParseFloat(ctx.Query("lat"), 64)
		lng, lngErr := strconv.ParseFloat(ctx.Query("lng"), 64)
		if latErr != nil || lngErr != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "lat and lng must be numbers.")
			return
		}
		point = &entities.GeoPoint{Lat: lat, Lng: lng}
	}
	view, err := c.uc.TowingOptions(ctx.Request.Context(), point)
	respond(ctx, http.StatusOK, view, err)
}
