package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/payments/dtos"
)

func pageParams(ctx *gin.Context) (pagination.Params, bool) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return page, false
	}
	return page, true
}

func providerParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "PROVIDER_NOT_FOUND", "Provider not found.")
		return uuid.Nil, false
	}
	return id, true
}

// Wallet godoc
//
//	@Summary		Wallet of the customer
//	@Description	Balance and movements: refunds of references, payments from the wallet and invite rewards.
//	@Tags			Payments
//	@Produce		json
//	@Param			page	query		int	false	"Page"
//	@Param			size	query		int	false	"Page size"
//	@Success		200		{object}	dtos.AccountStatement
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/wallet [get]
func (c *PaymentController) Wallet(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	view, err := c.payments.Wallet(ctx.Request.Context(), customerID, page)
	respond(ctx, http.StatusOK, view, err)
}

// MyLedger godoc
//
//	@Summary		Statement of the provider
//	@Description	A positive balance is what AGORA owes the provider. A negative balance is the commission the provider owes for cash jobs.
//	@Tags			Provider work
//	@Produce		json
//	@Param			page	query		int	false	"Page"
//	@Param			size	query		int	false	"Page size"
//	@Success		200		{object}	dtos.AccountStatement
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/ledger [get]
func (c *PaymentController) MyLedger(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	view, err := c.payments.ProviderLedger(ctx.Request.Context(), providerID, page)
	respond(ctx, http.StatusOK, view, err)
}

// ProviderLedger godoc
//
//	@Summary	Statement of a provider
//	@Tags		Backoffice
//	@Produce	json
//	@Param		id		path		string	true	"Provider id"
//	@Param		page	query		int		false	"Page"
//	@Param		size	query		int		false	"Page size"
//	@Success	200		{object}	dtos.AccountStatement
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/providers/{id}/ledger [get]
func (c *PaymentController) ProviderLedger(ctx *gin.Context) {
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	view, err := c.payments.ProviderLedger(ctx.Request.Context(), id, page)
	respond(ctx, http.StatusOK, view, err)
}

// Settle godoc
//
//	@Summary		Settle the balance of a provider
//	@Description	payout pays what AGORA owes and collect receives the debt of the provider. reference is the bank document: the same reference is applied only once.
//	@Tags			Backoffice
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Provider id"
//	@Param			body	body		dtos.SettlementInput	true	"Settlement"
//	@Success		200		{object}	dtos.SettlementResult
//	@Failure		422		{object}	shared.ErrorResponse	"Above the balance"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/providers/{id}/settlements [post]
func (c *PaymentController) Settle(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := providerParam(ctx)
	if !ok {
		return
	}
	var in dtos.SettlementInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.payments.Settle(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// SetGatewayFee godoc
//
//	@Summary		Record the real fee of the gateway
//	@Description	The value comes from the AppyPay statement and is never assumed. It is a cost of the platform and can be recorded once per payment.
//	@Tags			Backoffice
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Payment id"
//	@Param			body	body		dtos.GatewayFeeInput	true	"Fee"
//	@Success		200		{object}	dtos.PaymentView
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/payments/{id}/gateway-fee [put]
func (c *PaymentController) SetGatewayFee(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := paymentParam(ctx)
	if !ok {
		return
	}
	var in dtos.GatewayFeeInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.payments.SetGatewayFee(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// LedgerSummary godoc
//
//	@Summary		Totals of the ledger
//	@Description	Per account, the balances of the providers and how many transactions do not add up to zero, which has to be 0.
//	@Tags			Backoffice
//	@Produce		json
//	@Param			page	query		int	false	"Page of providers"
//	@Param			size	query		int	false	"Page size"
//	@Success		200		{object}	dtos.LedgerSummary
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/ledger/summary [get]
func (c *PaymentController) LedgerSummary(ctx *gin.Context) {
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	view, err := c.payments.LedgerSummary(ctx.Request.Context(), page)
	respond(ctx, http.StatusOK, view, err)
}

// Tip godoc
//
//	@Summary		Give a tip to the provider
//	@Description	Customer of a completed request, once. All of it goes to the provider, without commission. Paid digitally even when the request was paid in cash. The amount cannot pass the maximum in GET /rating-options.
//	@Tags			Ratings
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Request id"
//	@Param			body	body		dtos.TipInput	true	"Tip"
//	@Success		201		{object}	dtos.PaymentView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse	"Not completed, or a tip was already given"
//	@Failure		422		{object}	shared.ErrorResponse	"Out of range"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/tips [post]
func (c *PaymentController) Tip(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.TipInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, created, err := c.payments.Tip(ctx.Request.Context(), customerID, id, in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(ctx, status, view, err)
}
