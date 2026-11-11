package dispatch

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	demandRefreshEvery   = 50 * time.Second
	demandHistoryWindow  = 24 * time.Hour
	maxDemandHistoryRows = 2000
	auditEntityDemand    = "demand_pricing"
	demandSaveAction     = "demand_pricing.save"
)

var ErrDemandNotLive = errors.New("demand pricing only applies to categories dispatched to online providers")

func (u *Usecase) demandEnabled() bool {
	return u.Demand != nil && u.Signals != nil
}

func (u *Usecase) demandFor(ctx context.Context, customerID uuid.UUID, zone *entities.ServiceZone, service *entities.DispatchService, pickup entities.GeoPoint, scheduled bool, now time.Time) entities.DemandQuote {
	if !u.demandEnabled() || scheduled {
		return entities.NeutralDemand()
	}
	log := logger.FromContext(ctx)
	pricing, err := u.Demand.FindDemandPricing(ctx, zone.ID, service.CategoryID)
	if err != nil {
		log.Warn("demand pricing unavailable", zap.Error(err))
		return entities.NeutralDemand()
	}
	if pricing == nil || !pricing.Measures() {
		return entities.NeutralDemand()
	}
	area, err := u.Geo.Cell(pickup, u.settings.DemandAreaResolution)
	if err != nil {
		log.Warn("demand area unavailable", zap.Error(err))
		return entities.NeutralDemand()
	}
	key := repository.DemandKey{ZoneID: zone.ID, Category: service.CategorySlug}
	if err := u.Signals.RecordQuote(ctx, key, area, customerID, now); err != nil {
		log.Warn("demand quote not counted", zap.Error(err))
	}
	if !pricing.Charges() {
		return entities.NeutralDemand()
	}
	factor, err := u.Signals.Factor(ctx, key, area)
	if err != nil {
		log.Warn("demand factor unavailable", zap.Error(err))
		return entities.NeutralDemand()
	}
	return entities.DemandQuote{FactorBps: pricing.Capped(factor), Commissions: pricing.CommissionOnSurge}
}

func (u *Usecase) RefreshDemand(ctx context.Context) error {
	if !u.demandEnabled() {
		return nil
	}
	claimed, err := u.Signals.ClaimRefresh(ctx, demandRefreshEvery)
	if err != nil || !claimed {
		return err
	}
	configs, err := u.Demand.ListDemandPricing(ctx)
	if err != nil {
		return err
	}
	indexes, err := u.Config.ListLiveIndexes(ctx)
	if err != nil {
		return err
	}
	now := u.now()
	log := logger.FromContext(ctx)
	snapshots := make([]entities.DemandSnapshot, 0)
	for _, pricing := range configs {
		index, live := u.demandIndex(indexes, pricing.CategoryID)
		if !live {
			continue
		}
		key := repository.DemandKey{ZoneID: pricing.ZoneID, Category: index.Slug}
		if !pricing.Measures() {
			if err := u.Signals.Publish(ctx, key, nil, nil, u.settings.DemandFactorTTL); err != nil {
				log.Warn("demand not cleared", zap.String("zone_id", pricing.ZoneID.String()), zap.Error(err))
			}
			continue
		}
		areas, err := u.refreshDemandOf(ctx, pricing, index, key, now)
		if err != nil {
			log.Warn("demand not refreshed", zap.String("zone_id", pricing.ZoneID.String()), zap.String("category", index.Slug), zap.Error(err))
			continue
		}
		for _, area := range areas {
			snapshots = append(snapshots, entities.DemandSnapshot{ZoneID: pricing.ZoneID, CategoryID: pricing.CategoryID, DemandArea: area})
		}
	}
	if err := u.Demand.SaveSnapshots(ctx, snapshots); err != nil {
		return err
	}
	if u.settings.DemandHistoryRetention > 0 {
		if _, err := u.Demand.PurgeSnapshots(ctx, now.Add(-u.settings.DemandHistoryRetention)); err != nil {
			return err
		}
	}
	return nil
}

func (u *Usecase) refreshDemandOf(ctx context.Context, pricing *entities.DemandPricing, index entities.LiveIndex, key repository.DemandKey, now time.Time) ([]entities.DemandArea, error) {
	demand, err := u.pendingDemand(ctx, pricing, key, now)
	if err != nil {
		return nil, err
	}
	previous, err := u.Signals.Ratios(ctx, key)
	if err != nil {
		return nil, err
	}
	areas := make(map[int64]struct{}, len(demand)+len(previous))
	for area := range demand {
		areas[area] = struct{}{}
	}
	for area := range previous {
		areas[area] = struct{}{}
	}
	ratios, factors := map[int64]int{}, map[int64]int{}
	out := make([]entities.DemandArea, 0, len(areas))
	for area := range areas {
		free, err := u.freeProvidersIn(ctx, index, area)
		if err != nil {
			return nil, err
		}
		current := entities.DemandRatioBps(demand[area], free)
		last, seen := previous[area]
		if !seen {
			last = entities.NeutralDemandFactorBps
		}
		smoothed := entities.SmoothDemandRatio(last, current)
		if entities.DemandSettled(smoothed, current) {
			continue
		}
		ratios[area] = smoothed
		factor := pricing.FactorFor(smoothed)
		if factor > entities.NeutralDemandFactorBps {
			factors[area] = factor
		}
		out = append(out, entities.DemandArea{Area: area, Pending: demand[area], Free: free, RatioBps: smoothed, FactorBps: factor, RecordedAt: now})
	}
	if err := u.Signals.Publish(ctx, key, ratios, factors, u.settings.DemandFactorTTL); err != nil {
		return nil, err
	}
	return out, nil
}

