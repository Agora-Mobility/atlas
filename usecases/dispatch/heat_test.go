package dispatch

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

// gridGeo puts a point in a 0.1 degree square, so tests can tell cells apart without H3.
type gridGeo struct{ services.IGeoIndex }

func (gridGeo) Cell(point entities.GeoPoint, _ int) (int64, error) {
	return int64(math.Round(-point.Lat*10))*10000 + int64(math.Round(point.Lng*10)), nil
}

func (gridGeo) Center(cell int64) (entities.GeoPoint, error) {
	return entities.GeoPoint{Lat: -float64(cell/10000) / 10, Lng: float64(cell%10000) / 10}, nil
}

func (gridGeo) Format(cell int64) string { return strconv.FormatInt(cell, 10) }

func (gridGeo) InteriorCells([]entities.GeoPolygon, int) ([]int64, error) { return nil, nil }

type heatRequests struct {
	repository.IRequestRepository
	demand map[int64]int64
	flows  []repository.CellFlow
	filter repository.HeatFilter
}

func (r *heatRequests) DemandByCell(_ context.Context, filter repository.HeatFilter) (map[int64]int64, error) {
	r.filter = filter
	return r.demand, nil
}

func (r *heatRequests) FlowsByCell(_ context.Context, filter repository.HeatFilter) ([]repository.CellFlow, error) {
	r.filter = filter
	return r.flows, nil
}

type heatLocator struct {
	repository.IProviderLocator
	live []repository.LiveCandidate
}

func (l heatLocator) AllPositions(context.Context) ([]repository.LiveCandidate, error) {
	return l.live, nil
}

// heatConfig covers everything north of -9.0 with one zone.
type heatConfig struct {
	repository.IDispatchConfigRepository
	zone *entities.ServiceZone
}

func (c heatConfig) ListZones(context.Context, bool) ([]*entities.ServiceZone, error) {
	return []*entities.ServiceZone{c.zone}, nil
}

func (c heatConfig) ZoneCoveringPoint(_ context.Context, point entities.GeoPoint) (*uuid.UUID, error) {
	if point.Lat > -9.0 {
		return &c.zone.ID, nil
	}
	return nil, nil
}

type heatWorld struct {
	usecase  *Usecase
	requests *heatRequests
	zone     *entities.ServiceZone
	clock    time.Time
}

func newHeatWorld(live ...repository.LiveCandidate) *heatWorld {
	w := &heatWorld{requests: &heatRequests{}, zone: &entities.ServiceZone{ID: uuid.New()}, clock: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
	w.usecase = NewUsecase(Deps{Requests: w.requests, Locator: heatLocator{live: live}, Config: heatConfig{zone: w.zone}, Geo: gridGeo{}}, Settings{})
	w.usecase.now = func() time.Time { return w.clock }
	return w
}

func provider(lat, lng float64, categories ...string) repository.LiveCandidate {
	return repository.LiveCandidate{ProviderID: uuid.New(), Point: entities.GeoPoint{Lat: lat, Lng: lng}, Categories: categories}
}

func cellOf(lat, lng float64) int64 {
	cell, _ := gridGeo{}.Cell(entities.GeoPoint{Lat: lat, Lng: lng}, heatResolution)
	return cell
}

func TestHeatMapDefaultsToTheLastDayAndMergesSupplyAndDemand(t *testing.T) {
	w := newHeatWorld(provider(-8.8, 13.2, "taxi"), provider(-8.8, 13.2, "taxi"), provider(-8.9, 13.3, "towing"))
	w.requests.demand = map[int64]int64{cellOf(-8.8, 13.2): 4, cellOf(-8.7, 13.1): 1}

	heat, err := w.usecase.HeatMap(context.Background(), dtos.HeatQuery{})
	require.NoError(t, err)

	assert.Equal(t, w.clock, w.requests.filter.To)
	assert.Equal(t, w.clock.Add(-24*time.Hour), w.requests.filter.From)
	assert.Equal(t, heatResolution, heat.Resolution)
	byCell := map[string]dtos.HeatCell{}
	for _, cell := range heat.Cells {
		byCell[cell.Cell] = cell
	}
	require.Len(t, byCell, 3)
	assert.Equal(t, dtos.HeatCell{Cell: strconv.FormatInt(cellOf(-8.8, 13.2), 10), Lat: -8.8, Lng: 13.2, Supply: 2, Demand: 4}, byCell[strconv.FormatInt(cellOf(-8.8, 13.2), 10)])
	assert.EqualValues(t, 1, byCell[strconv.FormatInt(cellOf(-8.9, 13.3), 10)].Supply, "a cell with only supply still shows")
	assert.EqualValues(t, 1, byCell[strconv.FormatInt(cellOf(-8.7, 13.1), 10)].Demand)
}

func TestHeatMapFiltersSupplyByCategoryAndZone(t *testing.T) {
	w := newHeatWorld(provider(-8.8, 13.2, "taxi", "towing"), provider(-8.8, 13.2, "towing"), provider(-9.5, 13.2, "taxi"))
	from, to := w.clock.Add(-2*time.Hour), w.clock.Add(-time.Hour)

	heat, err := w.usecase.HeatMap(context.Background(), dtos.HeatQuery{From: &from, To: &to, ZoneID: &w.zone.ID, Category: "taxi"})
	require.NoError(t, err)

	assert.Equal(t, repository.HeatFilter{From: from, To: to, ZoneID: &w.zone.ID, Category: "taxi"}, w.requests.filter)
	require.Len(t, heat.Cells, 1, "the towing provider and the one outside the zone are left out")
	assert.EqualValues(t, 1, heat.Cells[0].Supply)
	assert.Equal(t, from, heat.From)
	assert.Equal(t, to, heat.To)
}

func TestHeatMapRejectsABadPeriod(t *testing.T) {
	w := newHeatWorld()
	later, earlier, tooOld := w.clock, w.clock.Add(-time.Hour), w.clock.Add(-32*24*time.Hour)

	for name, query := range map[string]dtos.HeatQuery{
		"ends before it starts": {From: &later, To: &earlier},
		"empty":                 {From: &later, To: &later},
		"longer than 31 days":   {From: &tooOld},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := w.usecase.HeatMap(context.Background(), query)
			require.ErrorIs(t, err, ErrHeatPeriod)
			_, err = w.usecase.Flows(context.Background(), dtos.FlowQuery{HeatQuery: query})
			require.ErrorIs(t, err, ErrHeatPeriod)
		})
	}
}

