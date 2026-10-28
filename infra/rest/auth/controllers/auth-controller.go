package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

const genericCodeSentMessage = "If the account exists, a verification code was sent."

type AuthController struct {
	authUsecase              *usecases.AuthUsecase
	refreshUsecase           *usecases.RefreshUsecase
	phoneVerificationUsecase *usecases.PhoneVerificationUsecase
	recoveryUsecase          *usecases.RecoveryUsecase
}

func NewAuthController(
	authUsecase *usecases.AuthUsecase,
	refreshUsecase *usecases.RefreshUsecase,
	phoneVerificationUsecase *usecases.PhoneVerificationUsecase,
	recoveryUsecase *usecases.RecoveryUsecase,
) *AuthController {
	return &AuthController{
		authUsecase:              authUsecase,
		refreshUsecase:           refreshUsecase,
		phoneVerificationUsecase: phoneVerificationUsecase,
		recoveryUsecase:          recoveryUsecase,
	}
}

// Register godoc
//
//	@Summary		Register a new user
//	@Description	Creates an unverified account and sends a verification code by SMS. The role comes from the client: agora-customer-app creates a customer, agora-provider-app creates a provider. Other clients cannot register.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RegisterInput	true	"Register payload"
//	@Success		201		{object}	dtos.UserView
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Unknown client"
//	@Failure		403		{object}	shared.ErrorResponse	"Client does not allow registration"
//	@Failure		409		{object}	shared.ErrorResponse	"Email or phone already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Failure		429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/register [post]
func (c *AuthController) Register(ctx *gin.Context) {
	var req dtos.RegisterInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	resp, err := c.authUsecase.Register(ctx.Request.Context(), req, client)
	if err != nil {
		c.handleRegisterError(ctx, err)
		return
	}

	ctx.JSON(http.StatusCreated, resp)
}

func (c *AuthController) handleRegisterError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrRegistrationNotAllowed):
		shared.Forbidden(ctx, "REGISTRATION_NOT_ALLOWED", "This client does not allow self registration.")
	case errors.Is(err, usecases.ErrEmailAlreadyExists):
		shared.Conflict(ctx, "EMAIL_ALREADY_EXISTS", "Email already registered.")
	case errors.Is(err, usecases.ErrPhoneAlreadyExists):
		shared.Conflict(ctx, "PHONE_ALREADY_EXISTS", "Phone already registered.")
	default:
		handleCommonError(ctx, err)
	}
}

// Login godoc
//
//	@Summary		Authenticate a user
//	@Description	The identifier is an e-mail or a phone number in E.164 format. The account role must be accepted by the client: customers sign in through agora-customer-app, providers through agora-provider-app, partners through agora-partner-panel, operators and admins through agora-backoffice.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.LoginInput	true	"Login payload"
//	@Success		200		{object}	dtos.SessionOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Invalid credentials or unknown client"
//	@Failure		403		{object}	shared.ErrorResponse	"Account blocked, phone not verified or client not allowed"
//	@Failure		429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/login [post]
func (c *AuthController) Login(ctx *gin.Context) {
	var req dtos.LoginInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	resp, err := c.authUsecase.Login(ctx.Request.Context(), req, client, ctx.ClientIP(), ctx.Request.UserAgent())
	if err != nil {
		c.handleLoginError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

func (c *AuthController) handleLoginError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrInvalidCredentials):
		shared.Unauthorized(ctx, "INVALID_CREDENTIALS", "Invalid identifier or password.")
	case errors.Is(err, usecases.ErrPhoneNotVerified):
		shared.Forbidden(ctx, "PHONE_NOT_VERIFIED", "Phone number not verified.")
	default:
		handleSignInError(ctx, err)
	}
}

// Refresh godoc
//
//	@Summary		Refresh access token
//	@Description	Exchanges a valid refresh token for a new access token and rotates the refresh token. The refresh token only works with the client that created the session.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RefreshRequest	true	"Refresh payload"
//	@Success		200		{object}	dtos.SessionOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Invalid, expired, revoked or reused refresh token"
//	@Failure		403		{object}	shared.ErrorResponse	"Account blocked or token bound to another client"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/refresh [post]
func (c *AuthController) Refresh(ctx *gin.Context) {
	var req dtos.RefreshRequest
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	resp, err := c.refreshUsecase.Execute(ctx.Request.Context(), req, client, ctx.ClientIP(), ctx.Request.UserAgent())
	if err != nil {
		c.handleRefreshError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

func (c *AuthController) handleRefreshError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrInvalidRefreshToken):
		shared.Unauthorized(ctx, "INVALID_REFRESH_TOKEN", "Invalid refresh token.")
	case errors.Is(err, usecases.ErrRefreshTokenRevoked):
		shared.Unauthorized(ctx, "REFRESH_TOKEN_REVOKED", "Refresh token revoked.")
	case errors.Is(err, usecases.ErrRefreshTokenExpired):
		shared.Unauthorized(ctx, "REFRESH_TOKEN_EXPIRED", "Refresh token expired.")
	case errors.Is(err, usecases.ErrRefreshTokenReused):
		shared.Unauthorized(ctx, "REFRESH_TOKEN_REUSED", "Refresh token reuse detected.")
	case errors.Is(err, usecases.ErrRefreshClientMismatch):
		shared.Forbidden(ctx, "TOKEN_CLIENT_MISMATCH", "Session was not created for this client.")
	default:
		handleSignInError(ctx, err)
	}
}

