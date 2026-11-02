package entities

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RequestStatus string

const (
	RequestCreated         RequestStatus = "created"
	RequestScheduled       RequestStatus = "scheduled"
	RequestSearching       RequestStatus = "searching"
	RequestOffered         RequestStatus = "offered"
	RequestAwaitingChoice  RequestStatus = "awaiting_choice"
	RequestAccepted        RequestStatus = "accepted"
	RequestProviderEnRoute RequestStatus = "provider_en_route"
	RequestInProgress      RequestStatus = "in_progress"
	RequestCompleted       RequestStatus = "completed"
	RequestExpired         RequestStatus = "expired"
	RequestCancelled       RequestStatus = "cancelled"
)

type RequestActorType string

const (
	RequestActorCustomer RequestActorType = "customer"
	RequestActorProvider RequestActorType = "provider"
	RequestActorOperator RequestActorType = "operator"
	RequestActorSystem   RequestActorType = "system"
)

type RequestActor struct {
	Type RequestActorType
	ID   *uuid.UUID
}

func SystemRequestActor() RequestActor {
	return RequestActor{Type: RequestActorSystem}
}

var (
	ErrInvalidTransition   = errors.New("invalid request transition")
	ErrTransitionForbidden = errors.New("this actor cannot make this transition")
	ErrCancelReasonMissing = errors.New("a cancel reason is required")
)

const (
	maxReferenceLength = 140
	maxAddressLength   = 200
	maxReasonLength    = 600
)

var allowedTransitions = map[RequestStatus][]RequestStatus{
	RequestCreated:         {RequestScheduled, RequestSearching, RequestCancelled},
	RequestScheduled:       {RequestSearching, RequestCancelled},
	RequestSearching:       {RequestOffered, RequestAccepted, RequestExpired, RequestCancelled},
	RequestOffered:         {RequestSearching, RequestAwaitingChoice, RequestAccepted, RequestCancelled},
	RequestAwaitingChoice:  {RequestAccepted, RequestCancelled},
	RequestAccepted:        {RequestProviderEnRoute, RequestCancelled},
	RequestProviderEnRoute: {RequestInProgress, RequestCancelled},
	RequestInProgress:      {RequestCompleted},
}

var transitionActors = map[RequestStatus][]RequestActorType{
	RequestScheduled:       {RequestActorSystem},
	RequestSearching:       {RequestActorSystem, RequestActorOperator},
	RequestOffered:         {RequestActorSystem},
	RequestAwaitingChoice:  {RequestActorSystem},
	RequestAccepted:        {RequestActorProvider, RequestActorCustomer, RequestActorOperator},
	RequestProviderEnRoute: {RequestActorProvider, RequestActorOperator},
	RequestInProgress:      {RequestActorProvider, RequestActorOperator},
	RequestCompleted:       {RequestActorProvider, RequestActorOperator},
	RequestExpired:         {RequestActorSystem},
	RequestCancelled:       {RequestActorCustomer, RequestActorProvider, RequestActorOperator, RequestActorSystem},
}

func (s RequestStatus) IsTerminal() bool {
	return s == RequestCompleted || s == RequestExpired || s == RequestCancelled
}

func (s RequestStatus) IsSearching() bool {
	return s == RequestSearching || s == RequestOffered
}

func (s RequestStatus) HasProvider() bool {
	return s == RequestAccepted || s == RequestProviderEnRoute || s == RequestInProgress || s == RequestCompleted
}

type RequestPlace struct {
	AddressID uuid.UUID `json:"-"`
	Point     GeoPoint  `json:"point"`
	Reference string    `json:"reference,omitempty"`
	Address   string    `json:"address,omitempty"`
}

