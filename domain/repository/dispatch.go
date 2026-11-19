package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var (
	ErrRequestVersionConflict = errors.New("the request changed in the meantime")
	ErrDuplicateIdempotency   = errors.New("this idempotency key was already used")
	ErrOfferAlreadySent       = errors.New("this provider already has an offer for the request")
	ErrInvalidGeometry        = errors.New("invalid zone geometry")
)

type IDispatchConfigRepository interface {
	ListZones(ctx context.Context, onlyActive bool) ([]*entities.ServiceZone, error)
	FindZone(ctx context.Context, id uuid.UUID) (*entities.ServiceZone, error)
	CreateZone(ctx context.Context, zone *entities.ServiceZone) error
	UpdateZone(ctx context.Context, zone *entities.ServiceZone) error
	ZoneCoveringPoint(ctx context.Context, point entities.GeoPoint) (*uuid.UUID, error)
	IsValidGeometry(ctx context.Context, geojson string) (bool, error)
	ResolvePolicy(ctx context.Context, categoryID, zoneID uuid.UUID) (*entities.DispatchPolicy, error)
	FindPolicy(ctx context.Context, id uuid.UUID) (*entities.DispatchPolicy, error)
	ListPolicies(ctx context.Context) ([]*entities.DispatchPolicy, error)
	CreatePolicy(ctx context.Context, policy *entities.DispatchPolicy) error
	UpdatePolicy(ctx context.Context, policy *entities.DispatchPolicy) error
	ListLiveIndexes(ctx context.Context) ([]entities.LiveIndex, error)
	FindService(ctx context.Context, id uuid.UUID) (*entities.DispatchService, error)
	ListServicesOfCategory(ctx context.Context, slug string) ([]entities.DispatchService, error)
	FindCategoryIDBySlug(ctx context.Context, slug string) (*uuid.UUID, error)
}

type IPricingRepository interface {
	CurrentTariff(ctx context.Context, serviceID, zoneID uuid.UUID) (*entities.ServiceTariff, error)
	FindTariff(ctx context.Context, id uuid.UUID) (*entities.ServiceTariff, error)
	ListCurrentTariffs(ctx context.Context) ([]*entities.ServiceTariff, error)
	ReplaceTariff(ctx context.Context, tariff *entities.ServiceTariff) error
	CreateEstimate(ctx context.Context, estimate *entities.FareEstimate) error
	FindEstimate(ctx context.Context, id uuid.UUID) (*entities.FareEstimate, error)
	UseEstimate(ctx context.Context, id uuid.UUID, now time.Time) (bool, error)
	RecordETA(ctx context.Context, originCell, destinationCell int64, hourBand int, duration time.Duration) error
	LearnedETA(ctx context.Context, originCell, destinationCell int64, hourBand int) (time.Duration, int, error)
	TrackedDistance(ctx context.Context, requestID uuid.UUID, since time.Time) (meters, samples int, err error)
	SaveTripMetrics(ctx context.Context, metrics entities.TripMetrics) error
	FindTripMetrics(ctx context.Context, requestID uuid.UUID) (*entities.TripMetrics, error)
}

type IProviderRepository interface {
	Create(ctx context.Context, userID uuid.UUID) error
	Find(ctx context.Context, userID uuid.UUID) (*entities.Provider, error)
	Save(ctx context.Context, provider *entities.Provider) error
	Applications(ctx context.Context, filter ProviderApplicationFilter) ([]ProviderApplication, int64, error)
	CategoryOf(ctx context.Context, userID uuid.UUID) (*entities.LiveIndex, error)
	ClearCoverage(ctx context.Context, providerID uuid.UUID) error
	FilterEligible(ctx context.Context, serviceID uuid.UUID, providerIDs []uuid.UUID) ([]entities.EligibleProvider, error)
	WithActiveJob(ctx context.Context, providerIDs []uuid.UUID, busyUntil time.Time) ([]uuid.UUID, error)
	ReplaceCoverage(ctx context.Context, providerID uuid.UUID, base entities.GeoPoint, radiusKm float64, cellsByCategory map[uuid.UUID][]int64) error
	CoveringCell(ctx context.Context, categoryID uuid.UUID, cell int64) ([]uuid.UUID, error)
	TouchJobCompleted(ctx context.Context, providerID uuid.UUID, at time.Time) error
	SaveLocationSample(ctx context.Context, providerID, requestID uuid.UUID, point entities.GeoPoint, at time.Time) error
	PurgeLocationSamples(ctx context.Context, before time.Time) (int64, error)
}

