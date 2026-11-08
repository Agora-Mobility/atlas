package dispatch

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func (u *Usecase) CreateEstimates(ctx context.Context, customerID uuid.UUID, in dtos.FareEstimateInput) (*dtos.FareEstimateList, error) {
	pickup := entities.GeoPoint{Lat: in.Pickup.Lat, Lng: in.Pickup.Lng}
	if err := pickup.Validate("pickup"); err != nil {
		return nil, err
	}
	zone, err := u.zones.Resolve(ctx, pickup)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, ErrOutsideServiceArea
	}
	offered, err := u.Config.ListServicesOfCategory(ctx, in.Category)
	if err != nil {
		return nil, err
	}
	if len(offered) == 0 {
		return nil, ErrServiceNotOffered
	}
	var destination *entities.GeoPoint
	if in.Destination != nil {
		point := entities.GeoPoint{Lat: in.Destination.Lat, Lng: in.Destination.Lng}
		if err := point.Validate("destination"); err != nil {
			return nil, err
		}
		destination = &point
	}
	if offered[0].RequiresDestination && destination == nil {
		return nil, entitiesDestinationError()
	}
	stops := make([]entities.GeoPoint, 0, len(in.Stops))
	for _, stop := range in.Stops {
		point := entities.GeoPoint{Lat: stop.Lat, Lng: stop.Lng}
		if err := point.Validate("stops"); err != nil {
			return nil, err
		}
		stops = append(stops, point)
	}

	policy, err := u.Config.ResolvePolicy(ctx, offered[0].CategoryID, zone.ID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, ErrServiceNotOffered
	}
	if err := policy.ValidateStops(len(stops)); err != nil {
		return nil, err
	}
	now := u.now()
	priceAt := now
	if in.ScheduledFor != nil {
		if err := policy.ValidateSchedule(*in.ScheduledFor, now); err != nil {
			return nil, err
		}
		priceAt = *in.ScheduledFor
	}
	local := priceAt.In(zone.Location())
	route, err := u.routeFor(ctx, pickup, destination, stops, local.Hour())
	if err != nil {
		return nil, err
	}

	list := &dtos.FareEstimateList{Items: make([]dtos.FareEstimateView, 0, len(offered))}
	if destination != nil {
		list.Route = &dtos.RouteView{DistanceM: route.DistanceMeters, DurationS: int(route.Duration.Seconds()), Geometry: route.Geometry, Source: string(route.Source)}
	}
	expiresAt := now.Add(u.settings.FareEstimateTTL)
	vehicleTypes, err := u.Reference.Items(ctx, entities.CatalogTowingVehicle, in.Category, false)
	if err != nil {
		return nil, err
	}
	removalVehicles, err := u.Reference.Items(ctx, entities.CatalogRemovalVehicle, in.Category, false)
	if err != nil {
		return nil, err
	}
	for i := range offered {
		service := &offered[i]
		tariff, err := u.Pricing.CurrentTariff(ctx, service.ID, zone.ID)
		if err != nil {
			return nil, err
		}
		if tariff == nil {
			continue
		}
		if len(removalVehicles) > 0 {
			if err := u.removalEstimates(ctx, list, removalVehicles, service, tariff, customerID, pickup, destination, stops, in, route, now, expiresAt); err != nil {
				return nil, err
			}
			continue
		}
		if len(vehicleTypes) > 0 {
			if err := u.towingEstimates(ctx, list, vehicleTypes, service, tariff, customerID, pickup, destination, stops, in, route, now, expiresAt); err != nil {
				return nil, err
			}
			continue
		}
		total := tariff.FareForRoute(route.DistanceMeters, route.Duration, local, len(stops))
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		estimate := &entities.FareEstimate{
			ID: id, CustomerID: customerID, TariffID: tariff.ID, ServiceID: service.ID, Origin: pickup, Destination: destination, Stops: stops, ScheduledFor: in.ScheduledFor,
			DistanceMeters: route.DistanceMeters, DurationSeconds: int(route.Duration.Seconds()), RouteSource: string(route.Source), RouteGeometry: route.Geometry,
			TotalCents: total, CommissionCents: tariff.CommissionFor(total), Currency: tariff.Currency, ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := u.Pricing.CreateEstimate(ctx, estimate); err != nil {
			return nil, err
		}
		businessmetrics.FareQuotes.WithLabelValues(in.Category, string(route.Source), "estimate").Inc()
		list.Items = append(list.Items, dtos.FareEstimateView{
			FareEstimateID: id, ServiceID: service.ID, ServiceName: service.Name, ServiceSlug: service.Slug, TotalCents: total, Currency: tariff.Currency,
			DistanceM: route.DistanceMeters, DurationS: int(route.Duration.Seconds()), RouteSource: string(route.Source), ExpiresInS: int(u.settings.FareEstimateTTL.Seconds()),
		})
	}
	if len(list.Items) == 0 {
		return nil, ErrNoTariff
	}
	return list, nil
}