// RequestPhoneVerification godoc
//
//	@Summary		Send a phone verification code
//	@Description	Always answers 202 so the response does not reveal whether the phone is registered.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.PhoneVerificationRequestInput	true	"Phone payload"
//	@Success		202		{object}	dtos.MessageOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Unknown client"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Failure		429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/phone/verification-code [post]
func (c *AuthController) RequestPhoneVerification(ctx *gin.Context) {
	var req dtos.PhoneVerificationRequestInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	if err := c.phoneVerificationUsecase.RequestCode(ctx.Request.Context(), req, client); err != nil {
		handleCommonError(ctx, err)
		return
	}

	ctx.JSON(http.StatusAccepted, dtos.MessageOutput{Message: genericCodeSentMessage})
}

// ConfirmPhoneVerification godoc
//
//	@Summary	Verify the phone and start a session
//	@Tags		Auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.PhoneVerificationConfirmInput	true	"Phone and code"
//	@Success	200		{object}	dtos.SessionOutput
//	@Failure	400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure	401		{object}	shared.ErrorResponse	"Unknown client"
//	@Failure	403		{object}	shared.ErrorResponse	"Account blocked or client not allowed"
//	@Failure	422		{object}	shared.ErrorResponse	"Invalid, expired or exhausted code"
//	@Failure	429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure	500		{object}	shared.ErrorResponse	"Internal error"
//	@Security	ClientId
//	@Router		/api/v1/auth/phone/verify [post]
func (c *AuthController) ConfirmPhoneVerification(ctx *gin.Context) {
	var req dtos.PhoneVerificationConfirmInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	resp, err := c.phoneVerificationUsecase.Confirm(ctx.Request.Context(), req, client, ctx.ClientIP(), ctx.Request.UserAgent())
	if err != nil {
		handleSignInError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// RequestRecovery godoc
//
//	@Summary		Request password recovery
//	@Description	Sends a recovery code by SMS. Always answers 202 so the response does not reveal whether the account exists.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RecoveryRequestInput	true	"Recovery request payload"
//	@Success		202		{object}	dtos.MessageOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Unknown client"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields"
//	@Failure		429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/recovery/request [post]
func (c *AuthController) RequestRecovery(ctx *gin.Context) {
	var req dtos.RecoveryRequestInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	if err := c.recoveryUsecase.RequestRecovery(ctx.Request.Context(), req, client); err != nil {
		handleCommonError(ctx, err)
		return
	}

	ctx.JSON(http.StatusAccepted, dtos.MessageOutput{Message: genericCodeSentMessage})
}

// ConfirmRecovery godoc
//
//	@Summary		Confirm recovery and set a new password
//	@Description	Ends every session of the user.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.RecoveryConfirmInput	true	"Recovery confirm payload"
//	@Success		200		{object}	dtos.MessageOutput
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request or missing client"
//	@Failure		401		{object}	shared.ErrorResponse	"Unknown client"
//	@Failure		403		{object}	shared.ErrorResponse	"Client not allowed"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid code or password"
//	@Failure		429		{object}	shared.ErrorResponse	"Too many requests"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		ClientId
//	@Router			/api/v1/auth/recovery/confirm [post]
func (c *AuthController) ConfirmRecovery(ctx *gin.Context) {
	var req dtos.RecoveryConfirmInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	client, ok := requestClient(ctx)
	if !ok {
		return
	}

	if err := c.recoveryUsecase.ConfirmRecovery(ctx.Request.Context(), req, client); err != nil {
		handleSignInError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dtos.MessageOutput{Message: "Password updated. Please sign in again."})
}

// Logout godoc
//
//	@Summary		Logout current session
//	@Description	Revokes the refresh tokens of the current session.
//	@Tags			Auth
//	@Produce		json
//	@Success		204	"No Content"
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized"
//	@Failure		403	{object}	shared.ErrorResponse	"Token bound to another client"
//	@Failure		500	{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/auth/logout [post]
func (c *AuthController) Logout(ctx *gin.Context) {
	sessionID, ok := middleware.SessionIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}

	if err := c.authUsecase.Logout(ctx.Request.Context(), sessionID); err != nil {
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

// LogoutAll godoc
//
//	@Summary		Logout from all sessions
//	@Description	Revokes every refresh token of the authenticated user, in every client.
//	@Tags			Auth
//	@Produce		json
//	@Success		204	"No Content"
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized"
//	@Failure		403	{object}	shared.ErrorResponse	"Token bound to another client"
//	@Failure		500	{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/auth/logout-all [post]
func (c *AuthController) LogoutAll(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}

	if err := c.authUsecase.LogoutAll(ctx.Request.Context(), userID); err != nil {
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

// Me godoc
//
//	@Summary	Get the authenticated user
//	@Tags		Users
//	@Produce	json
//	@Success	200	{object}	dtos.UserView
//	@Failure	401	{object}	shared.ErrorResponse	"Unauthorized"
//	@Failure	403	{object}	shared.ErrorResponse	"Token bound to another client"
//	@Failure	404	{object}	shared.ErrorResponse	"User not found"
//	@Failure	500	{object}	shared.ErrorResponse	"Internal error"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me [get]
func (c *AuthController) Me(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}

	resp, err := c.authUsecase.Me(ctx.Request.Context(), userID)
	if err != nil {
		handleCommonError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, resp)
}

// ChangePassword godoc
//
//	@Summary		Change the password of the authenticated user
//	@Description	Ends every session of the user.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			body	body	dtos.ChangePasswordInput	true	"Change password payload"
//	@Success		204		"No Content"
//	@Failure		400		{object}	shared.ErrorResponse	"Invalid request"
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized"
//	@Failure		403		{object}	shared.ErrorResponse	"Token bound to another client"
//	@Failure		422		{object}	shared.ErrorResponse	"Wrong current password or invalid new password"
//	@Failure		500		{object}	shared.ErrorResponse	"Internal error"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/password [put]
func (c *AuthController) ChangePassword(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}

	var req dtos.ChangePasswordInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}

	if err := c.authUsecase.ChangePassword(ctx.Request.Context(), userID, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, usecases.ErrInvalidCurrentPassword):
			shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_CURRENT_PASSWORD", "Current password is incorrect.")
		default:
			handleCommonError(ctx, err)
		}
		return
	}

	ctx.Status(http.StatusNoContent)
}