type IRequestRepository interface {
	Create(ctx context.Context, request *entities.ServiceRequest) error
	Find(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error)
	FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error)
	TryLock(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error)
	FindByIdempotencyKey(ctx context.Context, customerID uuid.UUID, key string) (*entities.ServiceRequest, error)
	Update(ctx context.Context, request *entities.ServiceRequest, expectedVersion int) error
	RecordChange(ctx context.Context, requestID uuid.UUID, change *entities.RequestStatusChange) error
	DueRequests(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error)
	DueReminders(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error)
	ActiveOfCustomer(ctx context.Context, customerID uuid.UUID) (*entities.ServiceRequest, error)
	ActiveOfProvider(ctx context.Context, providerID uuid.UUID) (*entities.ServiceRequest, error)
	ListOfCustomer(ctx context.Context, filter RequestFilter) ([]*entities.ServiceRequest, int64, error)
	// ListOfProvider lists the requests assigned to filter.ProviderID, most recent first.
	ListOfProvider(ctx context.Context, filter RequestFilter) ([]*entities.ServiceRequest, int64, error)
	OperatorQueue(ctx context.Context, limit, offset int) ([]*entities.ServiceRequest, error)
	AddFareAdjustment(ctx context.Context, adjustment entities.FareAdjustment) error
	ListFareAdjustments(ctx context.Context, requestID uuid.UUID) ([]entities.FareAdjustment, error)
	DemandByCell(ctx context.Context, filter HeatFilter) (map[int64]int64, error)
	FlowsByCell(ctx context.Context, filter HeatFilter) ([]CellFlow, error)
	ListWithProviderOnTheWay(ctx context.Context, limit int) ([]*entities.ServiceRequest, error)
	AdminList(ctx context.Context, filter AdminRequestFilter) ([]AdminRequest, int64, error)
	Summary(ctx context.Context, filter AdminRequestFilter) ([]RequestSummaryRow, error)
	History(ctx context.Context, requestID uuid.UUID) ([]RequestHistoryEntry, error)
	OrganizationOf(ctx context.Context, requestID uuid.UUID) (*uuid.UUID, error)
	LockCustomer(ctx context.Context, customerID uuid.UUID) error
	NextPublicCode(ctx context.Context, categoryID uuid.UUID) (string, error)
	MarkStopArrived(ctx context.Context, requestID uuid.UUID, position int, at time.Time) (bool, error)
	RecentDestinations(ctx context.Context, customerID uuid.UUID, limit int) ([]RecentDestination, error)
	ActiveOfCustomerService(ctx context.Context, customerID, serviceID uuid.UUID) ([]*entities.ServiceRequest, error)
	PassengerNotice(ctx context.Context, requestID uuid.UUID) (*PassengerNotice, error)
}

type RecentDestination struct {
	Point     entities.GeoPoint
	Line      string
	Reference string
	UsedAt    time.Time
}

type PassengerNotice struct {
	Template     string
	ProviderName string
	VehiclePlate string
}

type HeatFilter struct {
	From     time.Time
	To       time.Time
	ZoneID   *uuid.UUID
	Category string
}

type CellFlow struct {
	Origin      int64
	Destination int64
	Requests    int64
}

type AdminRequestFilter struct {
	Statuses       []string
	Category       string
	ServiceID      *uuid.UUID
	ZoneID         *uuid.UUID
	OrganizationID *uuid.UUID
	ProviderID     *uuid.UUID
	CustomerID     *uuid.UUID
	From           *time.Time
	To             *time.Time
	RouteReview    bool
	Search         string
	Sort           string
	Limit          int
	Offset         int
}

type AdminRequest struct {
	ID               uuid.UUID
	Code             string
	Status           string
	Version          int
	Category         string
	CategoryName     string
	ServiceID        uuid.UUID
	ServiceName      string
	CustomerID       uuid.UUID
	CustomerName     string
	ProviderID       *uuid.UUID
	ProviderName     string
	OrganizationID   *uuid.UUID
	OrganizationName string
	ZoneID           uuid.UUID
	ZoneName         string
	FareCents        *int64
	Currency         string
	NeedsOperator    bool
	ScheduledFor     *time.Time
	CreatedAt        time.Time
	CompletedAt      *time.Time
	PickupLine       string
	PickupLat        float64
	PickupLng        float64
	DestinationLine  string
	DeviationBps     *int
	RouteReview      bool
}

type RequestSummaryRow struct {
	Category         string
	CategoryName     string
	ServiceID        uuid.UUID
	ServiceName      string
	Status           string
	ZoneID           uuid.UUID
	ZoneName         string
	OrganizationID   *uuid.UUID
	OrganizationName string
	Currency         string
	Total            int64
	FareCents        int64
}

