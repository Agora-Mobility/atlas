package experience

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

// PublicVouchers godoc
//
//	@Summary	Vouchers open to everyone
//	@Tags		Vouchers
//	@Produce	json
//	@Param		category	query		string	false	"Category slug"
//	@Success	200			{object}	dtos.VoucherList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/vouchers [get]
func (c *Controller) PublicVouchers(ctx *gin.Context) {
	list, err := c.uc.PublicVouchers(ctx.Request.Context(), ctx.Query("category"))
	respond(ctx, http.StatusOK, list, err)
}

// AdminVouchers godoc
//
//	@Summary	List vouchers
//	@Tags		Backoffice
//	@Produce	json
//	@Param		page	query		int	false	"Page"
//	@Param		size	query		int	false	"Page size"
//	@Success	200		{object}	dtos.VoucherList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/vouchers [get]
func (c *Controller) AdminVouchers(ctx *gin.Context) {
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	list, err := c.uc.ListVouchers(ctx.Request.Context(), page)
	respond(ctx, http.StatusOK, list, err)
}

// AdminCreateVoucher godoc
//
//	@Summary		Create a voucher
//	@Description	A percentage in basis points or a value in cents, never both. Limited to a service or category when given, and always paid by AGORA.
//	@Tags			Backoffice
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.VoucherInput	true	"Voucher"
//	@Success		201		{object}	dtos.VoucherView
//	@Failure		409		{object}	shared.ErrorResponse	"The code exists"
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/vouchers [post]
func (c *Controller) AdminCreateVoucher(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var in dtos.VoucherInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.CreateVoucher(ctx.Request.Context(), actor, in)
	respond(ctx, http.StatusCreated, view, err)
}

// AdminUpdateVoucher godoc
//
//	@Summary		Change a voucher
//	@Description	The code and the service of a voucher do not change.
//	@Tags			Backoffice
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Voucher id"
//	@Param			body	body		dtos.VoucherInput	true	"Voucher"
//	@Success		200		{object}	dtos.VoucherView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/vouchers/{id} [put]
func (c *Controller) AdminUpdateVoucher(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "id", "VOUCHER_NOT_FOUND", "Voucher not found.")
	if !ok {
		return
	}
	var in dtos.VoucherInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.UpdateVoucher(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}
