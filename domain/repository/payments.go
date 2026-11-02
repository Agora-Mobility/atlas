package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrPaymentAlreadyPending = errors.New("this request already has a pending payment")

type CategoryPaymentMethod struct {
	CategoryID   uuid.UUID
	CategorySlug string
	CategoryName string
	Method       entities.PaymentMethod
	IsEnabled    bool
}

type PaymentFilter struct {
	Statuses  []string
	Method    string
	Category  string
	RequestID *uuid.UUID
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

type AdminPayment struct {
	Payment      entities.Payment
	Category     string
	CustomerName string
}

type PaymentEvent struct {
	Source     string
	Status     string
	Detail     string
	OccurredAt time.Time
}

type PaymentRefund struct {
	ID               uuid.UUID
	PaymentID        uuid.UUID
	AmountCents      int64
	Reason           string
	ProviderRefundID string
	ActorID          *uuid.UUID
	CreatedAt        time.Time
}

type IPaymentRepository interface {
	ListMethods(ctx context.Context, category string, onlyEnabled bool) ([]CategoryPaymentMethod, error)
	IsMethodEnabled(ctx context.Context, categoryID uuid.UUID, method entities.PaymentMethod) (bool, error)
	SetMethod(ctx context.Context, categoryID uuid.UUID, method entities.PaymentMethod, enabled bool) (bool, error)
	HasOutstanding(ctx context.Context, customerID uuid.UUID) (bool, error)
	SetRequestPayment(ctx context.Context, requestID uuid.UUID, method entities.PaymentMethod, status entities.RequestPaymentStatus) error
	Create(ctx context.Context, payment *entities.Payment) error
	Save(ctx context.Context, payment *entities.Payment) error
	Find(ctx context.Context, id uuid.UUID) (*entities.Payment, error)
	FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.Payment, error)
	FindByProviderCharge(ctx context.Context, chargeID string) (*entities.Payment, error)
	FindByMerchantID(ctx context.Context, merchantID string) (*entities.Payment, error)
	PendingOfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Payment, error)
	PaidOfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Payment, error)
	ListOfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.Payment, error)
	Due(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	AdminList(ctx context.Context, filter PaymentFilter) ([]AdminPayment, int64, error)
	AddEvent(ctx context.Context, paymentID uuid.UUID, event PaymentEvent) error
	Events(ctx context.Context, paymentID uuid.UUID) ([]PaymentEvent, error)
	AddRefund(ctx context.Context, refund *PaymentRefund) error
	Refunds(ctx context.Context, paymentID uuid.UUID) ([]PaymentRefund, error)
}
