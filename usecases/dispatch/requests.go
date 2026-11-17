package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	analyticsResolution = 8
	maxActiveListed     = 50
)

var serverOnlyDetails = []string{"volume_m3", "fare_cents", "price_cents", "total_cents"}

func (u *Usecase) CreateRequest(ctx context.Context, customerID uuid.UUID, idempotencyKey string, in dtos.CreateRequestInput) (*dtos.RequestView, bool, error) {
	existing, err := u.Requests.FindByIdempotencyKey(ctx, customerID, idempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		view, err := u.customerView(ctx, existing)
		return view, false, err
	}

	service, err := u.Config.FindService(ctx, in.ServiceID)
	if err != nil {
		return nil, false, err
	}
	if service == nil || !service.IsOffered {
		return nil, false, ErrServiceNotOffered
	}
	pickup := in.Pickup.ToPlace()
	if err := pickup.Point.Validate("pickup"); err != nil {
		return nil, false, err
	}
	zone, err := u.zones.Resolve(ctx, pickup.Point)
	if err != nil {
		return nil, false, err
	}
	if zone == nil {
		return nil, false, ErrOutsideServiceArea
	}
	policy, err := u.Config.ResolvePolicy(ctx, service.CategoryID, zone.ID)
	if err != nil {
		return nil, false, err
	}
	if policy == nil {
		return nil, false, ErrServiceNotOffered
	}
	if policy.BookingOnly {
		return nil, false, entities.ErrBookingOnly
	}
	paymentMethod, err := u.checkPayment(ctx, service.CategoryID, in.PaymentMethod)
	if err != nil {
		return nil, false, err
	}
	now := u.now()
	var destination *entities.RequestPlace
	if in.Destination != nil {
		place := in.Destination.ToPlace()
		destination = &place
	}
	fare, err := u.resolveFare(ctx, customerID, service, policy, zone, pickup, destination, in, now)
	if err != nil {
		return nil, false, err
	}

	id, err := newUUID()
	if err != nil {
		return nil, false, err
	}
	stops := make([]entities.RequestPlace, 0, len(in.Stops))
	for _, stop := range in.Stops {
		stops = append(stops, stop.ToPlace())
	}
	passengerName, passengerPhone := "", ""
	if in.Passenger != nil {
		passengerName, passengerPhone = in.Passenger.Name, in.Passenger.Phone
	}
	request, err := entities.NewServiceRequest(entities.NewRequestInput{
		ID: id, CustomerID: customerID, Service: *service, Policy: policy, Zone: zone, Pickup: pickup, Destination: destination, Stops: stops,
		ContactPhone: in.ContactPhone, PassengerName: passengerName, PassengerPhone: passengerPhone, Details: withoutServerFields(in.Details),
		ScheduledFor: in.ScheduledFor, Fare: fare, PaymentMethod: paymentMethod, IdempotencyKey: idempotencyKey, Now: now,
	})
	if err != nil {
		return nil, false, err
	}
	if err := u.prepareTowing(ctx, request, policy, in); err != nil {
		return nil, false, err
	}
	if err := u.prepareRemoval(ctx, request, zone, in, now); err != nil {
		return nil, false, err
	}
	if request.PublicCode, err = u.Requests.NextPublicCode(ctx, service.CategoryID); err != nil {
		return nil, false, err
	}
	if request.PickupH3R8, err = u.Geo.Cell(request.Pickup.Point, analyticsResolution); err != nil {
		return nil, false, err
	}
	if request.PickupH3R9, err = u.Geo.Cell(request.Pickup.Point, entities.HeatmapResolution); err != nil {
		return nil, false, err
	}
	if request.Destination != nil {
		cell, err := u.Geo.Cell(request.Destination.Point, analyticsResolution)
		if err != nil {
			return nil, false, err
		}
		request.DestinationH3 = &cell
	}

	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Requests.LockCustomer(ctx, customerID); err != nil {
			return err
		}
		if err := u.holdRemovalDay(ctx, request); err != nil {
			return err
		}
		if err := u.Requests.Create(ctx, request); err != nil {
			return err
		}
		if err := u.rejectDuplicate(ctx, request); err != nil {
			return err
		}
		if err := u.applyVoucher(ctx, request, service, in.VoucherCode, now); err != nil {
			return err
		}
		if request.Fare.EstimateID != nil {
			fresh, err := u.Pricing.UseEstimate(ctx, *request.Fare.EstimateID, now)
			if err != nil {
				return err
			}
			if !fresh {
				return ErrFareEstimateUsed
			}
		}
		if request.Fare.DemandCents > 0 {
			if err := u.Requests.AddFareAdjustment(ctx, entities.FareAdjustment{RequestID: request.ID, Reason: entities.AdjustmentDemand, AmountCents: request.Fare.DemandCents, CreatedAt: now}); err != nil {
				return err
			}
		}
		created := &entities.RequestStatusChange{To: entities.RequestCreated, Version: request.Version, Actor: entities.RequestActor{Type: entities.RequestActorCustomer, ID: &customerID}, At: now}
		if err := u.announceTransition(ctx, request, created, policy); err != nil {
			return err
		}
		target := entities.RequestSearching
		if request.ScheduledFor != nil {
			target = entities.RequestScheduled
		}
		change, err := request.Transition(target, entities.SystemRequestActor(), "", now)
		if err != nil {
			return err
		}
		if target == entities.RequestScheduled {
			request.ScheduleSearch(policy, now)
			if request.Prepay {
				request.NextActionAt = nil
			}
		} else {
			request.BeginSearch(policy, now)
		}
		return u.recordTransition(ctx, request, change, policy)
	})
	if errors.Is(err, repository.ErrDuplicateIdempotency) {
		winner, err := u.Requests.FindByIdempotencyKey(ctx, customerID, idempotencyKey)
		if err != nil {
			return nil, false, err
		}
		view, err := u.customerView(ctx, winner)
		return view, false, err
	}
	if err != nil {
		return nil, false, err
	}
	businessmetrics.FareQuotes.WithLabelValues(request.CategorySlug, "any", "request").Inc()
	businessmetrics.RequestOutcomes.WithLabelValues(request.CategorySlug, string(request.Status)).Inc()

	if request.Status == entities.RequestSearching {
		if err := u.Advance(ctx, request.ID); err != nil {
			logger.FromContext(ctx).Warn("first search failed, the watcher will retry", zap.String("request_id", request.ID.String()), zap.Error(err))
		}
		fresh, err := u.Requests.Find(ctx, request.ID)
		if err == nil && fresh != nil {
			request = fresh
		}
	}
	view, err := u.customerView(ctx, request)
	return view, true, err
}

