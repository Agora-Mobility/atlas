package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	auditEntityZone     = "service_zone"
	auditEntityPolicy   = "dispatch_policy"
	auditEntityTariff   = "service_tariff"
	auditEntityProvider = "provider_profile"
	auditEntityRequest  = "service_request"
	heatResolution      = 8
	defaultHeatWindow   = 24 * time.Hour
)

func entitiesDestinationError() error {
	return customerrors.FieldError("destination", "Este serviço precisa de um destino.")
}

func (u *Usecase) ListZones(ctx context.Context) (*dtos.ZoneList, error) {
	zones, err := u.Config.ListZones(ctx, false)
	if err != nil {
		return nil, err
	}
	list := &dtos.ZoneList{Items: make([]dtos.ZoneView, 0, len(zones))}
	for _, zone := range zones {
		list.Items = append(list.Items, toZoneView(zone))
	}
	return list, nil
}

func (u *Usecase) CreateZone(ctx context.Context, actor entities.Actor, in dtos.ZoneInput) (*dtos.ZoneView, error) {
	zone, err := u.zoneFrom(ctx, uuid.Nil, in)
	if err != nil {
		return nil, err
	}
	zone.ID, err = newUUID()
	if err != nil {
		return nil, err
	}
	view := toZoneView(zone)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Config.CreateZone(ctx, zone); err != nil {
			return err
		}
		return u.audit(ctx, actor, "service_zone.create", auditEntityZone, zone.ID, nil, zoneAudit(view))
	})
	if err != nil {
		return nil, err
	}
	u.zones.Invalidate()
	return &view, nil
}

func (u *Usecase) UpdateZone(ctx context.Context, actor entities.Actor, id uuid.UUID, in dtos.ZoneInput) (*dtos.ZoneView, error) {
	current, err := u.Config.FindZone(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrZoneNotFound
	}
	zone, err := u.zoneFrom(ctx, id, in)
	if err != nil {
		return nil, err
	}
	before, after := toZoneView(current), toZoneView(zone)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Config.UpdateZone(ctx, zone); err != nil {
			return err
		}
		return u.audit(ctx, actor, "service_zone.update", auditEntityZone, id, zoneAudit(before), zoneAudit(after))
	})
	if err != nil {
		return nil, err
	}
	u.zones.Invalidate()
	return &after, nil
}

func (u *Usecase) zoneFrom(ctx context.Context, id uuid.UUID, in dtos.ZoneInput) (*entities.ServiceZone, error) {
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, customerrors.FieldError("timezone", "O fuso horário não é válido.")
	}
	geojson := string(in.Geometry)
	valid, err := u.Config.IsValidGeometry(ctx, geojson)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, customerrors.FieldError("geometry", "A geometria não é um polígono válido.")
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	return &entities.ServiceZone{
		ID: id, CountryCode: strings.ToUpper(in.CountryCode), Name: strings.TrimSpace(in.Name), GeoJSON: geojson, Timezone: in.Timezone,
		Currency: strings.ToUpper(in.Currency), IsActive: active,
	}, nil
}

func toZoneView(zone *entities.ServiceZone) dtos.ZoneView {
	return dtos.ZoneView{
		ID: zone.ID, CountryCode: zone.CountryCode, Name: zone.Name, Timezone: zone.Timezone, Currency: zone.Currency,
		IsActive: zone.IsActive, Geometry: json.RawMessage(zone.GeoJSON),
	}
}

func zoneAudit(view dtos.ZoneView) dtos.ZoneView {
	view.Geometry = nil
	return view
}

func (u *Usecase) ListPolicies(ctx context.Context) (*dtos.PolicyList, error) {
	policies, err := u.Config.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	list := &dtos.PolicyList{Items: make([]dtos.PolicyView, 0, len(policies))}
	for _, policy := range policies {
		list.Items = append(list.Items, toPolicyView(policy))
	}
	return list, nil
}