func (p RequestPlace) normalized(field string) (RequestPlace, error) {
	if err := p.Point.Validate(field); err != nil {
		return p, err
	}
	p.Reference = strings.TrimSpace(p.Reference)
	p.Address = strings.TrimSpace(p.Address)
	if len([]rune(p.Reference)) > maxReferenceLength {
		return p, validationError(field+".reference", "O ponto de referência tem no máximo 140 caracteres.")
	}
	if len([]rune(p.Address)) > maxAddressLength {
		return p, validationError(field+".address", "A morada tem no máximo 200 caracteres.")
	}
	if p.AddressID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return p, err
		}
		p.AddressID = id
	}
	return p, nil
}

type RequestFare struct {
	EstimateID      *uuid.UUID
	Cents           *int64
	CommissionCents *int64
	BoostBps        int
	Currency        string
}

func (f RequestFare) ProviderNetCents() *int64 {
	if f.Cents == nil {
		return nil
	}
	net := *f.Cents
	if f.CommissionCents != nil {
		net -= *f.CommissionCents
	}
	return &net
}

type NewRequestInput struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	Service        DispatchService
	Policy         *DispatchPolicy
	Zone           *ServiceZone
	Pickup         RequestPlace
	Destination    *RequestPlace
	Stops          []RequestPlace
	ContactPhone   string
	PassengerName  string
	PassengerPhone string
	Details        json.RawMessage
	ScheduledFor   *time.Time
	Fare           RequestFare
	PaymentMethod  PaymentMethod
	IdempotencyKey string
	Now            time.Time
}

type ServiceRequest struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	CategoryID     uuid.UUID
	CategorySlug   string
	ServiceID      uuid.UUID
	ZoneID         uuid.UUID
	PolicyID       uuid.UUID
	Status         RequestStatus
	Version        int
	Pickup         RequestPlace
	PickupH3R8     int64
	Destination    *RequestPlace
	DestinationH3  *int64
	Stops          []RequestPlace
	ContactPhone   string
	PassengerName  string
	PassengerPhone string
	Details        json.RawMessage
	ScheduledFor   *time.Time
	SearchRound    int
	SearchExtended bool
	SearchDeadline *time.Time
	NextActionAt   *time.Time
	ReminderAt     *time.Time
	ProviderID     *uuid.UUID
	Fare           RequestFare
	PaymentMethod  PaymentMethod
	PaymentStatus  RequestPaymentStatus
	NeedsOperator  bool
	OperatorReason string
	IdempotencyKey string
	RouteChanged   bool
	CancelReason   string
	CancelledBy    string
	CancelReasonID *uuid.UUID
	CancelFeeCents int64
	DiscountCents  int64
	VoucherCode    string
	AcceptedAt     *time.Time
	ArrivedAt      *time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewServiceRequest(in NewRequestInput) (*ServiceRequest, error) {
	pickup, err := in.Pickup.normalized("pickup")
	if err != nil {
		return nil, err
	}
	var destination *RequestPlace
	if in.Service.RequiresDestination {
		if in.Destination == nil {
			return nil, validationError("destination", "Este serviço precisa de um destino.")
		}
		normalized, err := in.Destination.normalized("destination")
		if err != nil {
			return nil, err
		}
		destination = &normalized
	}
	stops, err := normalizedStops(in.Stops, in.Service.RequiresDestination, in.Policy)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, validationError("idempotency_key", "A chave de idempotência é obrigatória.")
	}
	if (in.PassengerName == "") != (in.PassengerPhone == "") {
		return nil, validationError("passenger", "Indica o nome e o telemóvel do passageiro.")
	}
	if in.ScheduledFor != nil {
		if err := in.Policy.ValidateSchedule(*in.ScheduledFor, in.Now); err != nil {
			return nil, err
		}
	}
	paymentMethod := in.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = PaymentCash
	}
	details := in.Details
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	return &ServiceRequest{
		ID:             in.ID,
		CustomerID:     in.CustomerID,
		CategoryID:     in.Service.CategoryID,
		CategorySlug:   in.Service.CategorySlug,
		ServiceID:      in.Service.ID,
		ZoneID:         in.Zone.ID,
		PolicyID:       in.Policy.ID,
		Status:         RequestCreated,
		Version:        1,
		Pickup:         pickup,
		Destination:    destination,
		Stops:          stops,
		ContactPhone:   strings.TrimSpace(in.ContactPhone),
		PassengerName:  strings.TrimSpace(in.PassengerName),
		PassengerPhone: strings.TrimSpace(in.PassengerPhone),
		Details:        details,
		ScheduledFor:   in.ScheduledFor,
		Fare:           in.Fare,
		PaymentMethod:  paymentMethod,
		PaymentStatus:  RequestUnpaid,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		CreatedAt:      in.Now,
		UpdatedAt:      in.Now,
	}, nil
}