func (u *Usecase) rejectDuplicate(ctx context.Context, request *entities.ServiceRequest) error {
	active, err := u.Requests.ActiveOfCustomerService(ctx, request.CustomerID, request.ServiceID)
	if err != nil {
		return err
	}
	var destination *entities.GeoPoint
	if request.Destination != nil {
		destination = &request.Destination.Point
	}
	for _, other := range active {
		if other.ID != request.ID && other.IsSameTrip(request.ServiceID, request.Pickup.Point, destination, request.Stops, request.ScheduledFor) {
			return ErrDuplicateRequest
		}
	}
	return nil
}

func withoutServerFields(details json.RawMessage) json.RawMessage {
	if len(details) == 0 {
		return details
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(details, &fields); err != nil {
		return details
	}
	for _, name := range serverOnlyDetails {
		delete(fields, name)
	}
	cleaned, err := json.Marshal(fields)
	if err != nil {
		return details
	}
	return cleaned
}

func (u *Usecase) checkPayment(ctx context.Context, categoryID uuid.UUID, requested string) (entities.PaymentMethod, error) {
	method := entities.PaymentMethod(requested)
	if method == "" {
		method = entities.PaymentCash
	}
	enabled, err := u.Payments.IsMethodEnabled(ctx, categoryID, method)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", entities.ErrPaymentMethodUnavailable
	}
	return method, nil
}

