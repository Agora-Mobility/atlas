package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

const (
	demandArea  = 7
	taxiIndexes = 8
)

type demandConfig struct {
	repository.IDispatchConfigRepository
	indexes []entities.LiveIndex
}

func (c demandConfig) ListLiveIndexes(context.Context) ([]entities.LiveIndex, error) {
	return c.indexes, nil
}

type fakeDemand struct {
	repository.IDemandRepository
	configs   []*entities.DemandPricing
	pending   map[int64][]uuid.UUID
	snapshots []entities.DemandSnapshot
}

func (d *fakeDemand) ListDemandPricing(context.Context) ([]*entities.DemandPricing, error) {
	return d.configs, nil
}

func (d *fakeDemand) FindDemandPricing(context.Context, uuid.UUID, uuid.UUID) (*entities.DemandPricing, error) {
	if len(d.configs) == 0 {
		return nil, nil
	}
	return d.configs[0], nil
}

func (d *fakeDemand) PendingCustomers(context.Context, uuid.UUID, uuid.UUID) (map[int64][]uuid.UUID, error) {
	return d.pending, nil
}

func (d *fakeDemand) SaveSnapshots(_ context.Context, snapshots []entities.DemandSnapshot) error {
	d.snapshots = append(d.snapshots, snapshots...)
	return nil
}

func (d *fakeDemand) PurgeSnapshots(context.Context, time.Time) (int64, error) { return 0, nil }

type published struct {
	ratios, factors map[int64]int
}

type fakeSignals struct {
	repository.IDemandSignals
	busy      bool
	freeCells map[int64]int
	previous  map[int64]int
	quotes    map[int64][]uuid.UUID
	factor    int
	recorded  []int64
	published []published
}

func (s *fakeSignals) ClaimRefresh(context.Context, time.Duration) (bool, error) { return !s.busy, nil }

func (s *fakeSignals) RecordQuote(_ context.Context, _ repository.DemandKey, area int64, _ uuid.UUID, _ time.Time) error {
	s.recorded = append(s.recorded, area)
	return nil
}

func (s *fakeSignals) RecentQuotes(context.Context, repository.DemandKey, time.Time, time.Time) (map[int64][]uuid.UUID, error) {
	return s.quotes, nil
}

func (s *fakeSignals) FreeProviders(_ context.Context, _ string, _ int, cells []int64) (int, error) {
	free := 0
	for _, cell := range cells {
		free += s.freeCells[cell]
	}
	return free, nil
}

func (s *fakeSignals) Ratios(context.Context, repository.DemandKey) (map[int64]int, error) {
	return s.previous, nil
}

func (s *fakeSignals) Publish(_ context.Context, _ repository.DemandKey, ratios, factors map[int64]int, _ time.Duration) error {
	s.published = append(s.published, published{ratios: ratios, factors: factors})
	return nil
}

func (s *fakeSignals) Factor(context.Context, repository.DemandKey, int64) (int, error) {
	return s.factor, nil
}

type demandWorld struct {
	usecase *Usecase
	demand  *fakeDemand
	signals *fakeSignals
	pricing *entities.DemandPricing
	zone    *entities.ServiceZone
	area    int64
	cell    int64
}