func requestClient(ctx *gin.Context) (*entities.Client, bool) {
	client, ok := middleware.ClientFromContext(ctx.Request.Context())
	if !ok {
		shared.BadRequest(ctx, "UNKNOWN_CLIENT", "Missing or unknown client.")
	}
	return client, ok
}

func handleSignInError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, usecases.ErrClientNotAllowed):
		shared.Forbidden(ctx, "CLIENT_NOT_ALLOWED", "This account cannot sign in through this client.")
	case errors.Is(err, usecases.ErrAccountBlocked):
		shared.Forbidden(ctx, "ACCOUNT_BLOCKED", "Account is blocked.")
	case errors.Is(err, usecases.ErrAccountInactive):
		shared.Forbidden(ctx, "ACCOUNT_INACTIVE", "Account is not active.")
	case errors.Is(err, usecases.ErrInvalidOTPCode):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_CODE", "The code is incorrect.")
	case errors.Is(err, usecases.ErrOTPExpired):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "CODE_EXPIRED", "The code expired or was not requested.")
	case errors.Is(err, usecases.ErrOTPMaxAttempts):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "CODE_ATTEMPTS_EXCEEDED", "Too many attempts. Request a new code.")
	case errors.Is(err, usecases.ErrSessionCreate), errors.Is(err, usecases.ErrTokenSigning):
		shared.InternalServerError(ctx, "AUTH_INTERNAL_ERROR", "An internal error occurred.", err)
	default:
		handleCommonError(ctx, err)
	}
}

func handleCommonError(ctx *gin.Context, err error) {
	if validationErr, ok := customerrors.AsValidation(err); ok {
		shared.ValidationFailed(ctx, validationErr)
		return
	}

	switch {
	case errors.Is(err, usecases.ErrPasswordTooShort):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_PASSWORD", "Password must be at least 8 characters.")
	case errors.Is(err, usecases.ErrPasswordTooLong):
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "INVALID_PASSWORD", "Password must be at most 128 characters.")
	case errors.Is(err, usecases.ErrUserNotFound):
		shared.NotFound(ctx, "USER_NOT_FOUND", "User not found.")
	case errors.Is(err, services.ErrSMSUnavailable):
		shared.ServiceUnavailable(ctx, "SMS_UNAVAILABLE", "SMS is temporarily unavailable.")
	default:
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
	}
}
