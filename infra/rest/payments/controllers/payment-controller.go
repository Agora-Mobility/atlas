package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/payments"
	"github.com/Agora-Mobility/atlas/usecases/payments/dtos"
)

const maxWebhookBytes = 1 << 16

type PaymentController struct {
	payments *payments.Usecase
}

func NewPaymentController(uc *payments.Usecase) *PaymentController {
	return &PaymentController{payments: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{payments.ErrRequestNotFound, http.StatusNotFound, "REQUEST_NOT_FOUND", "Request not found."},
	{payments.ErrPaymentNotFound, http.StatusNotFound, "PAYMENT_NOT_FOUND", "Payment not found."},
	{payments.ErrRefundDeclined, http.StatusUnprocessableEntity, "REFUND_DECLINED", "The gateway did not accept the refund."},
	{entities.ErrPaymentMethodUnavailable, http.StatusUnprocessableEntity, "PAYMENT_METHOD_UNAVAILABLE", "This payment method is not available for this service."},
	{entities.ErrPaymentNotPayable, http.StatusConflict, "REQUEST_NOT_PAYABLE", "This request cannot be paid now."},
	{entities.ErrPaymentAlreadyPaid, http.StatusConflict, "ALREADY_PAID", "This request is already paid."},
	{entities.ErrPaymentPendingElsewhere, http.StatusConflict, "PAYMENT_PENDING", "This request has a payment waiting for the customer."},
	{entities.ErrWalletInsufficient, http.StatusConflict, "WALLET_INSUFFICIENT", "The wallet does not have enough balance."},
	{entities.ErrSettlementTooLarge, http.StatusUnprocessableEntity, "SETTLEMENT_TOO_LARGE", "The settlement is above the balance of the provider."},
	{entities.ErrInvalidLedgerAmount, http.StatusUnprocessableEntity, "INVALID_AMOUNT", "The amount is not valid."},
	{entities.ErrGatewayFeeRecorded, http.StatusConflict, "GATEWAY_FEE_RECORDED", "The gateway fee of this payment is already recorded."},
	{entities.ErrGatewayFeeNotPaid, http.StatusConflict, "GATEWAY_FEE_NOT_APPLICABLE", "Only a paid digital payment has a gateway fee."},
	{payments.ErrProviderNotFound, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider not found."},
	{entities.ErrTipNotAllowed, http.StatusConflict, "TIP_NOT_ALLOWED", "A tip needs a completed request with a provider."},
	{entities.ErrTipOutOfRange, http.StatusUnprocessableEntity, "TIP_OUT_OF_RANGE", "The tip is outside the allowed values."},
	{entities.ErrTipAlreadyGiven, http.StatusConflict, "TIP_ALREADY_GIVEN", "This request already has a tip."},
	{entities.ErrCashNotExpected, http.StatusConflict, "CASH_NOT_EXPECTED", "This request is not paid in cash."},
	{entities.ErrPaymentMethodLocked, http.StatusConflict, "PAYMENT_METHOD_LOCKED", "The method of this request cannot change now."},
	{entities.ErrRefundNotSupported, http.StatusUnprocessableEntity, "REFUND_NOT_SUPPORTED", "This payment cannot be refunded through the gateway."},
	{entities.ErrRefundNeedsCancel, http.StatusConflict, "REFUND_NEEDS_CANCEL", "This request was paid ahead and is still open. Cancel it before refunding all of it."},
	{entities.ErrRefundTooLarge, http.StatusUnprocessableEntity, "REFUND_TOO_LARGE", "The refund is above what is left of the payment."},
	{services.ErrPaymentsUnavailable, http.StatusServiceUnavailable, "PAYMENTS_UNAVAILABLE", "Payments are not available."},
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
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

func userWithRole(ctx *gin.Context, role enums.Role) (uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	current, _ := middleware.RoleFromContext(reqCtx)
	if current != role {
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
		return uuid.Nil, false
	}
	return userID, true
}

func staffActor(ctx *gin.Context) (entities.Actor, bool) {
	reqCtx := ctx.Request.Context()
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return entities.Actor{}, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	return access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent()), true
}

func requestParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "REQUEST_NOT_FOUND", "Request not found.")
		return uuid.Nil, false
	}
	return id, true
}

func paymentParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "PAYMENT_NOT_FOUND", "Payment not found.")
		return uuid.Nil, false
	}
	return id, true
}