func newDemandWorld(t *testing.T) *demandWorld {
	t.Helper()
	geo := h3index.New()
	categoryID := uuid.New()
	zone := &entities.ServiceZone{ID: uuid.New(), Currency: "AOA", Timezone: "Africa/Luanda"}
	pricing := &entities.DemandPricing{
		ID: uuid.New(), ZoneID: zone.ID, CategoryID: categoryID, Mode: entities.DemandOn, MaxFactorBps: 13_000,
		Tiers: []entities.DemandTier{{AboveRatioBps: 10_000, FactorBps: 11_000}, {AboveRatioBps: 15_000, FactorBps: 12_000}, {AboveRatioBps: 20_000, FactorBps: 13_000}},
	}
	area, err := geo.Cell(pickupPoint, demandArea)
	require.NoError(t, err)
	cell, err := geo.Cell(pickupPoint, taxiIndexes)
	require.NoError(t, err)
	w := &demandWorld{
		demand: &fakeDemand{configs: []*entities.DemandPricing{pricing}}, signals: &fakeSignals{factor: entities.NeutralDemandFactorBps},
		pricing: pricing, zone: zone, area: area, cell: cell,
	}
	w.usecase = NewUsecase(Deps{
		Config: demandConfig{indexes: []entities.LiveIndex{
			{CategoryID: categoryID, Slug: "taxi", Resolution: 6},
			{CategoryID: categoryID, Slug: "taxi", Resolution: taxiIndexes},
		}},
		Demand: w.demand, Signals: w.signals, Geo: geo,
	}, Settings{DemandAreaResolution: demandArea, DemandQuoteWindow: 3 * time.Minute, DemandFactorTTL: 3 * time.Minute})
	w.usecase.now = func() time.Time { return time.Date(2026, 11, 10, 23, 0, 0, 0, time.UTC) }
	return w
}

func customers(n int) []uuid.UUID {
	ids := make([]uuid.UUID, 0, n)
	for range n {
		ids = append(ids, uuid.New())
	}
	return ids
}

func (w *demandWorld) service() *entities.DispatchService {
	return &entities.DispatchService{ID: uuid.New(), CategoryID: w.pricing.CategoryID, CategorySlug: "taxi"}
}

func TestRefreshDemandRaisesThePriceWhereRequestsOutnumberFreeProviders(t *testing.T) {
	w := newDemandWorld(t)
	waiting := customers(15)
	w.demand.pending = map[int64][]uuid.UUID{w.cell: waiting}
	w.signals.quotes = map[int64][]uuid.UUID{w.area: append(customers(3), waiting[:5]...)}
	w.signals.freeCells = map[int64]int{w.cell: 10}
	w.signals.previous = map[int64]int{w.area: 17_272}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	require.Len(t, w.signals.published, 1)
	assert.Equal(t, map[int64]int{w.area: 17_272}, w.signals.published[0].ratios)
	assert.Equal(t, map[int64]int{w.area: 12_000}, w.signals.published[0].factors)
	require.Len(t, w.demand.snapshots, 1)
	snapshot := w.demand.snapshots[0]
	assert.Equal(t, 18, snapshot.Pending)
	assert.Equal(t, 10, snapshot.Free)
	assert.Equal(t, 12_000, snapshot.FactorBps)
	assert.Equal(t, w.zone.ID, snapshot.ZoneID)
}

func TestRefreshDemandCountsEachCustomerOnce(t *testing.T) {
	w := newDemandWorld(t)
	customer := uuid.New()
	w.demand.pending = map[int64][]uuid.UUID{w.cell: {customer}}
	w.signals.quotes = map[int64][]uuid.UUID{w.area: {customer, customer}}
	w.signals.freeCells = map[int64]int{w.cell: 1}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	assert.Empty(t, w.signals.published[0].factors, "one customer who quoted and requested is one customer, and one free provider serves them")
	assert.Empty(t, w.demand.snapshots)
}

func TestRefreshDemandObservesWithoutChargingAndStillRecordsTheFactor(t *testing.T) {
	w := newDemandWorld(t)
	w.pricing.Mode = entities.DemandObserve
	w.demand.pending = map[int64][]uuid.UUID{w.cell: customers(3)}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	assert.Equal(t, map[int64]int{w.area: 13_000}, w.signals.published[0].factors)
	require.Len(t, w.demand.snapshots, 1)
	assert.Equal(t, 13_000, w.demand.snapshots[0].FactorBps)

	w.signals.factor = 13_000
	assert.Equal(t, entities.NeutralDemand(), w.usecase.demandFor(context.Background(), uuid.New(), w.zone, w.service(), pickupPoint, false, w.usecase.now()))
	assert.Equal(t, []int64{w.area}, w.signals.recorded, "observing still counts the quote")
}

func TestRefreshDemandRisesQuicklyFromNormal(t *testing.T) {
	w := newDemandWorld(t)
	w.demand.pending = map[int64][]uuid.UUID{w.cell: customers(3)}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	assert.Equal(t, map[int64]int{w.area: 31_000}, w.signals.published[0].ratios)
	assert.Equal(t, map[int64]int{w.area: 13_000}, w.signals.published[0].factors)
}

