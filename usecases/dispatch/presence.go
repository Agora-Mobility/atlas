package dispatch

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func (u *Usecase) GoOnline(ctx context.Context, providerID uuid.UUID, in dtos.OnlineInput) (*dtos.CadenceView, error) {
	profile, err := u.Providers.FindProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != entities.ProviderApproved {
		return nil, ErrProviderNotApproved
	}
	indexes, err := u.liveIndexesOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	cadence := entities.CadenceFor(entities.PhaseIdle, false, false)
	if err := u.Locator.SetOnline(ctx, providerID, indexes, cadence.PresenceTTL()); err != nil {
		return nil, err
	}
	if err := u.restoreTrip(ctx, providerID); err != nil {
		return nil, err
	}
	if in.Position != nil {
		return u.ReportPositions(ctx, providerID, dtos.LocationsInput{Positions: []dtos.PositionInput{*in.Position}})
	}
	return cadenceView(entities.PhaseIdle, cadence, 0, 0), nil
}

func (u *Usecase) restoreTrip(ctx context.Context, providerID uuid.UUID) error {
	active, err := u.Requests.ActiveOfProvider(ctx, providerID)
	if err != nil || active == nil {
		return err
	}
	policy, err := u.policyOf(ctx, active)
	if err != nil {
		return err
	}
	return u.Locator.SetTrip(ctx, providerID, tripFor(active, policy))
}

func (u *Usecase) GoOffline(ctx context.Context, providerID uuid.UUID) (*dtos.CadenceView, error) {
	if err := u.Locator.SetOffline(ctx, providerID); err != nil {
		return nil, err
	}
	return cadenceView(entities.PhaseOffline, entities.CadenceFor(entities.PhaseOffline, false, false), 0, 0), nil
}

func (u *Usecase) liveIndexesOf(ctx context.Context, providerID uuid.UUID) ([]entities.LiveIndex, error) {
	category, err := u.Providers.CategoryOf(ctx, providerID)
	if err != nil || category == nil {
		return nil, err
	}
	all, err := u.Config.ListLiveIndexes(ctx)
	if err != nil {
		return nil, err
	}
	indexes := make([]entities.LiveIndex, 0, 1)
	for _, index := range all {
		if index.CategoryID == category.CategoryID {
			indexes = append(indexes, index)
		}
	}
	return indexes, nil
}