func (u *Usecase) CreatePolicy(ctx context.Context, actor entities.Actor, in dtos.PolicyInput) (*dtos.PolicyView, error) {
	policy := fromPolicyInput(uuid.Nil, in)
	if err := u.validatePolicy(policy); err != nil {
		return nil, err
	}
	var err error
	if policy.ID, err = newUUID(); err != nil {
		return nil, err
	}
	view := toPolicyView(policy)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Config.CreatePolicy(ctx, policy); err != nil {
			return err
		}
		return u.audit(ctx, actor, "dispatch_policy.create", auditEntityPolicy, policy.ID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (u *Usecase) UpdatePolicy(ctx context.Context, actor entities.Actor, id uuid.UUID, in dtos.PolicyInput) (*dtos.PolicyView, error) {
	current, err := u.Config.FindPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrPolicyNotFound
	}
	policy := fromPolicyInput(id, in)
	policy.CategoryID, policy.ZoneID = current.CategoryID, current.ZoneID
	if err := u.validatePolicy(policy); err != nil {
		return nil, err
	}
	before, after := toPolicyView(current), toPolicyView(policy)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Config.UpdatePolicy(ctx, policy); err != nil {
			return err
		}
		return u.audit(ctx, actor, "dispatch_policy.update", auditEntityPolicy, id, before, after)
	})
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (u *Usecase) validatePolicy(policy *entities.DispatchPolicy) error {
	return policy.Validate(func(radiusKm float64, resolution int) int {
		return u.Geo.CellCountFor(u.Geo.RingsFor(radiusKm, resolution))
	})
}

func fromPolicyInput(id uuid.UUID, in dtos.PolicyInput) *entities.DispatchPolicy {
	tracking := make([]entities.RequestStatus, 0, len(in.TrackingStatuses))
	for _, status := range in.TrackingStatuses {
		tracking = append(tracking, entities.RequestStatus(status))
	}
	fallback := in.FallbackChannels
	if fallback == nil {
		fallback = []string{}
	}
	return &entities.DispatchPolicy{
		ID: id, CategoryID: in.CategoryID, ZoneID: in.ZoneID, CandidateSource: entities.CandidateSource(in.CandidateSource), H3Resolution: in.H3Resolution,
		RadiiKm: in.RadiiKm, OfferMode: entities.OfferMode(in.OfferMode), BatchSize: in.BatchSize, OfferTTLSeconds: in.OfferTTLSeconds, SearchTTLSeconds: in.SearchTTLSeconds,
		AckTimeoutSeconds: in.AckTimeoutSeconds, AckGiveUpSeconds: in.AckGiveUpSeconds, InitialChannels: in.InitialChannels, FallbackChannels: fallback,
		Ranking: entities.Ranking(in.Ranking), TrackingStatuses: tracking, OnExhausted: entities.ExhaustedAction(in.OnExhausted), ReserveProvider: in.ReserveProvider,
		ScheduleMaxDays: in.ScheduleMaxDays, SearchLeadSeconds: in.SearchLeadSeconds, PricingMode: entities.PricingMode(in.PricingMode), AllowBoost: in.AllowBoost,
		RankByETA: in.RankByETA, SMSOnAccept: in.SMSOnAccept,
	}
}

func toPolicyView(p *entities.DispatchPolicy) dtos.PolicyView {
	tracking := make([]string, 0, len(p.TrackingStatuses))
	for _, status := range p.TrackingStatuses {
		tracking = append(tracking, string(status))
	}
	return dtos.PolicyView{ID: p.ID, PolicyInput: dtos.PolicyInput{
		CategoryID: p.CategoryID, ZoneID: p.ZoneID, CandidateSource: string(p.CandidateSource), H3Resolution: p.H3Resolution, RadiiKm: p.RadiiKm, OfferMode: string(p.OfferMode),
		BatchSize: p.BatchSize, OfferTTLSeconds: p.OfferTTLSeconds, SearchTTLSeconds: p.SearchTTLSeconds, AckTimeoutSeconds: p.AckTimeoutSeconds, AckGiveUpSeconds: p.AckGiveUpSeconds,
		InitialChannels: p.InitialChannels, FallbackChannels: p.FallbackChannels, Ranking: string(p.Ranking), TrackingStatuses: tracking, OnExhausted: string(p.OnExhausted),
		ReserveProvider: p.ReserveProvider, ScheduleMaxDays: p.ScheduleMaxDays, SearchLeadSeconds: p.SearchLeadSeconds, PricingMode: string(p.PricingMode), AllowBoost: p.AllowBoost,
		RankByETA: p.RankByETA, SMSOnAccept: p.SMSOnAccept,
	}}
}

