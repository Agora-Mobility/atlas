package services

import (
	"context"
	"errors"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var (
	ErrPaymentsUnavailable = errors.New("payments are not available")
	ErrChargeNotFound      = errors.New("charge not found")
)

type ChargeStatus string

const (
	ChargePending ChargeStatus = "pending"
	ChargeSuccess ChargeStatus = "success"
	ChargeFailed  ChargeStatus = "failed"
)

type PaymentDeclinedError struct {
	Code    string
	Message string
}

func (e *PaymentDeclinedError) Error() string {
	return "payment declined: " + e.Code
}

type ChargeInput struct {
	Method      entities.PaymentMethod
	MerchantID  string
	AmountCents int64
	Currency    string
	Description string
	PhoneNumber string
}

type Charge struct {
	ID              string
	MerchantID      string
	Status          ChargeStatus
	AmountCents     int64
	Message         string
	ReferenceNumber string
	ReferenceEntity string
	DueAt           *time.Time
}

type IPaymentGateway interface {
	Enabled() bool
	CreateCharge(ctx context.Context, in ChargeInput) (*Charge, error)
	GetCharge(ctx context.Context, chargeID string) (*Charge, error)
	FindChargeByMerchantID(ctx context.Context, merchantID string) (*Charge, error)
	Refund(ctx context.Context, chargeID string, cents int64, description string) (string, error)
}