func (u *Usecase) resolveFare(ctx context.Context, customerID uuid.UUID, service *entities.DispatchService, policy *entities.DispatchPolicy, zone *entities.ServiceZone, pickup entities.RequestPlace, destination *entities.RequestPlace, in dtos.CreateRequestInput, now time.Time) (entities.RequestFare, error) {
	fare := entities.RequestFare{Currency: zone.Currency}
	if in.FareEstimateID == nil {
		switch policy.PricingMode {
		case entities.PricingFixed:
			return fare, ErrFareEstimateRequired
		case entities.PricingCallOut:
			tariff, err := u.Pricing.CurrentTariff(ctx, service.ID, zone.ID)
			if err != nil {
				return fare, err
			}
			if tariff != nil {
				total := tariff.FareFor(0, 0, now.In(zone.Location()))
				commission := tariff.CommissionFor(total)
				fare.Cents, fare.CommissionCents, fare.Currency = &total, &commission, tariff.Currency
			}
		case entities.PricingQuote:
		}
		return fare, nil
	}

	estimate, err := u.Pricing.FindEstimate(ctx, *in.FareEstimateID)
	if err != nil {
		return fare, err
	}
	var destinationPoint *entities.GeoPoint
	if destination != nil {
		destinationPoint = &destination.Point
	}
	stopPoints := make([]entities.GeoPoint, 0, len(in.Stops))
	for _, stop := range in.Stops {
		stopPoints = append(stopPoints, entities.GeoPoint{Lat: stop.Lat, Lng: stop.Lng})
	}
	if estimate == nil || estimate.CustomerID != customerID || estimate.ServiceID != service.ID || !estimate.MatchesRoute(pickup.Point, destinationPoint, stopPoints, in.ScheduledFor) {
		return fare, ErrFareEstimateInvalid
	}
	if estimate.UsedAt != nil {
		return fare, ErrFareEstimateUsed
	}
	if estimate.IsExpired(now) {
		fresh, err := u.CreateEstimates(ctx, customerID, dtos.FareEstimateInput{Category: service.CategorySlug, Pickup: in.Pickup, Destination: in.Destination, Stops: in.Stops, ScheduledFor: in.ScheduledFor})
		if err != nil {
			return fare, err
		}
		return fare, &FareEstimateExpiredError{Fresh: fresh}
	}
	id := estimate.ID
	total, commission := estimate.TotalCents, estimate.CommissionCents
	return entities.RequestFare{EstimateID: &id, Cents: &total, CommissionCents: &commission, Currency: estimate.Currency, VehicleTypeID: estimate.VehicleTypeID, EstimateDetails: estimate.Details, RouteGeometry: estimate.RouteGeometry, DemandCents: estimate.DemandCents}, nil
}

func (u *Usecase) GetRequest(ctx context.Context, userID, requestID uuid.UUID) (*dtos.RequestView, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrRequestNotFound
	}
	switch {
	case request.IsCustomer(userID):
		return u.customerView(ctx, request)
	case request.IsProvider(userID):
		return u.providerView(ctx, request)
	default:
		return nil, ErrRequestNotFound
	}
}

func (u *Usecase) ListRequests(ctx context.Context, customerID uuid.UUID, statuses []string, category string, page pagination.Params) (*dtos.RequestList, error) {
	page = page.Normalize()
	requests, total, err := u.Requests.ListOfCustomer(ctx, repository.RequestFilter{
		CustomerID: customerID, Statuses: statuses, Category: category, Limit: int(page.Limit()), Offset: int(page.Offset()),
	})
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
	return &dtos.RequestList{Items: items, Total: total, Page: page.Page, Size: page.Size}, nil
}

func (u *Usecase) ActiveForCustomer(ctx context.Context, customerID uuid.UUID) (*dtos.ActiveState, error) {
	request, err := u.Requests.ActiveOfCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	state := &dtos.ActiveState{Offers: []dtos.OfferView{}, Requests: []dtos.RequestView{}}
	if request != nil {
		if state.Request, err = u.customerView(ctx, request); err != nil {
			return nil, err
		}
	}
	all, _, err := u.Requests.ListOfCustomer(ctx, repository.RequestFilter{CustomerID: customerID, Statuses: entities.ActiveRequestStatuses(), Limit: maxActiveListed})
	if err != nil {
		return nil, err
	}
	for _, item := range all {
		view, err := u.customerSummary(ctx, item)
		if err != nil {
			return nil, err
		}
		state.Requests = append(state.Requests, *view)
	}
	return state, nil
}

func (u *Usecase) ActiveForProvider(ctx context.Context, providerID uuid.UUID) (*dtos.ActiveState, error) {
	state := &dtos.ActiveState{Offers: []dtos.OfferView{}, Requests: []dtos.RequestView{}}
	request, err := u.Requests.ActiveOfProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if request != nil {
		if state.Request, err = u.providerView(ctx, request); err != nil {
			return nil, err
		}
	}
	now := u.now()
	offers, err := u.Offers.OpenOfProvider(ctx, providerID, now)
	if err != nil {
		return nil, err
	}
	for _, offer := range offers {
		view, err := u.offerView(ctx, offer, now)
		if err != nil {
			return nil, err
		}
		state.Offers = append(state.Offers, *view)
	}
	return state, nil
}