func normalizedStops(stops []RequestPlace, requiresDestination bool, policy *DispatchPolicy) ([]RequestPlace, error) {
	if len(stops) == 0 {
		return []RequestPlace{}, nil
	}
	if !requiresDestination {
		return nil, validationError("stops", "Este serviço não tem paragens.")
	}
	if err := policy.ValidateStops(len(stops)); err != nil {
		return nil, err
	}
	out := make([]RequestPlace, 0, len(stops))
	for _, stop := range stops {
		normalized, err := stop.normalized("stops")
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

func (r *ServiceRequest) IsCustomer(userID uuid.UUID) bool {
	return r.CustomerID == userID
}

func (r *ServiceRequest) IsProvider(userID uuid.UUID) bool {
	return r.ProviderID != nil && *r.ProviderID == userID
}

func (r *ServiceRequest) Transition(to RequestStatus, actor RequestActor, reason string, now time.Time) (*RequestStatusChange, error) {
	if !canReach(r.Status, to) {
		return nil, ErrInvalidTransition
	}
	if !actorMay(to, actor.Type) {
		return nil, ErrTransitionForbidden
	}
	reason = strings.TrimSpace(reason)
	if to == RequestCancelled && reason == "" {
		return nil, ErrCancelReasonMissing
	}
	if len([]rune(reason)) > maxReasonLength {
		return nil, validationError("reason", "O motivo tem no máximo 600 caracteres.")
	}
	change := &RequestStatusChange{From: r.Status, To: to, Actor: actor, Reason: reason, At: now}
	expected := r.Version
	r.Status = to
	r.Version++
	r.UpdatedAt = now
	change.ExpectedVersion = expected
	change.Version = r.Version
	r.applyStatusFields(to, actor, reason, now)
	return change, nil
}

func (r *ServiceRequest) applyStatusFields(to RequestStatus, actor RequestActor, reason string, now time.Time) {
	switch to {
	case RequestAccepted:
		r.AcceptedAt = &now
		r.NeedsOperator = false
		r.OperatorReason = ""
		r.NextActionAt = nil
	case RequestInProgress:
		r.StartedAt = &now
	case RequestCompleted:
		r.CompletedAt = &now
		r.NextActionAt = nil
		r.ReminderAt = nil
	case RequestExpired:
		r.NextActionAt = nil
	case RequestCancelled:
		r.CancelReason = reason
		r.CancelledBy = string(actor.Type)
		r.NextActionAt = nil
		r.ReminderAt = nil
		r.NeedsOperator = false
	default:
	}
}

func (r *ServiceRequest) MarkArrived(now time.Time) error {
	if r.Status != RequestProviderEnRoute {
		return ErrInvalidTransition
	}
	if r.ArrivedAt == nil {
		r.ArrivedAt = &now
	}
	return nil
}

func (r *ServiceRequest) SendToOperator(reason string, now time.Time) {
	r.NeedsOperator = true
	r.OperatorReason = reason
	r.NextActionAt = nil
	r.UpdatedAt = now
}

func (r *ServiceRequest) ApplyBoost(bps int) (int64, error) {
	if r.Fare.Cents == nil {
		return 0, ErrBoostNotAllowed
	}
	if bps != BoostTenPercentBps && bps != BoostTwentyPercentBps {
		return 0, validationError("percent", "O aumento tem de ser de 10% ou 20%.")
	}
	if bps <= r.Fare.BoostBps {
		return 0, ErrBoostNotHigher
	}
	base := *r.Fare.Cents * bpsDenominator / int64(bpsDenominator+r.Fare.BoostBps)
	added := base*int64(bps)/bpsDenominator - base*int64(r.Fare.BoostBps)/bpsDenominator
	total := *r.Fare.Cents + added
	r.Fare.Cents = &total
	r.Fare.BoostBps = bps
	return added, nil
}

func canReach(from, to RequestStatus) bool {
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

func actorMay(to RequestStatus, actor RequestActorType) bool {
	for _, candidate := range transitionActors[to] {
		if candidate == actor {
			return true
		}
	}
	return false
}

type RequestStatusChange struct {
	From            RequestStatus
	To              RequestStatus
	ExpectedVersion int
	Version         int
	Actor           RequestActor
	Reason          string
	At              time.Time
}

func (r *ServiceRequest) Touch(now time.Time) int {
	expected := r.Version
	r.Version++
	r.UpdatedAt = now
	return expected
}

func (r *ServiceRequest) BeginSearch(policy *DispatchPolicy, now time.Time) {
	deadline := now.Add(time.Duration(policy.SearchTTLSeconds) * time.Second)
	r.SearchDeadline = &deadline
	r.SearchRound = 0
	r.NextActionAt = &now
}

func (r *ServiceRequest) ScheduleSearch(policy *DispatchPolicy, now time.Time) {
	at := r.ScheduledFor.Add(-time.Duration(policy.SearchLeadSeconds) * time.Second)
	if at.Before(now) {
		at = now
	}
	r.NextActionAt = &at
}

func (r *ServiceRequest) ChangeRoute(destination RequestPlace, stops []RequestPlace, policy *DispatchPolicy, fareCents, commissionCents int64) error {
	normalized, err := destination.normalized("destination")
	if err != nil {
		return err
	}
	normalizedAll, err := normalizedStops(stops, true, policy)
	if err != nil {
		return err
	}
	r.Destination = &normalized
	r.Stops = normalizedAll
	r.RouteChanged = true
	r.Fare.Cents = &fareCents
	r.Fare.CommissionCents = &commissionCents
	return nil
}

func (r *ServiceRequest) IsSameTrip(serviceID uuid.UUID, pickup GeoPoint, destination *GeoPoint, stops []RequestPlace, scheduledFor *time.Time) bool {
	if r.ServiceID != serviceID || DistanceMeters(r.Pickup.Point, pickup) > maxPinDriftMeters || (r.Destination == nil) != (destination == nil) || len(r.Stops) != len(stops) {
		return false
	}
	if r.Destination != nil && DistanceMeters(r.Destination.Point, *destination) > maxPinDriftMeters {
		return false
	}
	for i := range stops {
		if DistanceMeters(r.Stops[i].Point, stops[i].Point) > maxPinDriftMeters {
			return false
		}
	}
	if (r.ScheduledFor == nil) != (scheduledFor == nil) {
		return false
	}
	return r.ScheduledFor == nil || r.ScheduledFor.Equal(*scheduledFor)
}

func (r *ServiceRequest) AddToFare(cents int64) {
	if r.Fare.Cents == nil {
		return
	}
	total := *r.Fare.Cents + cents
	r.Fare.Cents = &total
}

func (r *ServiceRequest) AmountDueCents() int64 {
	if r.Status == RequestCancelled {
		return r.CancelFeeCents
	}
	if r.Status != RequestCompleted || r.Fare.Cents == nil {
		return 0
	}
	return *r.Fare.Cents - r.DiscountCents
}
