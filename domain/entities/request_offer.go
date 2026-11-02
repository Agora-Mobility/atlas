package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type OfferStatus string

const (
	OfferSent       OfferStatus = "sent"
	OfferDelivered  OfferStatus = "delivered"
	OfferAccepted   OfferStatus = "accepted"
	OfferRejected   OfferStatus = "rejected"
	OfferExpired    OfferStatus = "expired"
	OfferNoResponse OfferStatus = "no_response"
	OfferQuoted     OfferStatus = "quoted"
	OfferCancelled  OfferStatus = "cancelled"
)

var (
	ErrOfferClosed  = errors.New("the offer has ended")
	ErrOfferExpired = errors.New("the offer has expired")
)

const maxQuoteNoteLength = 300

type RequestOffer struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	ProviderID   uuid.UUID
	Status       OfferStatus
	SentAt       time.Time
	DeliveredAt  *time.Time
	DeliveredVia string
	ExpiresAt    time.Time
	RespondedAt  *time.Time
	NextActionAt *time.Time
	FallbackStep int
	QuoteCents   *int64
	QuoteNote    string
	DistanceM    *int
	NetCents     *int64
	BoostBps     int
	RejectReason *uuid.UUID
	RejectNote   string
}

func NewRequestOffer(id, requestID, providerID uuid.UUID, policy *DispatchPolicy, distanceM int, netCents *int64, boostBps int, now time.Time) *RequestOffer {
	offer := &RequestOffer{
		ID:         id,
		RequestID:  requestID,
		ProviderID: providerID,
		Status:     OfferSent,
		SentAt:     now,
		ExpiresAt:  now.Add(time.Duration(policy.OfferTTLSeconds) * time.Second),
		DistanceM:  &distanceM,
		NetCents:   netCents,
		BoostBps:   boostBps,
	}
	offer.scheduleNext(policy)
	return offer
}

func (o *RequestOffer) IsOpen() bool {
	return o.Status == OfferSent || o.Status == OfferDelivered
}

func (o *RequestOffer) RemainingSeconds(now time.Time) int {
	remaining := int(o.ExpiresAt.Sub(now).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (o *RequestOffer) MarkDelivered(channel string, policy *DispatchPolicy, now time.Time) (bool, error) {
	if o.Status == OfferDelivered {
		return false, nil
	}
	if !o.IsOpen() {
		return false, ErrOfferClosed
	}
	o.Status = OfferDelivered
	o.DeliveredAt = &now
	o.DeliveredVia = channel
	o.scheduleNext(policy)
	return true, nil
}

func (o *RequestOffer) Respond(status OfferStatus, now time.Time) error {
	if !o.IsOpen() {
		return ErrOfferClosed
	}
	if !now.Before(o.ExpiresAt) {
		return ErrOfferExpired
	}
	o.close(status, now)
	return nil
}

func (o *RequestOffer) SubmitQuote(cents int64, note string, validUntil, now time.Time) error {
	if err := o.Respond(OfferQuoted, now); err != nil {
		if errors.Is(err, ErrOfferClosed) && o.Status == OfferQuoted {
			o.ExpiresAt = validUntil
			return o.setQuote(cents, note)
		}
		return err
	}
	o.ExpiresAt = validUntil
	return o.setQuote(cents, note)
}

func (o *RequestOffer) setQuote(cents int64, note string) error {
	note = strings.TrimSpace(note)
	if cents <= 0 || cents > MaxFeeCents {
		return validationError("amount_cents", "O orçamento não é válido.")
	}
	if len([]rune(note)) > maxQuoteNoteLength {
		return validationError("note", "A nota tem no máximo 300 caracteres.")
	}
	o.QuoteCents = &cents
	o.QuoteNote = note
	return nil
}

func (o *RequestOffer) Close(status OfferStatus, now time.Time) {
	o.close(status, now)
}

func (o *RequestOffer) close(status OfferStatus, now time.Time) {
	o.Status = status
	o.RespondedAt = &now
	o.NextActionAt = nil
}

type OfferStep int

const (
	OfferStepNone OfferStep = iota
	OfferStepFallback
	OfferStepGiveUp
	OfferStepExpire
)

func (o *RequestOffer) DueStep(policy *DispatchPolicy, now time.Time) (OfferStep, string) {
	if !o.IsOpen() {
		return OfferStepNone, ""
	}
	if !now.Before(o.ExpiresAt) {
		return OfferStepExpire, ""
	}
	if o.Status == OfferDelivered {
		return OfferStepNone, ""
	}
	if policy.AckGiveUpSeconds > 0 && !now.Before(o.SentAt.Add(time.Duration(policy.AckGiveUpSeconds)*time.Second)) {
		return OfferStepGiveUp, ""
	}
	if o.FallbackStep < len(policy.FallbackChannels) && !now.Before(o.fallbackAt(policy)) {
		return OfferStepFallback, policy.FallbackChannels[o.FallbackStep]
	}
	return OfferStepNone, ""
}

func (o *RequestOffer) AdvanceFallback(policy *DispatchPolicy) {
	o.FallbackStep++
	o.scheduleNext(policy)
}

func (o *RequestOffer) Reschedule(policy *DispatchPolicy) {
	o.scheduleNext(policy)
}

func (o *RequestOffer) fallbackAt(policy *DispatchPolicy) time.Time {
	return o.SentAt.Add(time.Duration(policy.AckTimeoutSeconds*(o.FallbackStep+1)) * time.Second)
}

func (o *RequestOffer) scheduleNext(policy *DispatchPolicy) {
	next := o.ExpiresAt
	if o.Status == OfferSent {
		if policy.AckGiveUpSeconds > 0 {
			if giveUp := o.SentAt.Add(time.Duration(policy.AckGiveUpSeconds) * time.Second); giveUp.Before(next) {
				next = giveUp
			}
		}
		if o.FallbackStep < len(policy.FallbackChannels) {
			if fallback := o.fallbackAt(policy); fallback.Before(next) {
				next = fallback
			}
		}
	}
	o.NextActionAt = &next
}
