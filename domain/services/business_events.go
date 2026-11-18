package services

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// IBusinessEvents records the business events of Atlas as metrics, span
// attributes and structured logs. Usecases call it at the point where the
// state changes, inside the transaction: the implementation only emits once
// the transaction commits, and never fails or blocks the caller.
type IBusinessEvents interface {
	RequestTransitioned(ctx context.Context, e RequestTransition)
	PaymentCreated(ctx context.Context, e PaymentEvent)
	PaymentSettled(ctx context.Context, e PaymentEvent)
	PaymentRefunded(ctx context.Context, e RefundEvent)
	UserRegistered(ctx context.Context, e UserRegistration)
	ProviderReviewed(ctx context.Context, e ProviderReview)
	BookingTransitioned(ctx context.Context, e BookingTransition)
	// StartStep opens a span for a business step; end records err on it.
	StartStep(ctx context.Context, name string) (context.Context, func(err error))
}

type RequestTransition struct {
	RequestID uuid.UUID
	Category  string
	From      string
	To        string
	ActorType string
	// Scheduled marks a request booked for later: its time to acceptance
	// says nothing about how fast providers answer.
	Scheduled     bool
	PaymentMethod string
	FareCents     *int64
	Currency      string
	CreatedAt     time.Time
	At            time.Time
}

type PaymentEvent struct {
	PaymentID   uuid.UUID
	RequestID   uuid.UUID
	Category    string
	Method      string
	Kind        string
	Status      string
	AmountCents int64
	Currency    string
	// Source is who settled it: api, webhook, reconcile or provider.
	Source string
	// FailureCode is the gateway code of a declined charge, when there is one.
	FailureCode string
	// Declined is set when the gateway refused the charge when it was made,
	// rather than failing it later.
	Declined  bool
	Duplicate bool
	CreatedAt time.Time
	At        time.Time
}

type RefundEvent struct {
	PaymentID   uuid.UUID
	RequestID   uuid.UUID
	Category    string
	Method      string
	Kind        string
	AmountCents int64
	Currency    string
	// Source: admin (a staff refund) or automatic (a cancelled prepaid
	// booking, a payment after cancellation).
	Source string
	Full   bool
}

type UserRegistration struct {
	UserID uuid.UUID
	Role   string
	// Channel is the client type the user signed up with.
	Channel string
}

type ProviderReview struct {
	ProviderID uuid.UUID
	Outcome    string
}

type BookingTransition struct {
	BookingID uuid.UUID
	RequestID uuid.UUID
	From      string
	To        string
	ActorType string
}

// NoBusinessEvents discards every event. It is the default of usecases built
// without a recorder, such as in unit tests.
type NoBusinessEvents struct{}

func (NoBusinessEvents) RequestTransitioned(context.Context, RequestTransition) {}
func (NoBusinessEvents) PaymentCreated(context.Context, PaymentEvent)           {}
func (NoBusinessEvents) PaymentSettled(context.Context, PaymentEvent)           {}
func (NoBusinessEvents) PaymentRefunded(context.Context, RefundEvent)           {}
func (NoBusinessEvents) UserRegistered(context.Context, UserRegistration)       {}
func (NoBusinessEvents) ProviderReviewed(context.Context, ProviderReview)       {}
func (NoBusinessEvents) BookingTransitioned(context.Context, BookingTransition) {}
func (NoBusinessEvents) StartStep(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) {}
}

// BusinessOrNoop returns b, or NoBusinessEvents when b is nil.
func BusinessOrNoop(b IBusinessEvents) IBusinessEvents {
	if b == nil {
		return NoBusinessEvents{}
	}
	return b
}
