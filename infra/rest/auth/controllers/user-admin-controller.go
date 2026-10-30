package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type UserAdminController struct {
	userAdminUsecase *usecases.UserAdminUsecase
}

func NewUserAdminController(userAdminUsecase *usecases.UserAdminUsecase) *UserAdminController {
	return &UserAdminController{userAdminUsecase: userAdminUsecase}
}

// List godoc
//
//	@Summary		List customer and provider accounts
//	@Description	Requires users:read. Staff are managed as members.
//	@Tags			Backoffice users
//	@Produce		json
//	@Param			role	query		string	false	"Role"		Enums(customer, provider)
//	@Param			status	query		string	false	"Status"	Enums(active, inactive, blocked)
//	@Param			search	query		string	false	"Part of the name, e-mail or phone"
//	@Param			page	query		int		false	"Page (from 1)"
//	@Param			size	query		int		false	"Page size (max 100)"
//	@Success		200		{object}	dtos.UserPage
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid filters"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/users [get]
func (c *UserAdminController) List(ctx *gin.Context) {
	var query dtos.UserListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	resp, err := c.userAdminUsecase.List(ctx.Request.Context(), query)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// Get godoc
//
//	@Summary		Get a customer or provider account
//	@Description	Requires users:read.
//	@Tags			Backoffice users
//	@Produce		json
//	@Param			id	path		string	true	"User id"
//	@Success		200	{object}	dtos.UserView
//	@Failure		400	{object}	shared.ErrorResponse	"Invalid id"
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"User not found"
//	@Failure		500	{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/users/{id} [get]
func (c *UserAdminController) Get(ctx *gin.Context) {
	userID, ok := userIDParam(ctx)
	if !ok {
		return
	}

	resp, err := c.userAdminUsecase.Get(ctx.Request.Context(), userID)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// ListBlockReasons godoc
//
//	@Summary		List block reasons
//	@Description	Requires users:block. The active reasons an account can be blocked for. Send their ids in reason_ids when blocking.
//	@Tags			Backoffice users
//	@Produce		json
//	@Success		200	{object}	dtos.BlockReasonList
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		500	{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/block-reasons [get]
func (c *UserAdminController) ListBlockReasons(ctx *gin.Context) {
	resp, err := c.userAdminUsecase.ListBlockReasons(ctx.Request.Context())
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// Block godoc
//
//	@Summary		Block a user
//	@Description	Requires users:block. Ends every session of the customer or provider at once.
//	@Tags			Backoffice users
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"User id"
//	@Param			body	body		dtos.BlockUserInput	true	"Reasons from GET /api/v1/admin/block-reasons and an optional note"
//	@Success		200		{object}	dtos.UserView
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or own account"
//	@Failure		404		{object}	shared.ErrorResponse	"User not found"
//	@Failure		422		{object}	shared.ErrorResponse	"No reason given or unknown reason"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/users/{id}/block [post]
func (c *UserAdminController) Block(ctx *gin.Context) {
	actor, ok := requestActor(ctx)
	if !ok {
		return
	}
	userID, ok := userIDParam(ctx)
	if !ok {
		return
	}
	var req dtos.BlockUserInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	resp, err := c.userAdminUsecase.Block(ctx.Request.Context(), actor, userID, req)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// Unblock godoc
//
//	@Summary		Unblock a customer or provider account
//	@Description	Requires users:block.
//	@Tags			Backoffice users
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"User id"
//	@Param			body	body		dtos.UnblockUserInput	true	"Optional reason"
//	@Success		200		{object}	dtos.UserView
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or own account"
//	@Failure		404		{object}	shared.ErrorResponse	"User not found"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/users/{id}/unblock [post]
func (c *UserAdminController) Unblock(ctx *gin.Context) {
	actor, ok := requestActor(ctx)
	if !ok {
		return
	}
	userID, ok := userIDParam(ctx)
	if !ok {
		return
	}
	var req dtos.UnblockUserInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	resp, err := c.userAdminUsecase.Unblock(ctx.Request.Context(), actor, userID, req)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// ResetPassword godoc
//
//	@Summary		Reset the password of a user
//	@Description	Requires users:reset-password, for accounts that cannot use SMS recovery. Ends every session of the user at once.
//	@Tags			Backoffice users
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string					true	"User id"
//	@Param			body	body	dtos.ResetPasswordInput	true	"New password"
//	@Success		204		"No Content"
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"User not found"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid password"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/users/{id}/password [put]
func (c *UserAdminController) ResetPassword(ctx *gin.Context) {
	actor, ok := requestActor(ctx)
	if !ok {
		return
	}
	userID, ok := userIDParam(ctx)
	if !ok {
		return
	}
	var req dtos.ResetPasswordInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	if err := c.userAdminUsecase.ResetPassword(ctx.Request.Context(), actor, userID, req); err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}
