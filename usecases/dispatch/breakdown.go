package dispatch

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

var adjustmentLabels = map[entities.FareAdjustmentReason]string{
	entities.AdjustmentBoost:       "Aumento da oferta",
	entities.AdjustmentWaiting:     "Tempo de espera",
	entities.AdjustmentRouteChange: "Alteração de rota",
	entities.AdjustmentDemand:      "Procura alta",
}

func (u *Usecase) breakdownOf(ctx context.Context, request *entities.ServiceRequest, adjustments []entities.FareAdjustment) (*dtos.FareBreakdown, error) {
	view := &dtos.FareBreakdown{Currency: request.Fare.Currency, Method: string(request.PaymentMethod), DiscountCents: request.DiscountCents, VoucherCode: request.VoucherCode, CancelFeeCents: request.CancelFeeCents}
	if request.Fare.Cents != nil {
		view.FareCents = *request.Fare.Cents
		base := view.FareCents
		for i := range adjustments {
			base -= adjustments[i].AmountCents
		}
		stopFee, err := u.stopFeeOf(ctx, request)
		if err != nil {
			return nil, err
		}
		if stopFee > 0 {
			base -= stopFee
		}
		view.Items = append(view.Items, dtos.BreakdownItem{Code: "fare", Label: "Tarifa do serviço", AmountCents: base})
		if stopFee > 0 {
			view.Items = append(view.Items, dtos.BreakdownItem{Code: "stops", Label: "Paragens", AmountCents: stopFee})
		}
		for i := range adjustments {
			view.Items = append(view.Items, dtos.BreakdownItem{Code: string(adjustments[i].Reason), Label: adjustmentLabels[adjustments[i].Reason], AmountCents: adjustments[i].AmountCents})
		}
	}
	if request.DiscountCents > 0 {
		view.Items = append(view.Items, dtos.BreakdownItem{Code: "voucher", Label: "Voucher " + request.VoucherCode, AmountCents: -request.DiscountCents})
	}
	view.TotalCents = view.FareCents - request.DiscountCents
	if request.Status == entities.RequestCancelled {
		view.TotalCents = request.CancelFeeCents
	}
	payments, err := u.Payments.ListOfRequest(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	for i := range payments {
		payment := &payments[i]
		if payment.Status != entities.PaymentPaid {
			continue
		}
		if payment.Duplicate {
			continue
		}
		if payment.Kind == entities.PaymentKindTip {
			view.TipCents += payment.AmountCents
			continue
		}
		view.PaidCents += payment.AmountCents
		view.RefundedCents += payment.RefundedCents
	}
	return view, nil
}

func (u *Usecase) stopFeeOf(ctx context.Context, request *entities.ServiceRequest) (int64, error) {
	if len(request.Stops) == 0 || request.Fare.EstimateID == nil {
		return 0, nil
	}
	estimate, err := u.Pricing.FindEstimate(ctx, *request.Fare.EstimateID)
	if err != nil || estimate == nil {
		return 0, err
	}
	tariff, err := u.Pricing.FindTariff(ctx, estimate.TariffID)
	if err != nil || tariff == nil {
		return 0, err
	}
	return int64(len(request.Stops)) * tariff.StopFeeCents, nil
}

func (u *Usecase) applyVoucher(ctx context.Context, request *entities.ServiceRequest, service *entities.DispatchService, code string, now time.Time) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	if request.Fare.Cents == nil {
		return entities.ErrVoucherNotApplicable
	}
	voucher, err := u.Vouchers.LockByCode(ctx, code)
	if err != nil {
		return err
	}
	if voucher == nil {
		return entities.ErrVoucherNotFound
	}
	if err := voucher.CheckApplies(service, now); err != nil {
		return err
	}
	if voucher.MaxRedemptions != nil {
		used, err := u.Vouchers.CountRedemptions(ctx, voucher.ID)
		if err != nil {
			return err
		}
		if used >= int64(*voucher.MaxRedemptions) {
			return entities.ErrVoucherUsed
		}
	}
	mine, err := u.Vouchers.CountCustomerRedemptions(ctx, voucher.ID, request.CustomerID)
	if err != nil {
		return err
	}
	if mine >= int64(voucher.PerCustomerLimit) {
		return entities.ErrVoucherUsed
	}
	discount, err := voucher.DiscountFor(*request.Fare.Cents)
	if err != nil {
		return err
	}
	if err := u.Vouchers.Redeem(ctx, voucher.ID, request.CustomerID, &request.ID, discount); err != nil {
		return err
	}
	request.VoucherID, request.VoucherCode, request.DiscountCents = &voucher.ID, *voucher.Code, discount
	return nil
}

func (u *Usecase) repriceVoucher(ctx context.Context, request *entities.ServiceRequest, fareCents int64) error {
	if request.VoucherID == nil {
		return nil
	}
	voucher, err := u.Vouchers.Find(ctx, *request.VoucherID)
	if err != nil || voucher == nil {
		return err
	}
	discount, err := voucher.DiscountFor(fareCents)
	if errors.Is(err, entities.ErrVoucherBelowMinimum) {
		request.DiscountCents = 0
		return nil
	}
	if err != nil {
		return err
	}
	request.DiscountCents = discount
	return nil
}

func (u *Usecase) PreviewVoucher(ctx context.Context, customerID uuid.UUID, in dtos.VoucherPreviewInput) (*dtos.VoucherPreview, error) {
	estimate, err := u.Pricing.FindEstimate(ctx, in.FareEstimateID)
	if err != nil {
		return nil, err
	}
	if estimate == nil || estimate.CustomerID != customerID {
		return nil, ErrFareEstimateInvalid
	}
	service, err := u.Config.FindService(ctx, estimate.ServiceID)
	if err != nil {
		return nil, err
	}
	voucher, err := u.Vouchers.FindByCode(ctx, in.Code)
	if err != nil {
		return nil, err
	}
	if voucher == nil {
		return nil, entities.ErrVoucherNotFound
	}
	if err := voucher.CheckApplies(service, u.now()); err != nil {
		return nil, err
	}
	mine, err := u.Vouchers.CountCustomerRedemptions(ctx, voucher.ID, customerID)
	if err != nil {
		return nil, err
	}
	if mine >= int64(voucher.PerCustomerLimit) {
		return nil, entities.ErrVoucherUsed
	}
	discount, err := voucher.DiscountFor(estimate.TotalCents)
	if err != nil {
		return nil, err
	}
	return &dtos.VoucherPreview{Code: *voucher.Code, FareCents: estimate.TotalCents, DiscountCents: discount, TotalCents: estimate.TotalCents - discount, Currency: estimate.Currency}, nil
}