type RequestHistoryEntry struct {
	From       string
	To         string
	Version    int
	ActorType  string
	ActorID    *uuid.UUID
	Reason     string
	OccurredAt time.Time
}

type RequestFilter struct {
	CustomerID uuid.UUID
	ProviderID uuid.UUID
	Statuses   []string
	Category   string
	Limit      int
	Offset     int
}

type IOfferRepository interface {
	Create(ctx context.Context, offer *entities.RequestOffer) error
	Find(ctx context.Context, id uuid.UUID) (*entities.RequestOffer, error)
	FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.RequestOffer, error)
	Update(ctx context.Context, offer *entities.RequestOffer) error
	ListOfRequest(ctx context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error)
	ListOfRequestForUpdate(ctx context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error)
	Due(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error)
	OpenOfProvider(ctx context.Context, providerID uuid.UUID, now time.Time) ([]*entities.RequestOffer, error)
	// HistoryOfProvider lists every offer sent to the provider, most recent first.
	HistoryOfProvider(ctx context.Context, providerID uuid.UUID, statuses []string, limit, offset int) ([]*entities.RequestOffer, int64, error)
}

type IDeviceRepository interface {
	Register(ctx context.Context, userID uuid.UUID, token, platform string) error
	Unregister(ctx context.Context, userID uuid.UUID, token string) error
	Remove(ctx context.Context, token string) error
	Tokens(ctx context.Context, userID uuid.UUID) ([]string, error)
	Contact(ctx context.Context, userID uuid.UUID) (name, phone string, err error)
	RecordNotification(ctx context.Context, record NotificationRecord) error
	Delivered(ctx context.Context, deliveryKey string) (bool, error)
}

// IRidePushTokenRepository keeps the push tokens of the lock screen of a ride,
// one per platform, until the ride ends.
type IRidePushTokenRepository interface {
	Save(ctx context.Context, token entities.RidePushToken) error
	OfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.RidePushToken, error)
	DeleteOfRequest(ctx context.Context, requestID uuid.UUID) error
	Remove(ctx context.Context, token string) error
}

type NotificationRecord struct {
	UserID            *uuid.UUID
	Channel           string
	Template          string
	Status            string
	ProviderMessageID string
	Error             string
	DeliveryKey       string
}

type LiveCandidate struct {
	ProviderID uuid.UUID
	Point      entities.GeoPoint
	RecordedAt time.Time
	Categories []string
}

type IProviderLocator interface {
	SetOnline(ctx context.Context, providerID uuid.UUID, indexes []entities.LiveIndex, ttl time.Duration) error
	SetOffline(ctx context.Context, providerID uuid.UUID) error
	IsOnline(ctx context.Context, providerID uuid.UUID) (bool, error)
	LastPosition(ctx context.Context, providerID uuid.UUID) (*entities.ProviderPosition, error)
	UpdatePosition(ctx context.Context, position entities.ProviderPosition, ttl time.Duration) error
	Touch(ctx context.Context, providerID uuid.UUID, ttl time.Duration) error
	FindInCells(ctx context.Context, category string, resolution int, cells []int64) ([]LiveCandidate, error)
	CountInCells(ctx context.Context, category string, resolution int, cells []int64) (map[int64]int64, error)
	AllPositions(ctx context.Context) ([]LiveCandidate, error)
	Reserve(ctx context.Context, providerID, offerID uuid.UUID, ttl time.Duration) (bool, error)
	Release(ctx context.Context, providerID, offerID uuid.UUID) error
	IsReserved(ctx context.Context, providerID uuid.UUID) (bool, error)
	ReservedAmong(ctx context.Context, providerIDs []uuid.UUID) (map[uuid.UUID]struct{}, error)
	SetTrip(ctx context.Context, providerID uuid.UUID, trip entities.ActiveTrip) error
	ClearTrip(ctx context.Context, providerID uuid.UUID) error
	Trip(ctx context.Context, providerID uuid.UUID) (*entities.ActiveTrip, error)
	ClaimSample(ctx context.Context, providerID uuid.UUID, every time.Duration) (bool, error)
	ClaimLostAlert(ctx context.Context, requestID uuid.UUID, every time.Duration) (bool, error)
	// ClaimRideLivePush is true at most once per every for a request: the lock
	// screen gets a position update no more often than that.
	ClaimRideLivePush(ctx context.Context, requestID uuid.UUID, every time.Duration) (bool, error)
}
