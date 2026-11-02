package dispatch

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type fakeTx struct{}

func (fakeTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeOutbox struct {
	events []events.Envelope
}

func (o *fakeOutbox) Save(_ context.Context, env events.Envelope) error {
	o.events = append(o.events, env)
	return nil
}

func (o *fakeOutbox) count(eventType string) int {
	total := 0
	for i := range o.events {
		if o.events[i].EventType == eventType {
			total++
		}
	}
	return total
}

type fakeAudit struct{ repository.IAuditLogRepository }

type fakeConfig struct {
	repository.IDispatchConfigRepository
	policy *entities.DispatchPolicy
	zone   *entities.ServiceZone
}

func (c *fakeConfig) FindPolicy(context.Context, uuid.UUID) (*entities.DispatchPolicy, error) {
	return c.policy, nil
}

func (c *fakeConfig) ResolvePolicy(context.Context, uuid.UUID, uuid.UUID) (*entities.DispatchPolicy, error) {
	return c.policy, nil
}

func (c *fakeConfig) ListZones(context.Context, bool) ([]*entities.ServiceZone, error) {
	return []*entities.ServiceZone{c.zone}, nil
}

func (c *fakeConfig) ZoneCoveringPoint(context.Context, entities.GeoPoint) (*uuid.UUID, error) {
	return nil, nil
}

type fakeRequests struct {
	repository.IRequestRepository
	byID    map[uuid.UUID]*entities.ServiceRequest
	changes []*entities.RequestStatusChange
	failure error
}

func (r *fakeRequests) Find(_ context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	return r.byID[id], r.failure
}

func (r *fakeRequests) FindForUpdate(_ context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	return r.byID[id], r.failure
}

func (r *fakeRequests) TryLock(_ context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	return r.byID[id], r.failure
}

func (r *fakeRequests) Update(context.Context, *entities.ServiceRequest, int) error { return nil }

func (r *fakeRequests) RecordChange(_ context.Context, _ uuid.UUID, change *entities.RequestStatusChange) error {
	r.changes = append(r.changes, change)
	return nil
}

func (r *fakeRequests) ActiveOfProvider(context.Context, uuid.UUID) (*entities.ServiceRequest, error) {
	return nil, nil
}

func (r *fakeRequests) ListFareAdjustments(context.Context, uuid.UUID) ([]entities.FareAdjustment, error) {
	return nil, nil
}

func (r *fakeRequests) AddFareAdjustment(context.Context, entities.FareAdjustment) error {
	return nil
}

type fakeOffers struct {
	repository.IOfferRepository
	byID map[uuid.UUID]*entities.RequestOffer
}

func (o *fakeOffers) Create(_ context.Context, offer *entities.RequestOffer) error {
	for _, existing := range o.byID {
		if existing.RequestID == offer.RequestID && existing.ProviderID == offer.ProviderID && existing.BoostBps >= offer.BoostBps {
			return repository.ErrOfferAlreadySent
		}
	}
	o.byID[offer.ID] = offer
	return nil
}

func (o *fakeOffers) Find(_ context.Context, id uuid.UUID) (*entities.RequestOffer, error) {
	return o.byID[id], nil
}

func (o *fakeOffers) FindForUpdate(_ context.Context, id uuid.UUID) (*entities.RequestOffer, error) {
	return o.byID[id], nil
}

func (o *fakeOffers) Update(context.Context, *entities.RequestOffer) error { return nil }

func (o *fakeOffers) ListOfRequest(_ context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error) {
	var out []*entities.RequestOffer
	for _, offer := range o.byID {
		if offer.RequestID == requestID {
			out = append(out, offer)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SentAt.Before(out[j].SentAt) })
	return out, nil
}

func (o *fakeOffers) ListOfRequestForUpdate(ctx context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error) {
	return o.ListOfRequest(ctx, requestID)
}

type fakeProviders struct {
	repository.IProviderRepository
	eligible map[uuid.UUID]entities.EligibleProvider
	active   []uuid.UUID
	covering []uuid.UUID
}

func (p *fakeProviders) FilterEligible(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]entities.EligibleProvider, error) {
	var out []entities.EligibleProvider
	for _, id := range ids {
		if provider, ok := p.eligible[id]; ok {
			out = append(out, provider)
		}
	}
	return out, nil
}