func TestFlowsHideSmallPairsAndSortByVolume(t *testing.T) {
	w := newHeatWorld()
	a, b, c := cellOf(-8.8, 13.2), cellOf(-8.9, 13.3), cellOf(-8.7, 13.1)
	w.requests.flows = []repository.CellFlow{
		{Origin: a, Destination: b, Requests: 3},
		{Origin: b, Destination: a, Requests: 9},
		{Origin: a, Destination: c, Requests: 2},
		{Origin: c, Destination: c, Requests: 1},
	}

	view, err := w.usecase.Flows(context.Background(), dtos.FlowQuery{HeatQuery: dtos.HeatQuery{Category: "taxi"}})
	require.NoError(t, err)

	assert.Equal(t, "taxi", w.requests.filter.Category)
	assert.Equal(t, defaultFlowMinimum, view.MinRequests)
	assert.EqualValues(t, 15, view.Total)
	assert.EqualValues(t, 3, view.Hidden)
	require.Len(t, view.Flows, 2)
	assert.Equal(t, dtos.CellFlow{
		Origin:      dtos.HeatPoint{Cell: strconv.FormatInt(b, 10), Lat: -8.9, Lng: 13.3},
		Destination: dtos.HeatPoint{Cell: strconv.FormatInt(a, 10), Lat: -8.8, Lng: 13.2},
		Requests:    9,
	}, view.Flows[0])
	assert.EqualValues(t, 3, view.Flows[1].Requests)
}

func TestFlowsStopAtTheLimit(t *testing.T) {
	w := newHeatWorld()
	a, b := cellOf(-8.8, 13.2), cellOf(-8.9, 13.3)
	w.requests.flows = []repository.CellFlow{{Origin: a, Destination: b, Requests: 2}, {Origin: b, Destination: a, Requests: 5}}

	view, err := w.usecase.Flows(context.Background(), dtos.FlowQuery{MinRequests: 1, Limit: 1})
	require.NoError(t, err)

	require.Len(t, view.Flows, 1)
	assert.EqualValues(t, 5, view.Flows[0].Requests)
	assert.EqualValues(t, 2, view.Hidden)
	assert.EqualValues(t, 7, view.Total)
}

func TestFlowsRejectBoundsOutOfRange(t *testing.T) {
	w := newHeatWorld()
	for name, query := range map[string]dtos.FlowQuery{
		"minimum too high": {MinRequests: maxFlowMinimum + 1},
		"limit too high":   {Limit: maxFlowLimit + 1},
		"negative minimum": {MinRequests: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := w.usecase.Flows(context.Background(), query)
			require.ErrorIs(t, err, ErrFlowBounds)
		})
	}
}

func (gridGeo) Covering(center entities.GeoPoint, _ float64, _ int) ([]int64, error) {
	var cells []int64
	for dLat := -1; dLat <= 1; dLat++ {
		for dLng := -1; dLng <= 1; dLng++ {
			cell, _ := gridGeo{}.Cell(entities.GeoPoint{Lat: center.Lat + float64(dLat)/10, Lng: center.Lng + float64(dLng)/10}, heatResolution)
			cells = append(cells, cell)
		}
	}
	return cells, nil
}