func (u *Usecase) ListTariffs(ctx context.Context) (*dtos.TariffList, error) {
	tariffs, err := u.Pricing.ListCurrentTariffs(ctx)
	if err != nil {
		return nil, err
	}
	list := &dtos.TariffList{Items: make([]dtos.TariffView, 0, len(tariffs))}
	for _, tariff := range tariffs {
		list.Items = append(list.Items, toTariffView(tariff))
	}
	return list, nil
}

func (u *Usecase) PublishTariff(ctx context.Context, actor entities.Actor, in dtos.TariffInput) (*dtos.TariffView, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	tariff := &entities.ServiceTariff{
		ID: id, ServiceID: in.ServiceID, ZoneID: in.ZoneID, Currency: strings.ToUpper(in.Currency), BaseFeeCents: in.BaseFeeCents, PerKmFeeCents: in.PerKmFeeCents,
		PerMinuteFeeCents: in.PerMinuteFeeCents, MinimumFeeCents: in.MinimumFeeCents, ZoneFeeCents: in.ZoneFeeCents, NightFeeCents: in.NightFeeCents,
		NightStartHour: in.NightStartHour, NightEndHour: in.NightEndHour, FreeWaitSeconds: in.FreeWaitSeconds, CommissionBps: in.CommissionBps, ValidFrom: u.now(),
	}
	if err := tariff.Validate(); err != nil {
		return nil, err
	}
	service, err := u.Config.FindService(ctx, in.ServiceID)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, ErrServiceNotOffered
	}
	view := toTariffView(tariff)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Pricing.ReplaceTariff(ctx, tariff); err != nil {
			return err
		}
		return u.audit(ctx, actor, "service_tariff.publish", auditEntityTariff, id, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func toTariffView(t *entities.ServiceTariff) dtos.TariffView {
	return dtos.TariffView{ID: t.ID, ValidFrom: t.ValidFrom, TariffInput: dtos.TariffInput{
		ServiceID: t.ServiceID, ZoneID: t.ZoneID, Currency: t.Currency, BaseFeeCents: t.BaseFeeCents, PerKmFeeCents: t.PerKmFeeCents, PerMinuteFeeCents: t.PerMinuteFeeCents,
		MinimumFeeCents: t.MinimumFeeCents, ZoneFeeCents: t.ZoneFeeCents, NightFeeCents: t.NightFeeCents, NightStartHour: t.NightStartHour, NightEndHour: t.NightEndHour,
		FreeWaitSeconds: t.FreeWaitSeconds, CommissionBps: t.CommissionBps,
	}}
}

func (u *Usecase) SaveProvider(ctx context.Context, actor entities.Actor, userID uuid.UUID, role enums.Role, in dtos.ProviderAdminInput) (*dtos.ProviderAdminView, error) {
	if role != enums.RoleProvider {
		return nil, ErrProviderNotEligible
	}
	status := entities.ProviderStatus(in.Status)
	profile := &entities.ProviderProfile{UserID: userID, Status: status, VehiclePlate: strings.TrimSpace(in.VehiclePlate), VehicleDescription: strings.TrimSpace(in.VehicleDescription)}
	view := &dtos.ProviderAdminView{UserID: userID, Status: in.Status, VehiclePlate: profile.VehiclePlate, VehicleDescription: profile.VehicleDescription, ServiceIDs: in.ServiceIDs}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.SaveProfile(ctx, profile, in.ServiceIDs); err != nil {
			return err
		}
		return u.audit(ctx, actor, "provider_profile.save", auditEntityProvider, userID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	if status != entities.ProviderApproved {
		if err := u.Locator.SetOffline(ctx, userID); err != nil {
			return nil, err
		}
	}
	return view, nil
}

func (u *Usecase) OperatorQueue(ctx context.Context, page pagination.Params) (*dtos.RequestList, error) {
	page = page.Normalize()
	requests, err := u.Requests.OperatorQueue(ctx, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	items := make([]dtos.RequestView, 0, len(requests))
	for _, request := range requests {
		view, err := u.customerSummary(ctx, request)
		if err != nil {
			return nil, err
		}
		items = append(items, *view)
	}
	return &dtos.RequestList{Items: items, Total: int64(len(items)), Page: page.Page, Size: page.Size}, nil
}

func (u *Usecase) GetRequestAsOperator(ctx context.Context, requestID uuid.UUID) (*dtos.RequestView, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrRequestNotFound
	}
	return u.customerView(ctx, request)
}

func (u *Usecase) AssignManually(ctx context.Context, actor entities.Actor, requestID, providerID uuid.UUID) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	var policy *entities.DispatchPolicy
	var released []reservation
	operatorActor := entities.RequestActor{Type: entities.RequestActorOperator, ID: actor.ID}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil {
			return ErrRequestNotFound
		}
		eligible, err := u.Providers.FilterEligible(ctx, request.ServiceID, []uuid.UUID{providerID})
		if err != nil {
			return err
		}
		if len(eligible) == 0 {
			return ErrProviderNotEligible
		}
		policy, err = u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		now := u.now()
		from := request.Status
		change, err := request.Transition(entities.RequestAccepted, operatorActor, "manual_dispatch", now)
		if err != nil {
			return err
		}
		request.ProviderID = &providerID
		setReminder(request, now)
		if err := u.recordTransition(ctx, request, change, policy); err != nil {
			return err
		}
		released, err = u.closeOffers(ctx, request, nil, "manual_dispatch", now)
		if err != nil {
			return err
		}
		return u.audit(ctx, actor, "service_request.assign", auditEntityRequest, requestID, map[string]string{"status": string(from)}, map[string]string{"status": string(request.Status), "provider_id": providerID.String()})
	})
	if err != nil {
		return nil, err
	}
	u.afterAssignment(ctx, request, policy, released)
	return u.customerView(ctx, request)
}