func (p *fakeProviders) WithActiveJob(context.Context, []uuid.UUID, time.Time) ([]uuid.UUID, error) {
	return p.active, nil
}

func (p *fakeProviders) CoveringCell(context.Context, uuid.UUID, int64) ([]uuid.UUID, error) {
	return p.covering, nil
}

type fakeLocator struct {
	repository.IProviderLocator
	mu       sync.Mutex
	live     []repository.LiveCandidate
	locks    map[uuid.UUID]uuid.UUID
	released []uuid.UUID
	failure  error
}

func (l *fakeLocator) FindInCells(context.Context, string, int, []int64) ([]repository.LiveCandidate, error) {
	return l.live, l.failure
}

func (l *fakeLocator) IsReserved(_ context.Context, providerID uuid.UUID) (bool, error) {
	_, reserved := l.locks[providerID]
	return reserved, nil
}

func (l *fakeLocator) ReservedAmong(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	reserved := map[uuid.UUID]struct{}{}
	for _, id := range ids {
		if _, ok := l.locks[id]; ok {
			reserved[id] = struct{}{}
		}
	}
	return reserved, nil
}

func (l *fakeLocator) Reserve(_ context.Context, providerID, offerID uuid.UUID, _ time.Duration) (bool, error) {
	if _, reserved := l.locks[providerID]; reserved {
		return false, nil
	}
	l.locks[providerID] = offerID
	return true, nil
}

func (l *fakeLocator) Release(_ context.Context, providerID, _ uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, providerID)
	l.released = append(l.released, providerID)
	return nil
}

func (l *fakeLocator) SetTrip(context.Context, uuid.UUID, entities.ActiveTrip) error { return nil }
func (l *fakeLocator) ClearTrip(context.Context, uuid.UUID) error                    { return nil }
func (l *fakeLocator) LastPosition(context.Context, uuid.UUID) (*entities.ProviderPosition, error) {
	return nil, nil
}

type fakeGeo struct{ services.IGeoIndex }

func (fakeGeo) Cell(entities.GeoPoint, int) (int64, error) { return 1, nil }
func (fakeGeo) Disk(int64, int) ([]int64, error)           { return []int64{1}, nil }
func (fakeGeo) RingsFor(float64, int) int                  { return 1 }
func (fakeGeo) InteriorCells([]entities.GeoPolygon, int) ([]int64, error) {
	return []int64{1}, nil
}

type fakeChannels struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (c *fakeChannels) SendOffer(_ context.Context, channel string, _ OfferNotice) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, channel)
	return c.err
}

func (c *fakeChannels) channels() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.sent...)
}

var errBoom = errors.New("boom")

type etaLocator struct{ *fakeLocator }

func (etaLocator) LastPosition(context.Context, uuid.UUID) (*entities.ProviderPosition, error) {
	return &entities.ProviderPosition{Point: pickupPoint}, nil
}

type etaRouter struct {
	services.IRoutingProvider
	etas []time.Duration
}

func (r etaRouter) ETAs(context.Context, []entities.GeoPoint, entities.GeoPoint) ([]time.Duration, error) {
	return r.etas, nil
}

type fakePayments struct{ repository.IPaymentRepository }

func (fakePayments) HasOutstanding(context.Context, uuid.UUID) (bool, error) { return false, nil }

func (fakePayments) IsMethodEnabled(context.Context, uuid.UUID, entities.PaymentMethod) (bool, error) {
	return true, nil
}

func (fakePayments) ListOfRequest(context.Context, uuid.UUID) ([]entities.Payment, error) {
	return nil, nil
}
