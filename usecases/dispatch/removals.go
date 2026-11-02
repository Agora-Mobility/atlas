package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	removalsSlug    = "removals"
	dayLayout       = "2006-01-02"
	maxRemovalDays  = 62
	dm3PerM3        = 1000.0
	removalDetailsK = "removal"
)

var ErrBadDay = errors.New("the date is not valid")

func (u *Usecase) settingCents(ctx context.Context, key string) (int64, error) {
	value, err := u.settingNumber(ctx, key)
	return int64(value), err
}

func (u *Usecase) removalQuote(ctx context.Context, in *dtos.RemovalInput) (*entities.RemovalQuote, error) {
	if in == nil {
		in = &dtos.RemovalInput{}
	}
	maxHelpers, err := u.settingNumber(ctx, entities.SettingMaxHelpers)
	if err != nil {
		return nil, err
	}
	if in.Helpers > maxHelpers {
		return nil, entities.ErrRemovalHelpers
	}
	helperCents, err := u.settingCents(ctx, entities.SettingHelpersPriceCents)
	if err != nil {
		return nil, err
	}
	choices := make([]entities.RemovalItemChoice, 0, len(in.Items))
	for _, chosen := range in.Items {
		item, err := u.Reference.Item(ctx, chosen.ItemID)
		if err != nil {
			return nil, err
		}
		choices = append(choices, entities.RemovalItemChoice{Item: item, Qty: chosen.Qty})
	}
	lines, volume, err := entities.RemovalVolume(choices)
	if err != nil {
		return nil, err
	}
	available, err := u.Reference.Items(ctx, entities.CatalogRemovalExtra, removalsSlug, false)
	if err != nil {
		return nil, err
	}
	extras := make([]entities.RemovalLine, 0, len(in.Extras))
	extrasCents := int64(0)
	for _, code := range in.Extras {
		index := slices.IndexFunc(available, func(item entities.CatalogItem) bool { return item.Code == code })
		if index < 0 {
			return nil, entities.ErrRemovalExtraUnknown
		}
		cents := available[index].PriceAt(u.now())
		extras = append(extras, entities.RemovalLine{Code: code, Label: available[index].Label, AmountCents: cents})
		extrasCents += cents
	}
	return &entities.RemovalQuote{Items: lines, VolumeDm3: volume, Helpers: in.Helpers, HelpersCents: int64(in.Helpers) * helperCents, Extras: extras, ExtrasCents: extrasCents}, nil
}

