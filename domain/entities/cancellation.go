package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	maxCancelWindowSeconds    = 2_592_000
	maxAssignedWindowSeconds  = 604_800
	CancelActorCustomer       = "customer"
	CancelActorProvider       = "provider"
	CancelActorOfferRejection = "offer"
)

var (
	ErrCancelFeeChanged  = errors.New("the cancellation fee is not the one the customer saw")
	ErrUnknownCancelRule = errors.New("no cancellation rule for this service")
)

type CancellationRule struct {
	ServiceID              uuid.UUID
	ServiceSlug            string
	ServiceName            string
	FeeCents               int64
	FreeAfterAssignSeconds int
	ScheduledFreeSeconds   int
	ChargeWithoutProvider  bool
}

func (r *CancellationRule) Validate() error {
	if r.FeeCents < 0 || r.FeeCents > MaxFeeCents {
		return validationError("fee_cents", "A taxa não é válida.")
	}
	if r.FreeAfterAssignSeconds < 0 || r.FreeAfterAssignSeconds > maxAssignedWindowSeconds {
		return validationError("free_after_assign_seconds", "O prazo tem de estar entre 0 e 604800 segundos.")
	}
	if r.ScheduledFreeSeconds < 0 || r.ScheduledFreeSeconds > maxCancelWindowSeconds {
		return validationError("scheduled_free_seconds", "O prazo tem de estar entre 0 e 2592000 segundos.")
	}
	return nil
}

type CancelCost struct {
	FeeCents  int64
	FreeUntil *time.Time
}

func (r *CancellationRule) CostFor(request *ServiceRequest, now time.Time) CancelCost {
	assigned := request.ProviderID != nil && request.Status.HasProvider()
	if request.ScheduledFor != nil {
		deadline := request.ScheduledFor.Add(-time.Duration(r.ScheduledFreeSeconds) * time.Second)
		if now.Before(deadline) {
			return CancelCost{FreeUntil: &deadline}
		}
		if r.ChargeWithoutProvider {
			return CancelCost{FeeCents: r.FeeCents}
		}
	}
	if !assigned {
		return CancelCost{}
	}
	if request.AcceptedAt != nil {
		deadline := request.AcceptedAt.Add(time.Duration(r.FreeAfterAssignSeconds) * time.Second)
		if now.Before(deadline) {
			return CancelCost{FreeUntil: &deadline}
		}
	}
	return CancelCost{FeeCents: r.FeeCents}
}

func ReasonScopeFor(actor string) (RejectionScope, bool) {
	switch actor {
	case CancelActorCustomer:
		return RejectionScopeCustomerCancel, true
	case CancelActorProvider:
		return RejectionScopeProviderCancel, true
	case CancelActorOfferRejection:
		return RejectionScopeOfferReject, true
	}
	return "", false
}