func (u *Usecase) towingEstimates(ctx context.Context, list *dtos.FareEstimateList, vehicleTypes []entities.CatalogItem, service *entities.DispatchService, tariff *entities.ServiceTariff, customerID uuid.UUID, pickup entities.GeoPoint, destination *entities.GeoPoint, stops []entities.GeoPoint, in dtos.FareEstimateInput, route services.Route, now, expiresAt time.Time) error {
	for i := range vehicleTypes {
		vehicleType := &vehicleTypes[i]
		total := entities.TowingFare(vehicleType, vehicleType.PriceAt(now), route.DistanceMeters)
		id, err := newUUID()
		if err != nil {
			return err
		}
		typeID := vehicleType.ID
		estimate := &entities.FareEstimate{
			ID: id, CustomerID: customerID, TariffID: tariff.ID, ServiceID: service.ID, Origin: pickup, Destination: destination, Stops: stops, ScheduledFor: in.ScheduledFor, VehicleTypeID: &typeID,
			DistanceMeters: route.DistanceMeters, DurationSeconds: int(route.Duration.Seconds()), RouteSource: string(route.Source), RouteGeometry: route.Geometry,
			TotalCents: total, CommissionCents: tariff.CommissionFor(total), Currency: tariff.Currency, ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := u.Pricing.CreateEstimate(ctx, estimate); err != nil {
			return err
		}
		included := vehicleType.AttrInt(entities.AttrIncludedKm)
		list.Items = append(list.Items, dtos.FareEstimateView{
			FareEstimateID: id, ServiceID: service.ID, ServiceName: service.Name, ServiceSlug: service.Slug, TotalCents: total, Currency: tariff.Currency, DistanceM: route.DistanceMeters,
			DurationS: int(route.Duration.Seconds()), RouteSource: string(route.Source), ExpiresInS: int(u.settings.FareEstimateTTL.Seconds()), VehicleTypeID: &typeID, VehicleType: vehicleType.Label, IncludedKm: &included,
		})
	}
	return nil
}

func (u *Usecase) routeFor(ctx context.Context, pickup entities.GeoPoint, destination *entities.GeoPoint, stops []entities.GeoPoint, hour int) (services.Route, error) {
	if destination == nil {
		return services.Route{Source: services.RouteSourceFallback}, nil
	}
	route, err := u.Routing.Route(ctx, pickup, *destination, stops...)
	if err != nil {
		return services.Route{}, err
	}
	if route.Source == services.RouteSourceFallback && len(stops) == 0 {
		u.applyLearnedDuration(ctx, &route, pickup, *destination, hour)
	}
	return route, nil
}

func (u *Usecase) applyLearnedDuration(ctx context.Context, route *services.Route, pickup, destination entities.GeoPoint, hour int) {
	origin, err := u.Geo.Cell(pickup, analyticsResolution)
	if err != nil {
		return
	}
	target, err := u.Geo.Cell(destination, analyticsResolution)
	if err != nil {
		return
	}
	learned, samples, err := u.Pricing.LearnedETA(ctx, origin, target, hour)
	if err != nil {
		logger.FromContext(ctx).Warn("learned eta unavailable", zap.Error(err))
		return
	}
	if samples >= minLearnedSamples && learned > 0 {
		route.Duration = learned.Round(time.Second)
	}
}