// ListMethods godoc
//
//	@Summary		Payment methods of a category
//	@Description	Only what can be used now. Fills the method selector before and after a request.
//	@Tags			Payments
//	@Produce		json
//	@Param			category	query		string	false	"Category slug, such as taxi"
//	@Success		200			{object}	dtos.MethodList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/payment-methods [get]
func (c *PaymentController) ListMethods(ctx *gin.Context) {
	list, err := c.payments.Methods(ctx.Request.Context(), ctx.Query("category"), true)
	respond(ctx, http.StatusOK, list, err)
}

// ChangeMethod godoc
//
//	@Summary		Change the payment method of a request
//	@Description	Customer only. After the request is completed it cannot change to cash, and it cannot change while a payment is pending.
//	@Tags			Payments
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.ChangeMethodInput	true	"Method"
//	@Success		200		{object}	dtos.ChangeMethodInput
//	@Failure		404		{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"PAYMENT_METHOD_LOCKED"
//	@Failure		422		{object}	shared.ErrorResponse	"PAYMENT_METHOD_UNAVAILABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/payment-method [put]
func (c *PaymentController) ChangeMethod(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.ChangeMethodInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.payments.ChangeMethod(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// Pay godoc
//
//	@Summary		Pay a completed request
//	@Description	Customer only. gpo asks for approval in the Multicaixa Express app of phone_number, or of the phone of the account when empty. reference returns the number and entity to pay with. The amount always comes from the request. Follow the result with the request.payment event or GET /requests/{id}/payments. Sending the same method again returns the pending payment.
//	@Tags			Payments
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Request id"
//	@Param			body	body		dtos.PayInput	true	"Method"
//	@Success		201		{object}	dtos.PaymentView
//	@Success		200		{object}	dtos.PaymentView		"The payment already pending"
//	@Failure		404		{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Failure		409		{object}	shared.ErrorResponse	"REQUEST_NOT_PAYABLE, ALREADY_PAID or PAYMENT_PENDING"
//	@Failure		422		{object}	shared.ErrorResponse	"PAYMENT_METHOD_UNAVAILABLE"
//	@Failure		503		{object}	shared.ErrorResponse	"PAYMENTS_UNAVAILABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/payments [post]
func (c *PaymentController) Pay(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.PayInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, created, err := c.payments.Pay(ctx.Request.Context(), customerID, id, in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(ctx, status, view, err)
}

// ConfirmCash godoc
//
//	@Summary		Confirm that the cash was received
//	@Description	The provider of the completed request only. Repeating it returns the same payment.
//	@Tags			Payments
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.PaymentView
//	@Failure		404	{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Failure		409	{object}	shared.ErrorResponse	"REQUEST_NOT_PAYABLE, ALREADY_PAID or PAYMENT_PENDING"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/payments/cash [post]
func (c *PaymentController) ConfirmCash(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.payments.ConfirmCash(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// ListOfRequest godoc
//
//	@Summary		Payments of a request
//	@Description	The customer or the provider of the request. The provider does not see the phone or the reference.
//	@Tags			Payments
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.PaymentList
//	@Failure		404	{object}	shared.ErrorResponse	"REQUEST_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/payments [get]
func (c *PaymentController) ListOfRequest(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	list, err := c.payments.ListOfRequest(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, list, err)
}

type webhookPayload struct {
	ID                    string `json:"id"`
	MerchantTransactionID string `json:"merchantTransactionId"`
}

// Webhook godoc
//
//	@Summary		AppyPay payment notification
//	@Description	Called by AppyPay. The body is only a notice: the state always comes from asking AppyPay for the charge.
//	@Tags			Payments
//	@Accept			json
//	@Param			body	body	object	true	"AppyPay notification"
//	@Success		204
//	@Failure		400	{object}	shared.ErrorResponse
//	@Router			/api/v1/webhooks/appypay [post]
func (c *PaymentController) Webhook(ctx *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxWebhookBytes))
	if err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Unreadable body.")
		return
	}
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil || (payload.ID == "" && payload.MerchantTransactionID == "") {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid notification.")
		return
	}
	if err := c.payments.OnWebhook(ctx.Request.Context(), payload.ID, payload.MerchantTransactionID); err != nil {
		shared.ServiceUnavailable(ctx, "PAYMENT_NOT_CONFIRMED", "The payment could not be confirmed yet.")
		return
	}
	ctx.Status(http.StatusNoContent)
}

// AdminMethods godoc
//
//	@Summary		Payment methods per category
//	@Description	Requires services:manage.
//	@Tags			Backoffice payments
//	@Produce		json
//	@Param			category	query		string	false	"Category slug"
//	@Success		200			{object}	dtos.MethodList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payment-methods [get]
func (c *PaymentController) AdminMethods(ctx *gin.Context) {
	list, err := c.payments.Methods(ctx.Request.Context(), ctx.Query("category"), false)
	respond(ctx, http.StatusOK, list, err)
}

// SetMethod godoc
//
//	@Summary		Turn a payment method on or off for a category
//	@Description	Requires services:manage.
//	@Tags			Backoffice payments
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.SetMethodInput	true	"Setting"
//	@Success		200		{object}	dtos.MethodList
//	@Failure		422		{object}	shared.ErrorResponse	"PAYMENT_METHOD_UNAVAILABLE"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payment-methods [put]
func (c *PaymentController) SetMethod(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.SetMethodInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	list, err := c.payments.SetMethod(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusOK, list, err)
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

func adminFilter(ctx *gin.Context) (dtos.AdminFilter, bool) {
	filter := dtos.AdminFilter{Method: ctx.Query("method"), Category: ctx.Query("category")}
	if statuses := ctx.Query("status"); statuses != "" {
		filter.Statuses = strings.Split(statuses, ",")
	}
	if raw := ctx.Query("request_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid request_id.")
			return filter, false
		}
		filter.RequestID = &id
	}
	var ok bool
	if filter.From, ok = queryTime(ctx, "from"); !ok {
		return filter, false
	}
	if filter.To, ok = queryTime(ctx, "to"); !ok {
		return filter, false
	}
	return filter, true
}

// AdminList godoc
//
//	@Summary		List payments
//	@Description	Requires finance:read. Newest first.
//	@Tags			Backoffice payments
//	@Produce		json
//	@Param			status		query		string	false	"pending, paid, failed or expired, separated by commas"
//	@Param			method		query		string	false	"cash, gpo or reference"
//	@Param			category	query		string	false	"Category slug"
//	@Param			request_id	query		string	false	"Request"
//	@Param			from		query		string	false	"Created at or after"
//	@Param			to			query		string	false	"Created before"
//	@Param			page		query		int		false	"Page"
//	@Param			size		query		int		false	"Page size"
//	@Success		200			{object}	dtos.AdminPaymentList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payments [get]
func (c *PaymentController) AdminList(ctx *gin.Context) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	filter, ok := adminFilter(ctx)
	if !ok {
		return
	}
	page = page.Normalize()
	items, total, err := c.payments.AdminList(ctx.Request.Context(), filter, int(page.Limit()), int(page.Offset()))
	respond(ctx, http.StatusOK, &dtos.AdminPaymentList{Items: items, Total: total, Page: page.Page, Size: page.Size}, err)
}

