package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

var (
	ErrRequestNotFound      = errors.New("request not found")
	ErrOfferNotFound        = errors.New("offer not found")
	ErrServiceNotOffered    = errors.New("the service is not available")
	ErrOutsideServiceArea   = errors.New("the service is not available at this location yet")
	ErrFareEstimateRequired = errors.New("this service needs a fare estimate")
	ErrFareEstimateInvalid  = errors.New("the fare estimate does not match this request")
	ErrNoTariff             = errors.New("no tariff for this service in this zone")
	ErrFareEstimateUsed     = errors.New("the fare estimate was already used")
	ErrReceiptNotReady      = errors.New("the receipt exists after the request is completed")
	ErrStopNotFound         = errors.New("this stop does not exist")
	ErrReasonNotApplicable  = errors.New("this reason does not exist or does not apply here")
	ErrUnknownReasonActor   = errors.New("actor must be customer, provider or offer")
	ErrReportScope          = errors.New("scope must be customer_cancel, provider_cancel or offer_reject")
	ErrDuplicateRequest     = errors.New("the customer already has this request active")
	ErrRequestConflict      = repository.ErrRequestVersionConflict
	ErrProviderNotApproved  = errors.New("the provider is not approved")
	ErrProviderBusy         = errors.New("the provider already has an active job")
	ErrQuoteRequired        = errors.New("this offer is answered with a quote")
	ErrQuoteNotAllowed      = errors.New("this offer does not take quotes")
	ErrQuoteExpired         = errors.New("the quote is no longer valid")
	ErrInvalidChannel       = errors.New("unknown delivery channel")
	ErrRouteChangeForbidden = errors.New("the route of this request cannot change")
	ErrZoneNotFound         = errors.New("zone not found")
	ErrPolicyNotFound       = errors.New("policy not found")
	ErrProviderNotEligible  = errors.New("the provider cannot take this request")
)

type FareEstimateExpiredError struct {
	Fresh any
}

func (e *FareEstimateExpiredError) Error() string {
	return "the fare estimate expired"
}

const (
	maxCandidateBatchForETA = 10
	maxCandidatesPerSearch  = 50
	idleBandMeters          = 500
	searchRetryFloor        = 5 * time.Second
	searchRetryDivisor      = 60
	offerSafetyMargin       = 5 * time.Second
	sampleEvery             = 30 * time.Second
	lostAlertEvery          = 10 * time.Minute
	reminderLead            = 24 * time.Hour
	metersPerKm             = 1000
	secondsPerHour          = 3600
	minLearnedSamples       = 5
)

type Settings struct {
	WatcherInterval  time.Duration
	WatcherBatch     int
	FareEstimateTTL  time.Duration
	NearbyMinCount   int
	Limits           entities.PlausibilityLimits
	HistoryRetention time.Duration
	RoadFactor       float64
	AverageSpeedKmh  float64
	ChannelTimeout   time.Duration
}

type OfferChannels interface {
	SendOffer(ctx context.Context, channel string, offer OfferNotice) error
}

type OfferNotice struct {
	OfferID    uuid.UUID
	RequestID  uuid.UUID
	ProviderID uuid.UUID
	Category   string
	ExpiresIn  int
	Reference  string
	DistanceM  *int
}

type Deps struct {
	Config    repository.IDispatchConfigRepository
	Pricing   repository.IPricingRepository
	Providers repository.IProviderRepository
	Requests  repository.IRequestRepository
	Offers    repository.IOfferRepository
	Payments  repository.IPaymentRepository
	Cancels   repository.ICancellationRepository
	Ratings   repository.IRatingRepository
	Vouchers  repository.IVoucherRepository
	Devices   repository.IDeviceRepository
	Locator   repository.IProviderLocator
	Outbox    repository.IOutboxRepository
	Tx        repository.ITransactionManager
	Audit     repository.IAuditLogRepository
	Geo       services.IGeoIndex
	Routing   services.IRoutingProvider
	Feed      services.IPositionFeed
	Channels  OfferChannels
}

type Usecase struct {
	Deps
	settings Settings
	zones    *ZoneCache
	now      func() time.Time
	sends    chan struct{}
}

const concurrentChannelSends = 32

func NewUsecase(deps Deps, settings Settings) *Usecase {
	return &Usecase{
		Deps: deps, settings: settings,
		zones: NewZoneCache(deps.Config, deps.Geo),
		now:   func() time.Time { return time.Now().UTC() },
		sends: make(chan struct{}, concurrentChannelSends),
	}
}

func (u *Usecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Outbox.Save(ctx, envelope)
}

func (u *Usecase) audit(ctx context.Context, actor entities.Actor, action, entityType string, id uuid.UUID, before, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entityType, id.String(), before, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Audit.Record(ctx, entry)
}

func (u *Usecase) recordTransition(ctx context.Context, request *entities.ServiceRequest, change *entities.RequestStatusChange, policy *entities.DispatchPolicy) error {
	if err := u.Requests.Update(ctx, request, change.ExpectedVersion); err != nil {
		return err
	}
	return u.announceTransition(ctx, request, change, policy)
}

func (u *Usecase) announceTransition(ctx context.Context, request *entities.ServiceRequest, change *entities.RequestStatusChange, policy *entities.DispatchPolicy) error {
	if err := u.Requests.RecordChange(ctx, request.ID, change); err != nil {
		return err
	}
	return u.publish(ctx, events.RequestTransitionV1{
		Type: events.TransitionEventType(string(change.To)), RequestID: request.ID, CustomerID: request.CustomerID, ProviderID: request.ProviderID,
		Category: request.CategorySlug, Version: change.Version, From: string(change.From), To: string(change.To), ActorType: string(change.Actor.Type),
		Reason: change.Reason, FareCents: request.Fare.Cents, Currency: request.Fare.Currency, PassengerPhone: request.PassengerPhone,
		SMSOnAccept: policy != nil && policy.SMSOnAccept, CreatedAt: request.CreatedAt, OccurredAt: change.At,
	})
}

func (u *Usecase) policyOf(ctx context.Context, request *entities.ServiceRequest) (*entities.DispatchPolicy, error) {
	policy, err := u.Config.FindPolicy(ctx, request.PolicyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, ErrPolicyNotFound
	}
	return policy, nil
}

func (u *Usecase) offerEvent(eventType string, request *entities.ServiceRequest, offer *entities.RequestOffer, now time.Time) events.RequestOfferV1 {
	return events.RequestOfferV1{
		Type: eventType, OfferID: offer.ID, RequestID: request.ID, ProviderID: offer.ProviderID, CustomerID: request.CustomerID,
		Category: request.CategorySlug, Version: request.Version, ExpiresAt: offer.ExpiresAt, OccurredAt: now,
	}
}

func newUUID() (uuid.UUID, error) {
	return uuid.NewV7()
}