func (u *Usecase) ReportPositions(ctx context.Context, providerID uuid.UUID, in dtos.LocationsInput) (*dtos.CadenceView, error) {
	trip, err := u.Locator.Trip(ctx, providerID)
	if err != nil {
		return nil, err
	}
	online, err := u.Locator.IsOnline(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !online {
		if trip == nil {
			view := cadenceView(entities.PhaseOffline, entities.CadenceFor(entities.PhaseOffline, false, false), 0, len(in.Positions))
			return view, nil
		}
		indexes, err := u.indexesForActiveTrip(ctx, providerID)
		if err != nil {
			return nil, err
		}
		if err := u.Locator.SetOnline(ctx, providerID, indexes, entities.CadenceFor(entities.PhaseIdle, false, false).PresenceTTL()); err != nil {
			return nil, err
		}
	}

	last, accepted, rejected, err := u.acceptPositions(ctx, providerID, in.Positions)
	if err != nil {
		return nil, err
	}
	businessmetrics.ProviderPositions.WithLabelValues("accepted").Add(float64(accepted))
	businessmetrics.ProviderPositions.WithLabelValues("rejected").Add(float64(rejected))

	reserved, err := u.Locator.IsReserved(ctx, providerID)
	if err != nil {
		return nil, err
	}
	phase := phaseOf(trip, reserved)
	cadence := entities.CadenceFor(phase, in.BatteryLow, in.NetworkWeak)
	ttl := cadence.PresenceTTL()
	if !cadence.SendsGPS {
		ttl = entities.CadenceFor(entities.PhaseIdle, false, false).PresenceTTL()
	}
	if last != nil {
		if err := u.Locator.UpdatePosition(ctx, *last, ttl); err != nil {
			return nil, err
		}
		if trip != nil && trip.Tracked {
			u.shareWithCustomer(ctx, providerID, *trip, *last)
		}
	} else if err := u.Locator.Touch(ctx, providerID, ttl); err != nil {
		return nil, err
	}
	return cadenceView(phase, cadence, accepted, rejected), nil
}

func (u *Usecase) indexesForActiveTrip(ctx context.Context, providerID uuid.UUID) ([]entities.LiveIndex, error) {
	profile, err := u.Providers.FindProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != entities.ProviderApproved {
		return nil, nil
	}
	return u.liveIndexesOf(ctx, providerID)
}

func phaseOf(trip *entities.ActiveTrip, reserved bool) entities.ProviderPhase {
	if trip != nil {
		return trip.Phase()
	}
	if reserved {
		return entities.PhaseOffered
	}
	return entities.PhaseIdle
}

func (u *Usecase) acceptPositions(ctx context.Context, providerID uuid.UUID, batch []dtos.PositionInput) (*entities.ProviderPosition, int, int, error) {
	previous, err := u.Locator.LastPosition(ctx, providerID)
	if err != nil {
		return nil, 0, 0, err
	}
	ordered := append([]dtos.PositionInput{}, batch...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].AgeS > ordered[j].AgeS })

	now := u.now()
	var last *entities.ProviderPosition
	accepted, rejected := 0, 0
	for _, point := range ordered {
		position := entities.ProviderPosition{
			ProviderID: providerID, Point: entities.GeoPoint{Lat: point.Lat, Lng: point.Lng}, Heading: point.Heading,
			AccuracyM: point.AccuracyM, RecordedAt: now.Add(-time.Duration(point.AgeS) * time.Second),
		}
		reference := previous
		if last != nil {
			reference = last
		}
		if !u.settings.Limits.Accepts(position, reference) {
			rejected++
			continue
		}
		accepted++
		current := position
		last = &current
	}
	return last, accepted, rejected, nil
}

type positionPayload struct {
	Class     string   `json:"class"`
	RequestID string   `json:"request_id"`
	Lat       float64  `json:"lat"`
	Lng       float64  `json:"lng"`
	Heading   *float64 `json:"heading,omitempty"`
	ETASecond *int     `json:"eta_s,omitempty"`
}

func (u *Usecase) shareWithCustomer(ctx context.Context, providerID uuid.UUID, trip entities.ActiveTrip, position entities.ProviderPosition) {
	payload := positionPayload{Class: "ephemeral", RequestID: trip.RequestID.String(), Lat: position.Point.Lat, Lng: position.Point.Lng, Heading: position.Heading}
	if trip.Target != nil {
		roadMeters := entities.DistanceMeters(position.Point, *trip.Target) * u.settings.RoadFactor
		eta := int(roadMeters / (u.settings.AverageSpeedKmh * metersPerKm / secondsPerHour))
		payload.ETASecond = &eta
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	log := logger.FromContext(ctx)
	if err := u.Feed.PublishPosition(ctx, trip.CustomerID.String(), trip.RequestID.String(), encoded); err != nil {
		log.Warn("position not shared", zap.Error(err))
	}
	claimed, err := u.Locator.ClaimSample(ctx, providerID, sampleEvery)
	if err != nil || !claimed {
		return
	}
	if err := u.Providers.SaveLocationSample(ctx, providerID, trip.RequestID, position.Point, position.RecordedAt); err != nil {
		log.Warn("position sample not stored", zap.Error(err))
	}
}

func cadenceView(phase entities.ProviderPhase, cadence entities.Cadence, accepted, rejected int) *dtos.CadenceView {
	return &dtos.CadenceView{
		Online: phase != entities.PhaseOffline, Phase: string(phase), SendGPS: cadence.SendsGPS, IntervalS: int(cadence.Interval.Seconds()),
		DistanceM: cadence.Distance, Accepted: accepted, Rejected: rejected,
	}
}

func (u *Usecase) SetCoverage(ctx context.Context, providerID uuid.UUID, in dtos.CoverageInput) (*dtos.CoverageView, error) {
	base := entities.GeoPoint{Lat: in.Lat, Lng: in.Lng}
	if err := base.Validate("base"); err != nil {
		return nil, err
	}
	profile, err := u.Providers.FindProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != entities.ProviderApproved {
		return nil, ErrProviderNotApproved
	}
	category, err := u.Providers.CategoryOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	policies, err := u.Config.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	radiusKm := in.RadiusKm
	cellsByCategory := map[uuid.UUID][]int64{}
	totalCells := 0
	for _, policy := range policies {
		if category == nil || policy.ZoneID != nil || policy.CandidateSource != entities.CandidateCoverage || policy.CategoryID != category.CategoryID {
			continue
		}
		if radiusKm == 0 {
			radiusKm = policy.RadiiKm[0]
		}
		cells, err := u.Geo.Covering(base, radiusKm, policy.H3Resolution)
		if err != nil {
			return nil, err
		}
		cellsByCategory[category.CategoryID] = cells
		totalCells = len(cells)
	}
	now := u.now()
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.ReplaceCoverage(ctx, providerID, base, radiusKm, cellsByCategory); err != nil {
			return err
		}
		return u.publish(ctx, events.CoverageChangedV1{ProviderID: providerID, Lat: in.Lat, Lng: in.Lng, RadiusKm: radiusKm, Cells: totalCells, OccurredAt: now})
	})
	if err != nil {
		return nil, err
	}
	return &dtos.CoverageView{Lat: in.Lat, Lng: in.Lng, RadiusKm: radiusKm, Categories: len(cellsByCategory), Cells: totalCells}, nil
}