func (u *Usecase) ActiveRequestIDFor(ctx context.Context, userID uuid.UUID, isProvider bool) (string, error) {
	if isProvider {
		request, err := u.Requests.ActiveOfProvider(ctx, userID)
		if err != nil || request == nil {
			return "", err
		}
		return request.ID.String(), nil
	}
	request, err := u.Requests.ActiveOfCustomer(ctx, userID)
	if err != nil || request == nil || !request.Status.HasProvider() {
		return "", err
	}
	return request.ID.String(), nil
}

func (u *Usecase) cancel(ctx context.Context, requestID uuid.UUID, order cancelOrder) (*entities.ServiceRequest, error) {
	var request *entities.ServiceRequest
	var released []reservation
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !order.allowed(request) {
			return ErrRequestNotFound
		}
		if request.WorkshopID != nil {
			return entities.ErrBookingManaged
		}
		now := u.now()
		reason := order.text
		if order.reasonID != nil {
			if reason, err = u.resolveReason(ctx, *order.reasonID, order.scope, request.CategoryID, order.note); err != nil {
				return err
			}
		}
		fee := int64(0)
		if order.actor.Type == entities.RequestActorCustomer {
			if fee, err = u.cancelFee(ctx, request, now); err != nil {
				return err
			}
			if fee > 0 && (order.expectedFee == nil || *order.expectedFee != fee) {
				return &CancelFeeChangedError{FeeCents: fee, Currency: request.Fare.Currency}
			}
		}
		change, err := request.Transition(entities.RequestCancelled, order.actor, reason, now)
		if err != nil {
			return err
		}
		request.CancelReasonID, request.CancelFeeCents = order.reasonID, fee
		if request.VoucherID != nil {
			if err := u.Vouchers.Release(ctx, request.ID); err != nil {
				return err
			}
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		if err := u.recordTransition(ctx, request, change, policy); err != nil {
			return err
		}
		if order.actor.Type == entities.RequestActorProvider && order.actor.ID != nil {
			if err := u.Cancels.CountProviderCancellation(ctx, *order.actor.ID); err != nil {
				return err
			}
		}
		released, err = u.closeOffers(ctx, request, nil, "request_cancelled", now)
		if err != nil {
			return err
		}
		return u.refundPrepaid(ctx, request, "Pedido cancelado")
	})
	if err != nil {
		return nil, err
	}
	u.releaseAll(ctx, released)
	u.clearTrip(ctx, request)
	businessmetrics.RequestOutcomes.WithLabelValues(request.CategorySlug, string(request.Status)).Inc()
	return request, nil
}

func (u *Usecase) clearTrip(ctx context.Context, request *entities.ServiceRequest) {
	if request.ProviderID == nil {
		return
	}
	if err := u.Locator.ClearTrip(ctx, *request.ProviderID); err != nil {
		logger.FromContext(ctx).Warn("active trip not cleared", zap.Error(err))
	}
}