func (gridGeo) Boundary(cell int64) ([]entities.GeoPoint, error) {
	center, _ := gridGeo{}.Center(cell)
	return []entities.GeoPoint{{Lat: center.Lat - 0.05, Lng: center.Lng - 0.05}, {Lat: center.Lat + 0.05, Lng: center.Lng + 0.05}}, nil
}

type demandProviders struct {
	repository.IProviderRepository
	profile *entities.Provider
	slug    string
}

func (p demandProviders) Find(context.Context, uuid.UUID) (*entities.Provider, error) {
	return p.profile, nil
}

func (p demandProviders) CategoryOf(context.Context, uuid.UUID) (*entities.LiveIndex, error) {
	return &entities.LiveIndex{Slug: p.slug}, nil
}

type demandLocator struct {
	repository.IProviderLocator
	last *entities.ProviderPosition
}

func (l demandLocator) LastPosition(context.Context, uuid.UUID) (*entities.ProviderPosition, error) {
	return l.last, nil
}

func approvedProvider() *entities.Provider {
	service := uuid.New()
	return &entities.Provider{Status: entities.ProviderApproved, ServiceID: &service}
}

func newDemandWorld(profile *entities.Provider, last *entities.ProviderPosition) *heatWorld {
	w := newHeatWorld()
	w.usecase.Providers = demandProviders{profile: profile, slug: "taxi"}
	w.usecase.Locator = demandLocator{last: last}
	return w
}

func TestProviderDemandShowsLevelsAroundTheProvider(t *testing.T) {
	w := newDemandWorld(approvedProvider(), nil)
	w.requests.demand = map[int64]int64{
		cellOf(-8.8, 13.2): 9, // peak, level 3
		cellOf(-8.9, 13.2): 4, // level 2
		cellOf(-8.7, 13.3): 2, // level 1
		cellOf(-8.8, 13.3): 1, // a single request, hidden
		cellOf(-9.5, 13.2): 9, // far away, hidden
	}

	view, err := w.usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{Position: &entities.GeoPoint{Lat: -8.8, Lng: 13.2}})
	require.NoError(t, err)

	assert.Equal(t, repository.HeatFilter{From: w.clock.Add(-time.Hour), To: w.clock, Category: "taxi"}, w.requests.filter)
	assert.Equal(t, "taxi", view.Category)
	assert.Equal(t, 60, view.WindowMinutes)
	assert.Equal(t, w.clock.Add(time.Minute), view.ExpiresAt)
	levels := map[string]int{}
	for _, cell := range view.Cells {
		levels[cell.Cell] = cell.Level
		assert.NotEmpty(t, cell.Boundary)
	}
	assert.Equal(t, map[string]int{
		strconv.FormatInt(cellOf(-8.8, 13.2), 10): 3,
		strconv.FormatInt(cellOf(-8.9, 13.2), 10): 2,
		strconv.FormatInt(cellOf(-8.7, 13.3), 10): 1,
	}, levels)
}

func TestProviderDemandUsesTheLastPositionAndTheCache(t *testing.T) {
	w := newDemandWorld(approvedProvider(), &entities.ProviderPosition{Point: entities.GeoPoint{Lat: -8.8, Lng: 13.2}})
	w.requests.demand = map[int64]int64{cellOf(-8.8, 13.2): 3}

	view, err := w.usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{})
	require.NoError(t, err)
	require.Len(t, view.Cells, 1)

	w.requests.demand = map[int64]int64{}
	w.clock = w.clock.Add(30 * time.Second)
	cached, err := w.usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{})
	require.NoError(t, err)
	assert.Len(t, cached.Cells, 1, "within the minute the cache answers")

	w.clock = w.clock.Add(time.Minute)
	fresh, err := w.usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{})
	require.NoError(t, err)
	assert.Empty(t, fresh.Cells)
}

func TestProviderDemandNeedsAnApprovedProviderWithAPosition(t *testing.T) {
	pending := approvedProvider()
	pending.Status = entities.ProviderPending

	_, err := newDemandWorld(pending, nil).usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{})
	require.ErrorIs(t, err, ErrProviderNotApproved)

	_, err = newDemandWorld(approvedProvider(), nil).usecase.ProviderDemand(context.Background(), uuid.New(), dtos.DemandQuery{})
	require.ErrorIs(t, err, ErrPositionUnknown)
}

func TestHotLevelSplitsInThirdsOfThePeak(t *testing.T) {
	for _, tc := range []struct {
		requests, peak int64
		level          int
	}{
		{9, 9, 3}, {7, 9, 3}, {6, 9, 2}, {4, 9, 2}, {3, 9, 1}, {2, 9, 1}, {2, 2, 3}, {5, 0, 1},
	} {
		assert.Equal(t, tc.level, hotLevel(tc.requests, tc.peak), "%d of %d", tc.requests, tc.peak)
	}
}