func (u *Usecase) Nearby(ctx context.Context, point entities.GeoPoint, category string) (*dtos.NearbyView, error) {
	if err := point.Validate("point"); err != nil {
		return nil, err
	}
	zone, err := u.zones.Resolve(ctx, point)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, ErrOutsideServiceArea
	}
	categoryID, err := u.Config.FindCategoryIDBySlug(ctx, category)
	if err != nil {
		return nil, err
	}
	if categoryID == nil {
		return nil, ErrServiceNotOffered
	}
	policy, err := u.Config.ResolvePolicy(ctx, *categoryID, zone.ID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, ErrServiceNotOffered
	}
	counts, err := u.nearbyCounts(ctx, point, category, *categoryID, policy)
	if err != nil {
		return nil, err
	}
	view := &dtos.NearbyView{Cells: []dtos.NearbyCell{}, Available: len(counts) > 0}
	for cell, count := range counts {
		if count < int64(u.settings.NearbyMinCount) {
			continue
		}
		center, err := u.Geo.Center(cell)
		if err != nil {
			return nil, err
		}
		view.Cells = append(view.Cells, dtos.NearbyCell{Cell: u.Geo.Format(cell), Lat: center.Lat, Lng: center.Lng, Count: count})
	}
	sort.Slice(view.Cells, func(i, j int) bool { return view.Cells[i].Cell < view.Cells[j].Cell })
	return view, nil
}

func (u *Usecase) nearbyCounts(ctx context.Context, point entities.GeoPoint, category string, categoryID uuid.UUID, policy *entities.DispatchPolicy) (map[int64]int64, error) {
	if policy.CandidateSource == entities.CandidateLive {
		cells, err := u.Geo.Covering(point, policy.RadiiKm[0], policy.H3Resolution)
		if err != nil {
			return nil, err
		}
		return u.Locator.CountInCells(ctx, category, policy.H3Resolution, cells)
	}
	cell, err := u.Geo.Cell(point, policy.H3Resolution)
	if err != nil {
		return nil, err
	}
	providers, err := u.Providers.CoveringCell(ctx, categoryID, cell)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, nil
	}
	return map[int64]int64{cell: int64(len(providers))}, nil
}

func (u *Usecase) RegisterDevice(ctx context.Context, userID uuid.UUID, in dtos.DeviceInput) error {
	return u.Devices.Register(ctx, userID, in.Token, in.Platform)
}

func (u *Usecase) UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error {
	return u.Devices.Unregister(ctx, userID, token)
}
