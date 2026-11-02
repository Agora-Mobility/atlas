package appypay

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/services"
)

type Unavailable struct{}

var _ services.IPaymentGateway = Unavailable{}

func (Unavailable) Enabled() bool { return false }

func (Unavailable) CreateCharge(context.Context, services.ChargeInput) (*services.Charge, error) {
	return nil, services.ErrPaymentsUnavailable
}

func (Unavailable) GetCharge(context.Context, string) (*services.Charge, error) {
	return nil, services.ErrPaymentsUnavailable
}

func (Unavailable) FindChargeByMerchantID(context.Context, string) (*services.Charge, error) {
	return nil, services.ErrPaymentsUnavailable
}

func (Unavailable) Refund(context.Context, string, int64, string) (string, error) {
	return "", services.ErrPaymentsUnavailable
}
