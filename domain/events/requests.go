package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	ServiceRequestAggregate = "service_request"
	ProviderAggregate       = "provider"

	RequestCreated         = "requests.created"
	RequestScheduled       = "requests.scheduled"
	RequestSearching       = "requests.searching"
	RequestOffered         = "requests.offered"
	RequestAwaitingChoice  = "requests.awaiting-choice"
	RequestAccepted        = "requests.accepted"
	RequestProviderEnRoute = "requests.provider-en-route"
	RequestInProgress      = "requests.in-progress"
	RequestCompleted       = "requests.completed"
	RequestExpired         = "requests.expired"
	RequestCancelled       = "requests.cancelled"

	OfferCreated   = "requests.offer-created"
	OfferDelivered = "requests.offer-delivered"
	OfferExpired   = "requests.offer-expired"
	OfferRejected  = "requests.offer-rejected"
	OfferCancelled = "requests.offer-cancelled"

	QuoteSubmitted  = "requests.quote-submitted"
	FareBoosted     = "requests.fare-boosted"
	OperatorAlerted = "requests.operator-alerted"
	ReminderDue     = "requests.reminder-due"
	CoverageChanged = "providers.coverage-changed"
	PaymentChanged  = "payments.changed"
	RequestMessage  = "requests.message"
)

type RequestPoint struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Reference string  `json:"reference,omitempty"`
}

type RequestTransitionV1 struct {
	Type           string     `json:"-"`
	RequestID      uuid.UUID  `json:"request_id"`
	CustomerID     uuid.UUID  `json:"customer_id"`
	ProviderID     *uuid.UUID `json:"provider_id,omitempty"`
	Category       string     `json:"category"`
	Version        int        `json:"version"`
	From           string     `json:"from"`
	To             string     `json:"to"`
	ActorType      string     `json:"actor_type"`
	Reason         string     `json:"reason,omitempty"`
	FareCents      *int64     `json:"fare_cents,omitempty"`
	Currency       string     `json:"currency,omitempty"`
	PassengerPhone string     `json:"passenger_phone,omitempty"`
	SMSOnAccept    bool       `json:"sms_on_accept,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

func (e RequestTransitionV1) EventType() string   { return e.Type }
func (RequestTransitionV1) EventVersion() int     { return 1 }
func (RequestTransitionV1) AggregateType() string { return ServiceRequestAggregate }
func (e RequestTransitionV1) AggregateID() string { return e.RequestID.String() }

type RequestOfferV1 struct {
	Type          string        `json:"-"`
	OfferID       uuid.UUID     `json:"offer_id"`
	RequestID     uuid.UUID     `json:"request_id"`
	ProviderID    uuid.UUID     `json:"provider_id"`
	CustomerID    uuid.UUID     `json:"customer_id"`
	Category      string        `json:"category"`
	Version       int           `json:"request_version"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Pickup        *RequestPoint `json:"pickup,omitempty"`
	Destination   *RequestPoint `json:"destination,omitempty"`
	DistanceM     *int          `json:"distance_m,omitempty"`
	NetCents      *int64        `json:"net_cents,omitempty"`
	Currency      string        `json:"currency,omitempty"`
	PaymentMethod string        `json:"payment_method,omitempty"`
	Channels      []string      `json:"channels,omitempty"`
	Channel       string        `json:"channel,omitempty"`
	AckSeconds    *float64      `json:"ack_seconds,omitempty"`
	Reason        string        `json:"reason,omitempty"`
	OccurredAt    time.Time     `json:"occurred_at"`
}

func (e RequestOfferV1) EventType() string   { return e.Type }
func (RequestOfferV1) EventVersion() int     { return 1 }
func (RequestOfferV1) AggregateType() string { return ServiceRequestAggregate }
func (e RequestOfferV1) AggregateID() string { return e.RequestID.String() }