func (u *Usecase) Boost(ctx context.Context, customerID, requestID uuid.UUID, percent int) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	var released []reservation
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		if !policy.AllowBoost || !request.Status.IsSearching() || request.Prepay || request.RemovalDay != nil {
			return entities.ErrBoostNotAllowed
		}
		now := u.now()
		added, err := request.ApplyBoost(percent * 100)
		if err != nil {
			return err
		}
		if err := u.Requests.AddFareAdjustment(ctx, entities.FareAdjustment{RequestID: request.ID, Reason: entities.AdjustmentBoost, AmountCents: added, ActorID: &customerID, CreatedAt: now}); err != nil {
			return err
		}
		if released, err = u.closeOffers(ctx, request, nil, "fare_boosted", now); err != nil {
			return err
		}
		expected := request.Version
		if request.Status == entities.RequestOffered {
			change, err := request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "fare_boosted", now)
			if err != nil {
				return err
			}
			if err := u.announceTransition(ctx, request, change, policy); err != nil {
				return err
			}
		} else {
			request.Touch(now)
		}
		request.BeginSearch(policy, now)
		if err := u.Requests.Update(ctx, request, expected); err != nil {
			return err
		}
		return u.publish(ctx, events.FareBoostedV1{
			RequestID: request.ID, CustomerID: request.CustomerID, Category: request.CategorySlug, Version: request.Version, BoostBps: request.Fare.BoostBps,
			FareCents: *request.Fare.Cents, AddedCents: added, Currency: request.Fare.Currency, OccurredAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	u.releaseAll(ctx, released)
	businessmetrics.FareBoosts.WithLabelValues(request.CategorySlug).Inc()
	if err := u.Advance(ctx, request.ID); err != nil {
		logger.FromContext(ctx).Warn("search after boost failed, the watcher will retry", zap.Error(err))
	}
	if fresh, err := u.Requests.Find(ctx, request.ID); err == nil && fresh != nil {
		request = fresh
	}
	return u.customerView(ctx, request)
}

func (u *Usecase) ChangeRoute(ctx context.Context, customerID, requestID uuid.UUID, in dtos.ChangeRouteInput) (*dtos.RequestView, error) {
	current, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if current == nil || !current.IsCustomer(customerID) {
		return nil, ErrRequestNotFound
	}
	if current.RemovalDay != nil {
		return nil, ErrRouteChangeForbidden
	}
	destination := in.Destination.ToPlace()
	if err := destination.Point.Validate("destination"); err != nil {
		return nil, err
	}
	via := make([]entities.GeoPoint, 0, len(in.Stops))
	stops := make([]entities.RequestPlace, 0, len(in.Stops))
	for _, stop := range in.Stops {
		via = append(via, entities.GeoPoint{Lat: stop.Lat, Lng: stop.Lng})
		stops = append(stops, stop.ToPlace())
	}
	route, err := u.Routing.Route(ctx, current.Pickup.Point, destination.Point, via...)
	if err != nil {
		return nil, err
	}
	var request *entities.ServiceRequest
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.Status.IsTerminal() || request.Destination == nil || request.Fare.EstimateID == nil || request.Fare.Cents == nil {
			return ErrRouteChangeForbidden
		}
		estimate, err := u.Pricing.FindEstimate(ctx, *request.Fare.EstimateID)
		if err != nil {
			return err
		}
		tariff, err := u.Pricing.FindTariff(ctx, estimate.TariffID)
		if err != nil {
			return err
		}
		zone, err := u.Config.FindZone(ctx, request.ZoneID)
		if err != nil {
			return err
		}
		now := u.now()
		location := time.UTC
		if zone != nil {
			location = zone.Location()
		}
		priceAt := now
		if request.ScheduledFor != nil {
			priceAt = *request.ScheduledFor
		}
		base := tariff.FareForRoute(route.DistanceMeters, route.Duration, priceAt.In(location), len(stops))
		if request.VehicleTypeID != nil {
			vehicleType, err := u.Reference.Item(ctx, *request.VehicleTypeID)
			if err != nil {
				return err
			}
			if vehicleType != nil {
				base = entities.TowingFare(vehicleType, vehicleType.PriceAt(estimate.CreatedAt), route.DistanceMeters)
			}
		}
		quote := tariff.Quote(base, estimate.Demand())
		boosted := quote.TotalCents + quote.TotalCents*int64(request.Fare.BoostBps)/10_000
		difference := boosted - *request.Fare.Cents
		if err := u.repriceVoucher(ctx, request, boosted); err != nil {
			return err
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		if err := request.ChangeRoute(destination, stops, policy, boosted, quote.CommissionCents, route.Geometry); err != nil {
			return err
		}
		cell, err := u.Geo.Cell(destination.Point, analyticsResolution)
		if err != nil {
			return err
		}
		request.DestinationH3 = &cell
		if err := u.Requests.Update(ctx, request, request.Touch(now)); err != nil {
			return err
		}
		if err := u.Requests.AddFareAdjustment(ctx, entities.FareAdjustment{RequestID: request.ID, Reason: entities.AdjustmentRouteChange, AmountCents: difference, ActorID: &customerID, CreatedAt: now}); err != nil {
			return err
		}
		return u.publish(ctx, events.RouteChangedV1{
			RequestID: request.ID, CustomerID: request.CustomerID, ProviderID: request.ProviderID, Category: request.CategorySlug, Version: request.Version,
			FareCents: boosted, Currency: request.Fare.Currency, OccurredAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return u.customerView(ctx, request)
}

func (u *Usecase) customerSummary(ctx context.Context, request *entities.ServiceRequest) (*dtos.RequestView, error) {
	view := baseRequestView(request)
	view.PassengerName = request.PassengerName
	view.PassengerPhone = request.PassengerPhone
	view.ContactPhone = request.ContactPhone
	view.Details = request.Details
	view.CompletionCode = request.CompletionCode
	if request.RemovalDay != nil {
		team, err := u.teamOf(ctx, request.ID)
		if err != nil {
			return nil, err
		}
		view.Team = team
	}
	provider, err := u.providerSummary(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}
	view.Provider = provider
	service, err := u.Config.FindService(ctx, request.ServiceID)
	if err != nil {
		return nil, err
	}
	if service != nil {
		view.ServiceName = service.Name
	}
	if request.Status == entities.RequestCompleted {
		rating, err := u.Ratings.OfRequest(ctx, request.ID)
		if err != nil {
			return nil, err
		}
		view.CanRate = rating == nil
		if rating != nil {
			view.Rating = &dtos.GivenRating{Stars: rating.Stars, ComplimentIDs: rating.ComplimentIDs, Comment: rating.Comment, CreatedAt: rating.CreatedAt}
		}
	}
	return &view, nil
}

func (u *Usecase) customerView(ctx context.Context, request *entities.ServiceRequest) (*dtos.RequestView, error) {
	view, err := u.customerSummary(ctx, request)
	if err != nil {
		return nil, err
	}
	policy, err := u.policyOf(ctx, request)
	if err != nil {
		return nil, err
	}
	view.CanBoost = policy.AllowBoost && request.Status.IsSearching() && request.Fare.BoostBps < entities.BoostTwentyPercentBps && request.Fare.Cents != nil
	if request.Status == entities.RequestAwaitingChoice {
		if view.Quotes, err = u.quotesOf(ctx, request); err != nil {
			return nil, err
		}
	}
	if request.Status.HasProvider() && !request.Status.IsTerminal() && policy.Tracks(trackedStatus(request.Status)) && request.ProviderID != nil {
		if position, err := u.Locator.LastPosition(ctx, *request.ProviderID); err == nil && position != nil {
			view.Position = &dtos.PositionView{Lat: position.Point.Lat, Lng: position.Point.Lng, AgeS: int(u.now().Sub(position.RecordedAt).Seconds())}
		}
	}
	return u.withAdjustments(ctx, view, request)
}

func trackedStatus(status entities.RequestStatus) entities.RequestStatus {
	if status == entities.RequestAccepted {
		return entities.RequestProviderEnRoute
	}
	return status
}

func (u *Usecase) providerView(ctx context.Context, request *entities.ServiceRequest) (*dtos.RequestView, error) {
	view := baseRequestView(request)
	view.Details = request.Details
	view.ProviderNet = request.Fare.ProviderNetCents()
	view.CommissionCents = request.Fare.CommissionCents
	if !request.Status.IsTerminal() {
		view.ContactPhone = request.ContactPhone
		view.PassengerName = request.PassengerName
		view.PassengerPhone = request.PassengerPhone
	}
	return u.withAdjustments(ctx, &view, request)
}

func (u *Usecase) withAdjustments(ctx context.Context, view *dtos.RequestView, request *entities.ServiceRequest) (*dtos.RequestView, error) {
	adjustments, err := u.Requests.ListFareAdjustments(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	for _, adjustment := range adjustments {
		view.Adjustments = append(view.Adjustments, dtos.AdjustmentView{Reason: string(adjustment.Reason), AmountCents: adjustment.AmountCents, CreatedAt: adjustment.CreatedAt})
	}
	if view.Breakdown, err = u.breakdownOf(ctx, request, adjustments); err != nil {
		return nil, err
	}
	return view, nil
}

func baseRequestView(r *entities.ServiceRequest) dtos.RequestView {
	stops := make([]dtos.PlaceView, 0, len(r.Stops))
	for _, stop := range r.Stops {
		stops = append(stops, dtos.ToPlaceView(stop))
	}
	view := dtos.RequestView{
		ID: r.ID, Code: r.PublicCode, Status: string(r.Status), Version: r.Version, Category: r.CategorySlug, ServiceID: r.ServiceID, Pickup: dtos.ToPlaceView(r.Pickup), Stops: stops, RouteGeometry: r.Fare.RouteGeometry,
		ScheduledFor: r.ScheduledFor, FareCents: r.Fare.Cents, BoostBps: r.Fare.BoostBps, Currency: r.Fare.Currency, PaymentMethod: string(r.PaymentMethod), PaymentStatus: string(r.PaymentStatus), DiscountCents: r.DiscountCents, VoucherCode: r.VoucherCode, AmountDueCents: r.ExpectedDueCents(), NeedsOperator: r.NeedsOperator, OperatorReason: r.OperatorReason,
		CancelReason: r.CancelReason, CancelledBy: r.CancelledBy, CancelReasonID: r.CancelReasonID, CancelFeeCents: r.CancelFeeCents, AcceptedAt: r.AcceptedAt, ArrivedAt: r.ArrivedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt, CreatedAt: r.CreatedAt,
	}
	if r.Destination != nil {
		destination := dtos.ToPlaceView(*r.Destination)
		view.Destination = &destination
	}
	return view
}

func (u *Usecase) providerSummary(ctx context.Context, providerID *uuid.UUID) (*dtos.ProviderSummary, error) {
	if providerID == nil {
		return nil, nil
	}
	name, _, err := u.Devices.Contact(ctx, *providerID)
	if err != nil {
		return nil, err
	}
	avatar, err := u.avatarOf(ctx, *providerID)
	if err != nil {
		return nil, err
	}
	summary := &dtos.ProviderSummary{ID: *providerID, Name: name, AvatarURL: avatar}
	profile, err := u.Providers.Find(ctx, *providerID)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		summary.VehiclePlate, summary.VehicleDescription = profile.Vehicle.Plate, profile.Vehicle.Description()
		summary.Rating, summary.RatingCount, summary.CompletedRequests = entities.AverageRating(profile.RatingSum, profile.RatingCount), profile.RatingCount, profile.CompletedRequests
	}
	return summary, nil
}

func (u *Usecase) avatarOf(ctx context.Context, userID uuid.UUID) (string, error) {
	key, err := u.Account.Avatar(ctx, userID)
	if err != nil || key == "" {
		return "", err
	}
	signed, err := u.Storage.PresignDownload(ctx, key)
	if errors.Is(err, services.ErrStorageUnavailable) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}

func (u *Usecase) quotesOf(ctx context.Context, request *entities.ServiceRequest) ([]dtos.QuoteView, error) {
	offers, err := u.Offers.ListOfRequest(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	now := u.now()
	quotes := make([]dtos.QuoteView, 0, len(offers))
	for _, offer := range offers {
		if offer.Status != entities.OfferQuoted || offer.QuoteCents == nil || !now.Before(offer.ExpiresAt) {
			continue
		}
		provider, err := u.providerSummary(ctx, &offer.ProviderID)
		if err != nil {
			return nil, err
		}
		quotes = append(quotes, dtos.QuoteView{
			OfferID: offer.ID, Provider: provider, AmountCents: *offer.QuoteCents, Note: offer.QuoteNote,
			ValidForS: offer.RemainingSeconds(now), DistanceM: offer.DistanceM,
		})
	}
	return quotes, nil
}

func (u *Usecase) offerView(ctx context.Context, offer *entities.RequestOffer, now time.Time) (*dtos.OfferView, error) {
	request, err := u.Requests.Find(ctx, offer.RequestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrOfferNotFound
	}
	policy, err := u.policyOf(ctx, request)
	if err != nil {
		return nil, err
	}
	view := &dtos.OfferView{
		ID: offer.ID, RequestID: offer.RequestID, Version: request.Version, Status: string(offer.Status), Category: request.CategorySlug,
		ExpiresInS: offer.RemainingSeconds(now), Pickup: dtos.ToPlaceView(request.Pickup), DistanceM: offer.DistanceM, NetCents: offer.NetCents,
		Currency: request.Fare.Currency, Mode: string(policy.OfferMode), PaymentMethod: string(request.PaymentMethod), Details: request.Details,
	}
	if request.Destination != nil {
		destination := dtos.ToPlaceView(*request.Destination)
		view.Destination = &destination
	}
	for _, stop := range request.Stops {
		view.Stops = append(view.Stops, dtos.ToPlaceView(stop))
	}
	return view, nil
}