func (u *Usecase) pendingDemand(ctx context.Context, pricing *entities.DemandPricing, key repository.DemandKey, now time.Time) (map[int64]int, error) {
	pending, err := u.Demand.PendingCustomers(ctx, pricing.ZoneID, pricing.CategoryID)
	if err != nil {
		return nil, err
	}
	customers := map[int64]map[uuid.UUID]struct{}{}
	add := func(area int64, ids []uuid.UUID) {
		if customers[area] == nil {
			customers[area] = map[uuid.UUID]struct{}{}
		}
		for _, id := range ids {
			customers[area][id] = struct{}{}
		}
	}
	for cell, ids := range pending {
		area, err := u.Geo.Parent(cell, u.settings.DemandAreaResolution)
		if err != nil {
			return nil, err
		}
		add(area, ids)
	}
	quotes, err := u.Signals.RecentQuotes(ctx, key, now.Add(-u.settings.DemandQuoteWindow), now)
	if err != nil {
		return nil, err
	}
	for area, ids := range quotes {
		add(area, ids)
	}
	demand := make(map[int64]int, len(customers))
	for area, ids := range customers {
		demand[area] = len(ids)
	}
	return demand, nil
}

func (u *Usecase) freeProvidersIn(ctx context.Context, index entities.LiveIndex, area int64) (int, error) {
	resolution := u.settings.DemandAreaResolution
	cells := []int64{area}
	switch {
	case index.Resolution > resolution:
		children, err := u.Geo.Children(area, index.Resolution)
		if err != nil {
			return 0, err
		}
		cells = children
	case index.Resolution < resolution:
		parent, err := u.Geo.Parent(area, index.Resolution)
		if err != nil {
			return 0, err
		}
		cells = []int64{parent}
	}
	return u.Signals.FreeProviders(ctx, index.Slug, index.Resolution, cells)
}

func (u *Usecase) demandIndex(indexes []entities.LiveIndex, categoryID uuid.UUID) (entities.LiveIndex, bool) {
	var chosen entities.LiveIndex
	found := false
	for _, index := range indexes {
		if index.CategoryID != categoryID {
			continue
		}
		if !found || betterDemandIndex(index.Resolution, chosen.Resolution, u.settings.DemandAreaResolution) {
			chosen, found = index, true
		}
	}
	return chosen, found
}

func betterDemandIndex(candidate, current, area int) bool {
	if (candidate >= area) != (current >= area) {
		return candidate >= area
	}
	if candidate >= area {
		return candidate < current
	}
	return candidate > current
}

func (u *Usecase) ListDemandPricing(ctx context.Context) (*dtos.DemandPricingList, error) {
	if !u.demandEnabled() {
		return &dtos.DemandPricingList{Items: []dtos.DemandPricingView{}}, nil
	}
	configs, err := u.Demand.ListDemandPricing(ctx)
	if err != nil {
		return nil, err
	}
	indexes, err := u.Config.ListLiveIndexes(ctx)
	if err != nil {
		return nil, err
	}
	list := &dtos.DemandPricingList{Items: make([]dtos.DemandPricingView, 0, len(configs))}
	for _, pricing := range configs {
		index, _ := u.demandIndex(indexes, pricing.CategoryID)
		list.Items = append(list.Items, toDemandPricingView(pricing, index.Slug))
	}
	return list, nil
}