// AdminGet godoc
//
//	@Summary		Get a payment with its history and refunds
//	@Description	Requires finance:read.
//	@Tags			Backoffice payments
//	@Produce		json
//	@Param			id	path		string	true	"Payment id"
//	@Success		200	{object}	dtos.AdminPaymentDetail
//	@Failure		404	{object}	shared.ErrorResponse	"PAYMENT_NOT_FOUND"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payments/{id} [get]
func (c *PaymentController) AdminGet(ctx *gin.Context) {
	id, ok := paymentParam(ctx)
	if !ok {
		return
	}
	detail, err := c.payments.AdminGet(ctx.Request.Context(), id)
	respond(ctx, http.StatusOK, detail, err)
}

// Refund godoc
//
//	@Summary		Refund a payment
//	@Description	Requires refunds:approve. Only gpo payments: AppyPay does not refund references and cash stays out of the gateway. Leave amount_cents at 0 to refund all that is left.
//	@Tags			Backoffice payments
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string				true	"Payment id"
//	@Param			body			body		dtos.RefundInput	true	"Refund"
//	@Param			Idempotency-Key	header		string				true	"Unique per refund"
//	@Success		200				{object}	dtos.PaymentView
//	@Failure		404				{object}	shared.ErrorResponse	"PAYMENT_NOT_FOUND"
//	@Failure		422				{object}	shared.ErrorResponse	"REFUND_NOT_SUPPORTED, REFUND_TOO_LARGE or REFUND_DECLINED"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payments/{id}/refund [post]
func (c *PaymentController) Refund(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := paymentParam(ctx)
	if !ok {
		return
	}
	var in dtos.RefundInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.payments.Refund(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}