func (u *Usecase) removalEstimates(ctx context.Context, list *dtos.FareEstimateList, vehicles []entities.CatalogItem, service *entities.DispatchService, tariff *entities.ServiceTariff, customerID uuid.UUID, pickup entities.GeoPoint, destination *entities.GeoPoint, stops []entities.GeoPoint, in dtos.FareEstimateInput, route services.Route, now, expiresAt time.Time) error {
	base, err := u.removalQuote(ctx, in.Removal)
	if err != nil {
		return err
	}
	volume := base.VolumeDm3
	insurance, err := u.settingCents(ctx, entities.SettingInsuranceCents)
	if err != nil {
		return err
	}
	recommended := entities.RecommendedVehicle(vehicles, volume)
	if recommended == nil {
		return entities.ErrRemovalTooLarge
	}
	for i := range vehicles {
		vehicle := &vehicles[i]
		if vehicle.AttrInt(entities.AttrMaxVolume) < volume {
			continue
		}
		quote := entities.RemovalQuote{
			VehicleCode: vehicle.Code, Vehicle: vehicle.Label, VehicleCents: vehicle.PriceAt(now), VolumeDm3: volume, VolumeM3: float64(volume) / dm3PerM3, Items: base.Items, Helpers: base.Helpers, HelpersCents: base.HelpersCents,
			Extras: base.Extras, ExtrasCents: base.ExtrasCents, DurationMinutes: entities.RemovalDuration(vehicle, volume), InsuranceCents: insurance, Recommended: vehicle.ID == recommended.ID,
		}
		details, err := json.Marshal(quote)
		if err != nil {
			return err
		}
		total := quote.TotalCents()
		id, err := newUUID()
		if err != nil {
			return err
		}
		typeID := vehicle.ID
		estimate := &entities.FareEstimate{
			ID: id, CustomerID: customerID, TariffID: tariff.ID, ServiceID: service.ID, Origin: pickup, Destination: destination, Stops: stops, ScheduledFor: in.ScheduledFor, VehicleTypeID: &typeID, Details: details,
			DistanceMeters: route.DistanceMeters, DurationSeconds: int(route.Duration.Seconds()), RouteSource: string(route.Source), TotalCents: total, CommissionCents: tariff.CommissionFor(total), Currency: tariff.Currency, ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := u.Pricing.CreateEstimate(ctx, estimate); err != nil {
			return err
		}
		list.Items = append(list.Items, dtos.FareEstimateView{
			FareEstimateID: id, ServiceID: service.ID, ServiceName: service.Name, ServiceSlug: service.Slug, TotalCents: total, Currency: tariff.Currency, DistanceM: route.DistanceMeters, DurationS: int(route.Duration.Seconds()),
			RouteSource: string(route.Source), ExpiresInS: int(u.settings.FareEstimateTTL.Seconds()), VehicleTypeID: &typeID, VehicleType: vehicle.Label, Removal: &quote,
		})
	}
	return nil
}

func isRemovalEstimate(fare entities.RequestFare) bool {
	var quote entities.RemovalQuote
	return len(fare.EstimateDetails) > 0 && json.Unmarshal(fare.EstimateDetails, &quote) == nil && quote.VehicleCode != ""
}

func (u *Usecase) prepareRemoval(ctx context.Context, request *entities.ServiceRequest, zone *entities.ServiceZone, in dtos.CreateRequestInput, now time.Time) error {
	if !isRemovalEstimate(request.Fare) {
		return nil
	}
	if in.Removal == nil || request.ScheduledFor == nil {
		return entities.ErrRemovalDetails
	}
	window, err := u.Reference.Item(ctx, in.Removal.WindowID)
	if err != nil {
		return err
	}
	local := request.ScheduledFor.In(zone.Location())
	if !entities.WindowStart(window, local) {
		return entities.ErrRemovalWindow
	}
	maxDays, err := u.settingNumber(ctx, entities.SettingRemovalMaxDays)
	if err != nil {
		return err
	}
	if request.ScheduledFor.After(now.AddDate(0, 0, maxDays)) {
		return entities.ErrRemovalTooFar
	}
	properties, err := u.Reference.Items(ctx, entities.CatalogRemovalProperty, removalsSlug, false)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(properties, func(item entities.CatalogItem) bool { return item.Code == in.Removal.PropertyCode })
	if index < 0 {
		return entities.ErrRemovalDetails
	}
	places := entities.RemovalPlaces{PickupFloor: in.Removal.PickupFloor, PickupElevator: in.Removal.PickupElevator, DestinationFloor: in.Removal.DestinationFloor, DestinationElevator: in.Removal.DestinationElevator, ContactName: in.Removal.ContactName, Notes: in.Removal.Notes}
	if err := places.Normalize(); err != nil {
		return err
	}
	var quote map[string]any
	if err := json.Unmarshal(request.Fare.EstimateDetails, &quote); err != nil {
		return err
	}
	var fields map[string]any
	if err := json.Unmarshal(request.Details, &fields); err != nil || fields == nil {
		fields = map[string]any{}
	}
	quote["window"], quote["property"], quote["places"] = window.Label, properties[index].Label, places
	fields[removalDetailsK] = quote
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	windowID := window.ID
	request.Details, request.RemovalDay, request.RemovalWindowID = encoded, &day, &windowID
	request.Prepay = request.PaymentMethod == entities.PaymentReference
	return nil
}

func (u *Usecase) holdRemovalDay(ctx context.Context, request *entities.ServiceRequest) error {
	if request.RemovalDay == nil || request.VehicleTypeID == nil {
		return nil
	}
	if err := u.Removals.LockVehicle(ctx, *request.VehicleTypeID); err != nil {
		return err
	}
	used, err := u.Removals.CountOfDay(ctx, *request.VehicleTypeID, *request.RemovalDay)
	if err != nil {
		return err
	}
	vehicle, err := u.Reference.Item(ctx, *request.VehicleTypeID)
	if err != nil {
		return err
	}
	if vehicle == nil || used >= vehicle.AttrInt(entities.AttrDailyCapacity) {
		return entities.ErrRemovalDayFull
	}
	return nil
}

func toRemovalOption(item *entities.CatalogItem) dtos.RemovalOption {
	return dtos.RemovalOption{ID: item.ID, Code: item.Code, Label: item.Label, Group: item.Description, AmountCents: item.AmountCents, PromoCents: item.PromoCents, PromoUntil: item.PromoUntil, VolumeDm3: item.AttrInt(entities.AttrVolumeDm3), MaxVolumeM3: float64(item.AttrInt(entities.AttrMaxVolume)) / dm3PerM3}
}

func (u *Usecase) removalList(ctx context.Context, kind entities.CatalogKind) ([]dtos.RemovalOption, error) {
	items, err := u.Reference.Items(ctx, kind, removalsSlug, false)
	if err != nil {
		return nil, err
	}
	out := make([]dtos.RemovalOption, 0, len(items))
	for i := range items {
		out = append(out, toRemovalOption(&items[i]))
	}
	return out, nil
}

func (u *Usecase) RemovalOptions(ctx context.Context) (*dtos.RemovalOptions, error) {
	view := &dtos.RemovalOptions{}
	var err error
	if view.Items, err = u.removalList(ctx, entities.CatalogRemovalItem); err != nil {
		return nil, err
	}
	if view.Vehicles, err = u.removalList(ctx, entities.CatalogRemovalVehicle); err != nil {
		return nil, err
	}
	if view.Extras, err = u.removalList(ctx, entities.CatalogRemovalExtra); err != nil {
		return nil, err
	}
	if view.Windows, err = u.removalList(ctx, entities.CatalogRemovalWindow); err != nil {
		return nil, err
	}
	if view.Properties, err = u.removalList(ctx, entities.CatalogRemovalProperty); err != nil {
		return nil, err
	}
	if view.HelperCents, err = u.settingCents(ctx, entities.SettingHelpersPriceCents); err != nil {
		return nil, err
	}
	if view.MaxHelpers, err = u.settingNumber(ctx, entities.SettingMaxHelpers); err != nil {
		return nil, err
	}
	if view.InsuranceCents, err = u.settingCents(ctx, entities.SettingInsuranceCents); err != nil {
		return nil, err
	}
	view.Currency = u.settings.Currency
	return view, nil
}

func (u *Usecase) RemovalCalendar(ctx context.Context, vehicleID uuid.UUID, from string, days int) (*dtos.RemovalCalendar, error) {
	first, err := time.Parse(dayLayout, from)
	if err != nil || days < 1 || days > maxRemovalDays {
		return nil, ErrBadDay
	}
	vehicle, err := u.Reference.Item(ctx, vehicleID)
	if err != nil {
		return nil, err
	}
	if vehicle == nil || vehicle.Kind != entities.CatalogRemovalVehicle || !vehicle.IsActive {
		return nil, entities.ErrRemovalVehicle
	}
	windows, err := u.removalList(ctx, entities.CatalogRemovalWindow)
	if err != nil {
		return nil, err
	}
	view := &dtos.RemovalCalendar{Days: make([]dtos.RemovalDayView, 0, days)}
	for offset := range days {
		day := first.AddDate(0, 0, offset)
		used, err := u.Removals.CountOfDay(ctx, vehicleID, day)
		if err != nil {
			return nil, err
		}
		item := dtos.RemovalDayView{Date: day.Format(dayLayout), Status: string(entities.DayFree), Windows: windows}
		if used >= vehicle.AttrInt(entities.AttrDailyCapacity) {
			item.Status, item.Windows = string(entities.DayFull), nil
		}
		view.Days = append(view.Days, item)
	}
	return view, nil
}

func (u *Usecase) teamOf(ctx context.Context, requestID uuid.UUID) ([]dtos.TeamMemberView, error) {
	members, err := u.Removals.Team(ctx, requestID)
	if err != nil {
		return nil, err
	}
	out := make([]dtos.TeamMemberView, 0, len(members))
	for i := range members {
		out = append(out, dtos.TeamMemberView{Name: members[i].Name, Role: members[i].Role, Verified: members[i].Verified})
	}
	return out, nil
}

func (u *Usecase) replaceTeam(ctx context.Context, request *entities.ServiceRequest, in dtos.TeamInput, canVerify bool) (*dtos.TeamView, error) {
	if request.RemovalDay == nil || request.Status.IsTerminal() {
		return nil, entities.ErrTeamNotAllowed
	}
	members := make([]entities.TeamMember, 0, len(in.Members))
	for _, member := range in.Members {
		members = append(members, entities.TeamMember{Name: member.Name, Role: member.Role, Verified: canVerify && member.Verified})
	}
	team, err := entities.NewTeam(request.ID, members, u.now())
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error { return u.Removals.ReplaceTeam(ctx, request.ID, team) })
	if err != nil {
		return nil, err
	}
	items, err := u.teamOf(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	return &dtos.TeamView{Items: items}, nil
}

// SetTeamAsProvider cannot mark anyone as verified: that is up to the partner,
// who answers for the team.
func (u *Usecase) SetTeamAsProvider(ctx context.Context, providerID, requestID uuid.UUID, in dtos.TeamInput) (*dtos.TeamView, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || !request.IsProvider(providerID) {
		return nil, ErrRequestNotFound
	}
	return u.replaceTeam(ctx, request, in, false)
}

func (u *Usecase) SetTeamAsPartner(ctx context.Context, organizationID, requestID uuid.UUID, in dtos.TeamInput) (*dtos.TeamView, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrRequestNotFound
	}
	owner, err := u.Requests.OrganizationOf(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if owner == nil || *owner != organizationID {
		return nil, ErrRequestNotFound
	}
	return u.replaceTeam(ctx, request, in, true)
}

func (u *Usecase) OnPrepaid(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	policy, err := u.policyOf(ctx, request)
	if err != nil {
		return err
	}
	request.ScheduleSearch(policy, now)
	return u.Requests.Update(ctx, request, request.Touch(now))
}

func (u *Usecase) OnPrepayExpired(ctx context.Context, request *entities.ServiceRequest, _ time.Time) error {
	_, err := u.cancel(ctx, request.ID, cancelOrder{actor: entities.SystemRequestActor(), text: "A referência expirou sem pagamento", allowed: func(*entities.ServiceRequest) bool { return true }})
	return err
}

func (u *Usecase) refundPrepaid(ctx context.Context, request *entities.ServiceRequest, reason string) error {
	if !request.Prepay || request.PaymentStatus != entities.RequestPaid || u.Refunds == nil {
		return nil
	}
	return u.Refunds.RefundPrepaid(ctx, request.ID, reason)
}
