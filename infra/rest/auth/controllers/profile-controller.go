package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type ProfileController struct {
	profileUsecase *usecases.ProfileUsecase
}

func NewProfileController(profileUsecase *usecases.ProfileUsecase) *ProfileController {
	return &ProfileController{profileUsecase: profileUsecase}
}

// UpdateProfile godoc
//
//	@Summary		Update the profile of the authenticated user
//	@Description	Changes only the fields sent. Changing or removing the e-mail requires current_password or a code from /users/me/verification-code. The phone number changes through /users/me/phone.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.UpdateProfileInput	true	"Profile fields"
//	@Success		200		{object}	dtos.UserView
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Token bound to another client"
//	@Failure		409		{object}	shared.ErrorResponse	"Email already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields, missing confirmation, wrong password or code"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me [patch]
func (c *ProfileController) UpdateProfile(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return
	}
	var req dtos.UpdateProfileInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	resp, err := c.profileUsecase.UpdateProfile(ctx.Request.Context(), userID, req)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// RequestIdentityCode godoc
//
//	@Summary		Send a confirmation code to the account phone
//	@Description	The code confirms an e-mail change or the account deletion when the user has no password. Customers and providers only.
//	@Tags			Users
//	@Produce		json
//	@Success		202	{object}	dtos.MessageOutput
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Staff accounts confirm with the password"
//	@Failure		429	{object}	shared.ErrorResponse	"Code sent recently or too many requests"
//	@Failure		503	{object}	shared.ErrorResponse	"SMS unavailable"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/verification-code [post]
func (c *ProfileController) RequestIdentityCode(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return
	}

	if err := c.profileUsecase.RequestIdentityCode(ctx.Request.Context(), userID); err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusAccepted, dtos.MessageOutput{Message: "A confirmation code was sent to the account phone."})
}

// RequestPhoneChange godoc
//
//	@Summary		Send a code to a new phone number
//	@Description	Sends an SMS code to the new number. Only the signed-in user who asked for it can use the code.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.PhoneChangeRequestInput	true	"New phone"
//	@Success		202		{object}	dtos.MessageOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		409		{object}	shared.ErrorResponse	"Phone already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid phone or same as the current one"
//	@Failure		429		{object}	shared.ErrorResponse	"Code sent recently or too many requests"
//	@Failure		503		{object}	shared.ErrorResponse	"SMS unavailable"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/phone/verification-code [post]
func (c *ProfileController) RequestPhoneChange(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return
	}
	var req dtos.PhoneChangeRequestInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	if err := c.profileUsecase.RequestPhoneChange(ctx.Request.Context(), userID, req); err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusAccepted, dtos.MessageOutput{Message: "A verification code was sent to the new number."})
}

// ConfirmPhoneChange godoc
//
//	@Summary	Confirm the new phone number with the code
//	@Tags		Users
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.PhoneChangeConfirmInput	true	"New phone and code"
//	@Success	200		{object}	dtos.UserView
//	@Failure	400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure	401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure	409		{object}	shared.ErrorResponse	"Phone already registered"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid, expired or exhausted code"
//	@Failure	429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure	500		{object}	shared.ErrorResponse	"Internal error"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/phone [put]
func (c *ProfileController) ConfirmPhoneChange(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return
	}
	var req dtos.PhoneChangeConfirmInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	resp, err := c.profileUsecase.ConfirmPhoneChange(ctx.Request.Context(), userID, req)
	if err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// DeleteAccount godoc
//
//	@Summary		Delete the account of the authenticated user
//	@Description	Customers and providers only. Confirm with password or, without a password, with a code from /users/me/verification-code. Ends every session at once; the phone and e-mail can be registered again.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			body	body	dtos.DeleteAccountInput	true	"Password or code"
//	@Success		204		"No Content"
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Staff accounts cannot be deleted here"
//	@Failure		409		{object}	shared.ErrorResponse	"Account has no password"
//	@Failure		422		{object}	shared.ErrorResponse	"Missing confirmation, wrong password or code"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me [delete]
func (c *ProfileController) DeleteAccount(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		return
	}
	var req dtos.DeleteAccountInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	if err := c.profileUsecase.DeleteAccount(ctx.Request.Context(), userID, req); err != nil {
		handleAccountError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}
