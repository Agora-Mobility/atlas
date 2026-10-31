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
	"github.com/Agora-Mobility/atlas/usecases/catalog"
	"github.com/Agora-Mobility/atlas/usecases/catalog/dtos"
)

type CatalogController struct {
	catalog *catalog.Usecase
}

func NewCatalogController(uc *catalog.Usecase) *CatalogController {
	return &CatalogController{catalog: uc}
}

// Catalog godoc
//
//	@Summary		Service catalog of the app
//	@Description	Active categories in display order, each with its active services. Categories without active services are left out. No sign-in needed. Fees are in cêntimos of kwanza (1 Kz = 100).
//	@Tags			Catalog
//	@Produce		json
//	@Success		200	{object}	dtos.Catalog
//	@Failure		400	{object}	shared.ErrorResponse	"Missing X-Client-Id"
//	@Failure		401	{object}	shared.ErrorResponse	"Unknown client"
//	@Security		ClientId
//	@Router			/api/v1/catalog [get]
func (c *CatalogController) Catalog(ctx *gin.Context) {
	resp, err := c.catalog.Catalog(ctx.Request.Context())
	respond(ctx, http.StatusOK, resp, err)
}

// ListCategories godoc
//
//	@Summary		List the service categories
//	@Description	Requires categories:manage. Active and inactive, in display order.
//	@Tags			Backoffice catalog
//	@Produce		json
//	@Success		200	{object}	dtos.CategoryList
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/categories [get]
func (c *CatalogController) ListCategories(ctx *gin.Context) {
	resp, err := c.catalog.ListCategories(ctx.Request.Context())
	respond(ctx, http.StatusOK, resp, err)
}

// GetCategory godoc
//
//	@Summary		Get a service category
//	@Description	Requires categories:manage.
//	@Tags			Backoffice catalog
//	@Produce		json
//	@Param			id	path		string	true	"Category id"
//	@Success		200	{object}	dtos.CategoryView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"Category not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/categories/{id} [get]
func (c *CatalogController) GetCategory(ctx *gin.Context) {
	id, ok := idParam(ctx, errCategoryNotFound)
	if !ok {
		return
	}
	resp, err := c.catalog.GetCategory(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, resp, err)
}

// CreateCategory godoc
//
//	@Summary		Create a service category
//	@Description	Requires categories:manage. The slug cannot change later. The category starts active but stays out of the app until it has an active service.
//	@Tags			Backoffice catalog
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CreateCategoryInput	true	"Category"
//	@Success		201		{object}	dtos.CategoryView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		409		{object}	shared.ErrorResponse	"Slug already used"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/categories [post]
func (c *CatalogController) CreateCategory(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var req dtos.CreateCategoryInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.catalog.CreateCategory(ctx.Request.Context(), actor, req)
	respond(ctx, http.StatusCreated, resp, err)
}

// UpdateCategory godoc
//
//	@Summary		Change a service category
//	@Description	Requires categories:manage. Only the fields sent change. Setting is_active to false hides the category and all its services from the app; categories are never deleted.
//	@Tags			Backoffice catalog
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"Category id"
//	@Param			body	body		dtos.UpdateCategoryInput	true	"Fields to change"
//	@Success		200		{object}	dtos.CategoryView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Category not found"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/categories/{id} [patch]
func (c *CatalogController) UpdateCategory(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, errCategoryNotFound)
	if !ok {
		return
	}
	var req dtos.UpdateCategoryInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.catalog.UpdateCategory(ctx.Request.Context(), actor, id, req)
	respond(ctx, http.StatusOK, resp, err)
}