type QuoteSubmittedV1 struct {
	OfferID     uuid.UUID `json:"offer_id"`
	RequestID   uuid.UUID `json:"request_id"`
	ProviderID  uuid.UUID `json:"provider_id"`
	CustomerID  uuid.UUID `json:"customer_id"`
	Category    string    `json:"category"`
	Version     int       `json:"request_version"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (QuoteSubmittedV1) EventType() string     { return QuoteSubmitted }
func (QuoteSubmittedV1) EventVersion() int     { return 1 }
func (QuoteSubmittedV1) AggregateType() string { return ServiceRequestAggregate }
func (e QuoteSubmittedV1) AggregateID() string { return e.RequestID.String() }

type FareBoostedV1 struct {
	RequestID  uuid.UUID `json:"request_id"`
	CustomerID uuid.UUID `json:"customer_id"`
	Category   string    `json:"category"`
	Version    int       `json:"request_version"`
	BoostBps   int       `json:"boost_bps"`
	FareCents  int64     `json:"fare_cents"`
	AddedCents int64     `json:"added_cents"`
	Currency   string    `json:"currency"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (FareBoostedV1) EventType() string     { return FareBoosted }
func (FareBoostedV1) EventVersion() int     { return 1 }
func (FareBoostedV1) AggregateType() string { return ServiceRequestAggregate }
func (e FareBoostedV1) AggregateID() string { return e.RequestID.String() }

type OperatorAlertedV1 struct {
	RequestID  uuid.UUID `json:"request_id"`
	CustomerID uuid.UUID `json:"customer_id"`
	Category   string    `json:"category"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (OperatorAlertedV1) EventType() string     { return OperatorAlerted }
func (OperatorAlertedV1) EventVersion() int     { return 1 }
func (OperatorAlertedV1) AggregateType() string { return ServiceRequestAggregate }
func (e OperatorAlertedV1) AggregateID() string { return e.RequestID.String() }

type ReminderDueV1 struct {
	RequestID    uuid.UUID  `json:"request_id"`
	CustomerID   uuid.UUID  `json:"customer_id"`
	ProviderID   *uuid.UUID `json:"provider_id,omitempty"`
	Category     string     `json:"category"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	OccurredAt   time.Time  `json:"occurred_at"`
}

func (ReminderDueV1) EventType() string     { return ReminderDue }
func (ReminderDueV1) EventVersion() int     { return 1 }
func (ReminderDueV1) AggregateType() string { return ServiceRequestAggregate }
func (e ReminderDueV1) AggregateID() string { return e.RequestID.String() }

type CoverageChangedV1 struct {
	ProviderID uuid.UUID `json:"provider_id"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	RadiusKm   float64   `json:"radius_km"`
	Cells      int       `json:"cells"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (CoverageChangedV1) EventType() string     { return CoverageChanged }
func (CoverageChangedV1) EventVersion() int     { return 1 }
func (CoverageChangedV1) AggregateType() string { return ProviderAggregate }
func (e CoverageChangedV1) AggregateID() string { return e.ProviderID.String() }

var transitionEventTypes = map[string]string{
	"created":           RequestCreated,
	"scheduled":         RequestScheduled,
	"searching":         RequestSearching,
	"offered":           RequestOffered,
	"awaiting_choice":   RequestAwaitingChoice,
	"accepted":          RequestAccepted,
	"provider_en_route": RequestProviderEnRoute,
	"in_progress":       RequestInProgress,
	"completed":         RequestCompleted,
	"expired":           RequestExpired,
	"cancelled":         RequestCancelled,
}

func TransitionEventType(status string) string {
	return transitionEventTypes[status]
}

const RouteChanged = "requests.route-changed"

type RouteChangedV1 struct {
	RequestID  uuid.UUID  `json:"request_id"`
	CustomerID uuid.UUID  `json:"customer_id"`
	ProviderID *uuid.UUID `json:"provider_id,omitempty"`
	Category   string     `json:"category"`
	Version    int        `json:"request_version"`
	FareCents  int64      `json:"fare_cents"`
	Currency   string     `json:"currency"`
	OccurredAt time.Time  `json:"occurred_at"`
}

func (RouteChangedV1) EventType() string     { return RouteChanged }
func (RouteChangedV1) EventVersion() int     { return 1 }
func (RouteChangedV1) AggregateType() string { return ServiceRequestAggregate }
func (e RouteChangedV1) AggregateID() string { return e.RequestID.String() }

const (
	ProviderApplicationSubmitted = "providers.application-submitted"
	ProviderApproved             = "providers.approved"
	ProviderRejected             = "providers.rejected"
	ProviderSuspended            = "providers.suspended"
)

type ProviderReviewV1 struct {
	Type       string     `json:"-"`
	ProviderID uuid.UUID  `json:"provider_id"`
	ServiceID  *uuid.UUID `json:"service_id,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

func (e ProviderReviewV1) EventType() string   { return e.Type }
func (ProviderReviewV1) EventVersion() int     { return 1 }
func (ProviderReviewV1) AggregateType() string { return ProviderAggregate }
func (e ProviderReviewV1) AggregateID() string { return e.ProviderID.String() }

type PaymentChangedV1 struct {
	RequestID   uuid.UUID  `json:"request_id"`
	CustomerID  uuid.UUID  `json:"customer_id"`
	ProviderID  *uuid.UUID `json:"provider_id,omitempty"`
	PaymentID   *uuid.UUID `json:"payment_id,omitempty"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	AmountCents int64      `json:"amount_cents"`
	Currency    string     `json:"currency"`
	OccurredAt  time.Time  `json:"occurred_at"`
}

func (e PaymentChangedV1) EventType() string     { return PaymentChanged }
func (e PaymentChangedV1) EventVersion() int     { return 1 }
func (e PaymentChangedV1) AggregateType() string { return ServiceRequestAggregate }
func (e PaymentChangedV1) AggregateID() string   { return e.RequestID.String() }

type RequestMessageV1 struct {
	MessageID   uuid.UUID `json:"message_id"`
	RequestID   uuid.UUID `json:"request_id"`
	SenderType  string    `json:"sender_type"`
	RecipientID uuid.UUID `json:"recipient_id"`
	Body        string    `json:"body"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (e RequestMessageV1) EventType() string     { return RequestMessage }
func (e RequestMessageV1) EventVersion() int     { return 1 }
func (e RequestMessageV1) AggregateType() string { return ServiceRequestAggregate }
func (e RequestMessageV1) AggregateID() string   { return e.RequestID.String() }

const (
	BookingAggregate = "booking"
	BookingChanged   = "bookings.changed"
	BookingExtra     = "bookings.extra"
)

type BookingChangedV1 struct {
	BookingID    uuid.UUID `json:"booking_id"`
	RequestID    uuid.UUID `json:"request_id"`
	CustomerID   uuid.UUID `json:"customer_id"`
	Code         string    `json:"code"`
	WorkshopName string    `json:"workshop_name"`
	Status       string    `json:"status"`
	StartsAt     time.Time `json:"starts_at"`
	OccurredAt   time.Time `json:"occurred_at"`
}

func (BookingChangedV1) EventType() string     { return BookingChanged }
func (BookingChangedV1) EventVersion() int     { return 1 }
func (BookingChangedV1) AggregateType() string { return BookingAggregate }
func (e BookingChangedV1) AggregateID() string { return e.BookingID.String() }

type BookingExtraV1 struct {
	BookingID   uuid.UUID `json:"booking_id"`
	RequestID   uuid.UUID `json:"request_id"`
	CustomerID  uuid.UUID `json:"customer_id"`
	ExtraID     uuid.UUID `json:"extra_id"`
	Description string    `json:"description"`
	AmountCents int64     `json:"amount_cents"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func (BookingExtraV1) EventType() string     { return BookingExtra }
func (BookingExtraV1) EventVersion() int     { return 1 }
func (BookingExtraV1) AggregateType() string { return BookingAggregate }
func (e BookingExtraV1) AggregateID() string { return e.BookingID.String() }