func (u *Usecase) CancelByOperator(ctx context.Context, actor entities.Actor, requestID uuid.UUID, reason string) (*dtos.RequestView, error) {
	operatorActor := entities.RequestActor{Type: entities.RequestActorOperator, ID: actor.ID}
	request, err := u.cancel(ctx, requestID, operatorActor, reason, func(*entities.ServiceRequest) bool { return true })
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return u.audit(ctx, actor, "service_request.cancel", auditEntityRequest, requestID, nil, map[string]string{"status": string(request.Status), "reason": reason})
	})
	if err != nil {
		return nil, err
	}
	return u.customerView(ctx, request)
}

func (u *Usecase) RetrySearch(ctx context.Context, actor entities.Actor, requestID uuid.UUID) (*dtos.RequestView, error) {
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestSearching {
			return entities.ErrInvalidTransition
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		now := u.now()
		request.NeedsOperator = false
		request.OperatorReason = ""
		request.BeginSearch(policy, now)
		if err := u.Requests.Update(ctx, request, request.Touch(now)); err != nil {
			return err
		}
		return u.audit(ctx, actor, "service_request.retry", auditEntityRequest, requestID, nil, map[string]string{"status": string(request.Status)})
	})
	if err != nil {
		return nil, err
	}
	if err := u.Advance(ctx, requestID); err != nil && !errors.Is(err, repository.ErrRequestVersionConflict) {
		return nil, err
	}
	return u.GetRequestAsOperator(ctx, requestID)
}

func (u *Usecase) HeatMap(ctx context.Context, zoneID *uuid.UUID) (*dtos.HeatMap, error) {
	demand, err := u.Requests.DemandByCell(ctx, u.now().Add(-defaultHeatWindow), zoneID)
	if err != nil {
		return nil, err
	}
	positions, err := u.Locator.AllPositions(ctx)
	if err != nil {
		return nil, err
	}
	supply := make(map[int64]int64, len(positions))
	for _, position := range positions {
		cell, err := u.Geo.Cell(position.Point, heatResolution)
		if err != nil {
			return nil, err
		}
		supply[cell]++
	}
	all := make(map[int64]struct{}, len(demand)+len(supply))
	for cell := range demand {
		all[cell] = struct{}{}
	}
	if zoneID == nil {
		for cell := range supply {
			all[cell] = struct{}{}
		}
	}
	heat := &dtos.HeatMap{Resolution: heatResolution, Cells: make([]dtos.HeatCell, 0, len(all))}
	for cell := range all {
		center, err := u.Geo.Center(cell)
		if err != nil {
			return nil, err
		}
		heat.Cells = append(heat.Cells, dtos.HeatCell{Cell: u.Geo.Format(cell), Lat: center.Lat, Lng: center.Lng, Supply: supply[cell], Demand: demand[cell]})
	}
	sort.Slice(heat.Cells, func(i, j int) bool { return heat.Cells[i].Cell < heat.Cells[j].Cell })
	return heat, nil
}