// ListServices godoc
//
//	@Summary		List the services
//	@Description	Requires services:manage. Active and inactive unless filtered.
//	@Tags			Backoffice catalog
//	@Produce		json
//	@Param			category_id	query		string	false	"Category id"
//	@Param			is_active	query		bool	false	"Active or inactive only"
//	@Param			search		query		string	false	"Part of the name or slug"
//	@Param			page		query		int		false	"Page (from 1)"
//	@Param			size		query		int		false	"Page size (max 100)"
//	@Success		200			{object}	dtos.ServicePage
//	@Failure		401			{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403			{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		422			{object}	shared.ErrorResponse	"Invalid filters"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/services [get]
func (c *CatalogController) ListServices(ctx *gin.Context) {
	var query dtos.ServiceListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.catalog.ListServices(ctx.Request.Context(), query)
	respond(ctx, http.StatusOK, resp, err)
}

// GetService godoc
//
//	@Summary		Get a service
//	@Description	Requires services:manage.
//	@Tags			Backoffice catalog
//	@Produce		json
//	@Param			id	path		string	true	"Service id"
//	@Success		200	{object}	dtos.ServiceView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"Service not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/services/{id} [get]
func (c *CatalogController) GetService(ctx *gin.Context) {
	id, ok := idParam(ctx, errServiceNotFound)
	if !ok {
		return
	}
	resp, err := c.catalog.GetService(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, resp, err)
}

// CreateService godoc
//
//	@Summary		Create a service
//	@Description	Requires services:manage. The slug cannot change later. Fees are in cêntimos of kwanza (1 Kz = 100).
//	@Tags			Backoffice catalog
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CreateServiceInput	true	"Service"
//	@Success		201		{object}	dtos.ServiceView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		409		{object}	shared.ErrorResponse	"Slug already used"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or unknown category"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/services [post]
func (c *CatalogController) CreateService(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var req dtos.CreateServiceInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.catalog.CreateService(ctx.Request.Context(), actor, req)
	respond(ctx, http.StatusCreated, resp, err)
}

// UpdateService godoc
//
//	@Summary		Change a service
//	@Description	Requires services:manage. Only the fields sent change; pricing, when sent, replaces the three fees. Setting is_active to false hides the service from the app; services are never deleted.
//	@Tags			Backoffice catalog
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Service id"
//	@Param			body	body		dtos.UpdateServiceInput	true	"Fields to change"
//	@Success		200		{object}	dtos.ServiceView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Service not found"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or unknown category"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/services/{id} [patch]
func (c *CatalogController) UpdateService(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, errServiceNotFound)
	if !ok {
		return
	}
	var req dtos.UpdateServiceInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.catalog.UpdateService(ctx.Request.Context(), actor, id, req)
	respond(ctx, http.StatusOK, resp, err)
}

type notFound struct{ code, message string }

var (
	errCategoryNotFound = notFound{"CATEGORY_NOT_FOUND", "Category not found."}
	errServiceNotFound  = notFound{"SERVICE_NOT_FOUND", "Service not found."}
)

func idParam(ctx *gin.Context, missing notFound) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, missing.code, missing.message)
		return uuid.Nil, false
	}
	return id, true
}

// staffActor is the audited author of a backoffice change. The routes run
// behind RequirePermission, so the session always has an access.
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

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrCategoryNotFound):
		shared.NotFound(ctx, errCategoryNotFound.code, errCategoryNotFound.message)
	case errors.Is(err, catalog.ErrUnknownCategory):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "UNKNOWN_CATEGORY", "The category does not exist.")
	case errors.Is(err, catalog.ErrServiceNotFound):
		shared.NotFound(ctx, errServiceNotFound.code, errServiceNotFound.message)
	case errors.Is(err, catalog.ErrCategorySlugTaken):
		shared.Conflict(ctx, "CATEGORY_SLUG_TAKEN", "Another category already uses this slug.")
	case errors.Is(err, catalog.ErrServiceSlugTaken):
		shared.Conflict(ctx, "SERVICE_SLUG_TAKEN", "Another service already uses this slug.")
	default:
		if validationErr, ok := customerrors.AsValidation(err); ok {
			shared.ValidationFailed(ctx, validationErr)
			return
		}
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
	}
}