func TestRefreshDemandLetsTheFactorFallSlowlyAndThenForgetsTheArea(t *testing.T) {
	w := newDemandWorld(t)
	w.signals.freeCells = map[int64]int{w.cell: 4}
	w.signals.previous = map[int64]int{w.area: 20_000}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))
	assert.Equal(t, map[int64]int{w.area: 15_500}, w.signals.published[0].ratios)
	assert.Equal(t, map[int64]int{w.area: 12_000}, w.signals.published[0].factors)

	w.signals.previous = map[int64]int{w.area: 10_050}
	require.NoError(t, w.usecase.RefreshDemand(context.Background()))
	assert.Empty(t, w.signals.published[1].ratios)
	assert.Empty(t, w.signals.published[1].factors)
}

func TestRefreshDemandClearsAZoneThatWasSwitchedOff(t *testing.T) {
	w := newDemandWorld(t)
	w.pricing.Mode = entities.DemandOff
	w.demand.pending = map[int64][]uuid.UUID{w.cell: customers(30)}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	require.Len(t, w.signals.published, 1)
	assert.Nil(t, w.signals.published[0].factors)
	assert.Empty(t, w.demand.snapshots)
}

func TestRefreshDemandRunsOnOneInstanceAtATime(t *testing.T) {
	w := newDemandWorld(t)
	w.signals.busy = true
	w.demand.pending = map[int64][]uuid.UUID{w.cell: customers(30)}

	require.NoError(t, w.usecase.RefreshDemand(context.Background()))

	assert.Empty(t, w.signals.published)
}

func TestDemandForQuotesTheCurrentFactorWithinTheCap(t *testing.T) {
	w := newDemandWorld(t)
	w.pricing.MaxFactorBps, w.pricing.Tiers[2].FactorBps = 12_000, 12_000
	w.signals.factor = 13_000

	demand := w.usecase.demandFor(context.Background(), uuid.New(), w.zone, w.service(), pickupPoint, false, w.usecase.now())

	assert.Equal(t, entities.DemandQuote{FactorBps: 12_000}, demand)
	assert.Equal(t, []int64{w.area}, w.signals.recorded)
}

func TestDemandForLeavesScheduledAndUnconfiguredRidesAtTheNormalPrice(t *testing.T) {
	w := newDemandWorld(t)
	w.signals.factor = 13_000

	assert.Equal(t, entities.NeutralDemand(), w.usecase.demandFor(context.Background(), uuid.New(), w.zone, w.service(), pickupPoint, true, w.usecase.now()))
	assert.Empty(t, w.signals.recorded)

	w.pricing.Mode = entities.DemandOff
	assert.Equal(t, entities.NeutralDemand(), w.usecase.demandFor(context.Background(), uuid.New(), w.zone, w.service(), pickupPoint, false, w.usecase.now()))

	w.demand.configs = nil
	assert.Equal(t, entities.NeutralDemand(), w.usecase.demandFor(context.Background(), uuid.New(), w.zone, w.service(), pickupPoint, false, w.usecase.now()))
}

func TestDemandIndexPrefersTheFinestIndexCoveringTheArea(t *testing.T) {
	u := &Usecase{settings: Settings{DemandAreaResolution: demandArea}}
	categoryID := uuid.New()

	index, ok := u.demandIndex([]entities.LiveIndex{{CategoryID: categoryID, Resolution: 9}, {CategoryID: categoryID, Resolution: 5}, {CategoryID: categoryID, Resolution: 8}}, categoryID)
	assert.True(t, ok)
	assert.Equal(t, 8, index.Resolution)

	index, _ = u.demandIndex([]entities.LiveIndex{{CategoryID: categoryID, Resolution: 5}, {CategoryID: categoryID, Resolution: 6}}, categoryID)
	assert.Equal(t, 6, index.Resolution)

	_, ok = u.demandIndex(nil, categoryID)
	assert.False(t, ok)
}
