package dispatch

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

var adjustmentLabels = map[entities.FareAdjustmentReason]string{
	entities.AdjustmentBoost:       "Aumento da oferta",
	entities.AdjustmentWaiting:     "Tempo de espera",
	entities.AdjustmentRouteChange: "Alteração de rota",
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