func (u *Usecase) SaveDemandPricing(ctx context.Context, actor entities.Actor, in dtos.DemandPricingInput) (*dtos.DemandPricingView, error) {
	if !u.demandEnabled() {
		return nil, ErrDemandNotLive
	}
	category := strings.TrimSpace(in.Category)
	categoryID, err := u.Config.FindCategoryIDBySlug(ctx, category)
	if err != nil {
		return nil, err
	}
	if categoryID == nil {
		return nil, ErrServiceNotOffered
	}
	indexes, err := u.Config.ListLiveIndexes(ctx)
	if err != nil {
		return nil, err
	}
	if _, live := u.demandIndex(indexes, *categoryID); !live {
		return nil, ErrDemandNotLive
	}
	zone, err := u.Config.FindZone(ctx, in.ZoneID)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, ErrZoneNotFound
	}
	current, err := u.Demand.FindDemandPricing(ctx, zone.ID, *categoryID)
	if err != nil {
		return nil, err
	}
	var before any
	var id uuid.UUID
	if current != nil {
		id, before = current.ID, toDemandPricingView(current, category)
	} else if id, err = newUUID(); err != nil {
		return nil, err
	}
	pricing := &entities.DemandPricing{
		ID: id, ZoneID: zone.ID, CategoryID: *categoryID, Mode: entities.DemandMode(in.Mode), MaxFactorBps: in.MaxFactorBps, CommissionOnSurge: in.CommissionOnSurge,
		Tiers: make([]entities.DemandTier, 0, len(in.Tiers)), UpdatedBy: actor.ID, UpdatedAt: u.now(),
	}
	for _, tier := range in.Tiers {
		pricing.Tiers = append(pricing.Tiers, entities.DemandTier{AboveRatioBps: tier.AboveRatioBps, FactorBps: tier.FactorBps})
	}
	sort.SliceStable(pricing.Tiers, func(i, j int) bool { return pricing.Tiers[i].AboveRatioBps < pricing.Tiers[j].AboveRatioBps })
	if err := pricing.Validate(); err != nil {
		return nil, err
	}
	view := toDemandPricingView(pricing, category)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Demand.SaveDemandPricing(ctx, pricing); err != nil {
			return err
		}
		return u.audit(ctx, actor, demandSaveAction, auditEntityDemand, id, before, view)
	})
	if err != nil {
		return nil, err
	}
	if !pricing.Measures() {
		if err := u.Signals.Publish(ctx, repository.DemandKey{ZoneID: zone.ID, Category: category}, nil, nil, u.settings.DemandFactorTTL); err != nil {
			logger.FromContext(ctx).Warn("demand not cleared", zap.Error(err))
		}
	}
	return &view, nil
}

func (u *Usecase) DemandAreas(ctx context.Context, zoneID uuid.UUID, category string) (*dtos.DemandAreaList, error) {
	list := &dtos.DemandAreaList{Resolution: u.settings.DemandAreaResolution, Items: []dtos.DemandAreaView{}}
	if !u.demandEnabled() {
		return list, nil
	}
	key := repository.DemandKey{ZoneID: zoneID, Category: category}
	ratios, err := u.Signals.Ratios(ctx, key)
	if err != nil {
		return nil, err
	}
	factors, err := u.Signals.Factors(ctx, key)
	if err != nil {
		return nil, err
	}
	for area, ratio := range ratios {
		factor, ok := factors[area]
		if !ok {
			factor = entities.NeutralDemandFactorBps
		}
		view, err := u.demandAreaView(area, ratio, factor)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, view)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].FactorBps > list.Items[j].FactorBps })
	return list, nil
}

func (u *Usecase) DemandHistory(ctx context.Context, zoneID uuid.UUID, category string, since *time.Time) (*dtos.DemandHistory, error) {
	history := &dtos.DemandHistory{Resolution: u.settings.DemandAreaResolution, Items: []dtos.DemandSnapshotView{}}
	if !u.demandEnabled() {
		return history, nil
	}
	categoryID, err := u.Config.FindCategoryIDBySlug(ctx, category)
	if err != nil {
		return nil, err
	}
	if categoryID == nil {
		return nil, ErrServiceNotOffered
	}
	from := u.now().Add(-demandHistoryWindow)
	if since != nil {
		from = *since
	}
	snapshots, err := u.Demand.ListSnapshots(ctx, zoneID, *categoryID, from, maxDemandHistoryRows)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		snapshot := &snapshots[i]
		area, err := u.demandAreaView(snapshot.Area, snapshot.RatioBps, snapshot.FactorBps)
		if err != nil {
			return nil, err
		}
		history.Items = append(history.Items, dtos.DemandSnapshotView{DemandAreaView: area, Pending: snapshot.Pending, Free: snapshot.Free, RecordedAt: snapshot.RecordedAt})
	}
	return history, nil
}

func (u *Usecase) demandAreaView(area int64, ratio, factor int) (dtos.DemandAreaView, error) {
	center, err := u.Geo.Center(area)
	if err != nil {
		return dtos.DemandAreaView{}, err
	}
	return dtos.DemandAreaView{Cell: u.Geo.Format(area), Lat: center.Lat, Lng: center.Lng, RatioBps: ratio, FactorBps: factor}, nil
}

func toDemandPricingView(p *entities.DemandPricing, category string) dtos.DemandPricingView {
	tiers := make([]dtos.DemandTierInput, 0, len(p.Tiers))
	for _, tier := range p.Tiers {
		tiers = append(tiers, dtos.DemandTierInput{AboveRatioBps: tier.AboveRatioBps, FactorBps: tier.FactorBps})
	}
	return dtos.DemandPricingView{ID: p.ID, CategoryID: p.CategoryID, UpdatedAt: p.UpdatedAt, DemandPricingInput: dtos.DemandPricingInput{
		ZoneID: p.ZoneID, Category: category, Mode: string(p.Mode), Tiers: tiers, MaxFactorBps: p.MaxFactorBps, CommissionOnSurge: p.CommissionOnSurge,
	}}
}
