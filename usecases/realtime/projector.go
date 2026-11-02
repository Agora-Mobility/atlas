package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	sseOffer          = "request.offer"
	sseOfferCancelled = "request.offer_cancelled"
	sseOfferExpired   = "request.offer_expired"
	sseStatus         = "request.status"
	sseQuote          = "request.quote"
	sseFare           = "request.fare"
	sseRoute          = "request.route"
	sseAlert          = "request.alert"
	sseReminder       = "request.reminder"
	ssePayment        = "request.payment"
	sseMessage        = "request.message"
	maxEventBytes     = 2048
)

type Projector struct {
	mailbox services.IMailbox
	now     func() time.Time
}

func NewProjector(mailbox services.IMailbox) *Projector {
	return &Projector{mailbox: mailbox, now: time.Now}
}

func BindingKeys() []string {
	types := []string{
		events.RequestSearching, events.RequestScheduled, events.RequestOffered, events.RequestAwaitingChoice, events.RequestAccepted, events.RequestProviderEnRoute,
		events.RequestInProgress, events.RequestCompleted, events.RequestExpired, events.RequestCancelled, events.OfferCreated, events.OfferExpired,
		events.OfferCancelled, events.RequestMessage, events.PaymentChanged, events.QuoteSubmitted, events.FareBoosted, events.RouteChanged, events.OperatorAlerted, events.ReminderDue,
	}
	keys := make([]string, 0, len(types))
	for _, eventType := range types {
		keys = append(keys, events.RoutingKey(eventType, 1))
	}
	return keys
}

type projection struct {
	recipients []uuid.UUID
	sseType    string
	class      services.MessageClass
	body       map[string]any
	expiresAt  *time.Time
}

func (p *Projector) Handle(ctx context.Context, env events.Envelope) error {
	projected, err := p.project(env)
	if err != nil {
		return err
	}
	if projected == nil {
		return nil
	}
	projected.body["event_id"] = env.EventID.String()
	projected.body["class"] = projected.class
	data, err := json.Marshal(projected.body)
	if err != nil {
		return fmt.Errorf("encode %s: %w", env.EventType, err)
	}
	if len(data) > maxEventBytes {
		logger.FromContext(ctx).Warn("real-time event above the size budget", zap.String("event_type", env.EventType), zap.Int("bytes", len(data)))
	}
	message := services.MailboxMessage{EventID: env.EventID.String(), Type: projected.sseType, Class: projected.class, Data: data, ExpiresAt: projected.expiresAt}
	for _, recipient := range projected.recipients {
		if err := p.mailbox.Deliver(ctx, recipient.String(), message); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) project(env events.Envelope) (*projection, error) {
	switch env.EventType {
	case events.OfferCreated:
		var event events.RequestOfferV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return p.projectOffer(event), nil
	case events.OfferCancelled, events.OfferExpired:
		var event events.RequestOfferV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		sseType, class := sseOfferExpired, services.ClassState
		if env.EventType == events.OfferCancelled {
			sseType, class = sseOfferCancelled, services.ClassCritical
		}
		return &projection{
			recipients: []uuid.UUID{event.ProviderID}, sseType: sseType, class: class,
			body: map[string]any{"offer_id": event.OfferID, "request_id": event.RequestID, "request_version": event.Version, "reason": event.Reason},
		}, nil
	case events.RequestMessage:
		var event events.RequestMessageV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: []uuid.UUID{event.RecipientID}, sseType: sseMessage, class: services.ClassState,
			body: map[string]any{"message_id": event.MessageID, "request_id": event.RequestID, "sender": event.SenderType, "body": event.Body},
		}, nil
	case events.PaymentChanged:
		var event events.PaymentChangedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		recipients := []uuid.UUID{event.CustomerID}
		if event.ProviderID != nil {
			recipients = append(recipients, *event.ProviderID)
		}
		return &projection{
			recipients: recipients, sseType: ssePayment, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "payment_id": event.PaymentID, "method": event.Method, "status": event.Status, "amount_cents": event.AmountCents, "currency": event.Currency},
		}, nil
	case events.QuoteSubmitted:
		var event events.QuoteSubmittedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: []uuid.UUID{event.CustomerID}, sseType: sseQuote, class: services.ClassState,
			body: map[string]any{"offer_id": event.OfferID, "request_id": event.RequestID, "request_version": event.Version, "amount_cents": event.AmountCents, "currency": event.Currency},
		}, nil
	case events.FareBoosted:
		var event events.FareBoostedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: []uuid.UUID{event.CustomerID}, sseType: sseFare, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "request_version": event.Version, "fare_cents": event.FareCents, "boost_bps": event.BoostBps, "currency": event.Currency},
		}, nil
	case events.RouteChanged:
		var event events.RouteChangedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: withProvider(event.CustomerID, event.ProviderID), sseType: sseRoute, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "request_version": event.Version, "fare_cents": event.FareCents, "currency": event.Currency},
		}, nil
	case events.OperatorAlerted:
		var event events.OperatorAlertedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: []uuid.UUID{event.CustomerID}, sseType: sseAlert, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "reason": event.Reason},
		}, nil
	case events.ReminderDue:
		var event events.ReminderDueV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: withProvider(event.CustomerID, event.ProviderID), sseType: sseReminder, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "scheduled_for": event.ScheduledFor},
		}, nil
	default:
		var event events.RequestTransitionV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &projection{
			recipients: withProvider(event.CustomerID, event.ProviderID), sseType: sseStatus, class: services.ClassState,
			body: map[string]any{"request_id": event.RequestID, "request_version": event.Version, "category": event.Category, "status": event.To, "reason": event.Reason},
		}, nil
	}
}

func (p *Projector) projectOffer(event events.RequestOfferV1) *projection {
	if !p.now().Before(event.ExpiresAt) {
		return nil
	}
	expiresAt := event.ExpiresAt
	body := map[string]any{
		"offer_id": event.OfferID, "request_id": event.RequestID, "request_version": event.Version, "category": event.Category,
		"pickup": event.Pickup, "distance_m": event.DistanceM, "payment_method": event.PaymentMethod,
	}
	if event.Destination != nil {
		body["destination"] = event.Destination
	}
	if event.NetCents != nil {
		body["net_cents"] = event.NetCents
		body["currency"] = event.Currency
	}
	return &projection{recipients: []uuid.UUID{event.ProviderID}, sseType: sseOffer, class: services.ClassCritical, body: body, expiresAt: &expiresAt}
}

func withProvider(customerID uuid.UUID, providerID *uuid.UUID) []uuid.UUID {
	if providerID == nil {
		return []uuid.UUID{customerID}
	}
	return []uuid.UUID{customerID, *providerID}
}
