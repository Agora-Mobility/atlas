package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type PricingRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IPricingRepository = (*PricingRepository)(nil)

func NewPricingRepository(pool *pgxpool.Pool) *PricingRepository {
	return &PricingRepository{pool: pool}
}

func (r *PricingRepository) CurrentTariff(ctx context.Context, serviceID, zoneID uuid.UUID) (*entities.ServiceTariff, error) {
	row, err := postgres.Queries(ctx, r.pool).CurrentServiceTariff(ctx, gen.CurrentServiceTariffParams{ServiceID: serviceID, ZoneID: &zoneID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("current tariff: %w", err)
	}
	return toTariff(&row), nil
}

func (r *PricingRepository) FindTariff(ctx context.Context, id uuid.UUID) (*entities.ServiceTariff, error) {
	row, err := postgres.Queries(ctx, r.pool).GetServiceTariff(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tariff: %w", err)
	}
	return toTariff(&row), nil
}

func (r *PricingRepository) ListCurrentTariffs(ctx context.Context) ([]*entities.ServiceTariff, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListCurrentServiceTariffs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tariffs: %w", err)
	}
	tariffs := make([]*entities.ServiceTariff, 0, len(rows))
	for i := range rows {
		tariffs = append(tariffs, toTariff(&rows[i]))
	}
	return tariffs, nil
}

func (r *PricingRepository) ReplaceTariff(ctx context.Context, t *entities.ServiceTariff) error {
	queries := postgres.Queries(ctx, r.pool)
	validUntil := t.ValidFrom
	if err := queries.CloseServiceTariff(ctx, gen.CloseServiceTariffParams{ServiceID: t.ServiceID, ZoneID: t.ZoneID, ValidUntil: &validUntil}); err != nil {
		return fmt.Errorf("close tariff: %w", err)
	}
	err := queries.CreateServiceTariff(ctx, gen.CreateServiceTariffParams{
		ID: t.ID, ServiceID: t.ServiceID, ZoneID: t.ZoneID, Currency: t.Currency, BaseFeeCents: t.BaseFeeCents, PerKmFeeCents: t.PerKmFeeCents,
		PerMinuteFeeCents: t.PerMinuteFeeCents, MinimumFeeCents: t.MinimumFeeCents, ZoneFeeCents: t.ZoneFeeCents, NightFeeCents: t.NightFeeCents,
		NightStartHour: int16(t.NightStartHour), NightEndHour: int16(t.NightEndHour), FreeWaitSeconds: int32(t.FreeWaitSeconds), // #nosec G115 -- validated by the entity
		CommissionBps: int32(t.CommissionBps), ValidFrom: t.ValidFrom, StopFeeCents: t.StopFeeCents, // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("create tariff: %w", err)
	}
	return nil
}

func (r *PricingRepository) CreateEstimate(ctx context.Context, e *entities.FareEstimate) error {
	stops, err := json.Marshal(e.Stops)
	if err != nil {
		return fmt.Errorf("encode stops: %w", err)
	}
	var destinationLat, destinationLng *float64
	if e.Destination != nil {
		destinationLat, destinationLng = &e.Destination.Lat, &e.Destination.Lng
	}
	err = postgres.Queries(ctx, r.pool).CreateFareEstimate(ctx, gen.CreateFareEstimateParams{
		ID: e.ID, CustomerID: e.CustomerID, TariffID: e.TariffID, ServiceID: e.ServiceID, OriginLat: e.Origin.Lat, OriginLng: e.Origin.Lng,
		DestinationLat: destinationLat, DestinationLng: destinationLng, Stops: stops, DistanceM: int32(e.DistanceMeters), // #nosec G115 -- a route fits in int32
		DurationS: int32(e.DurationSeconds), RouteSource: e.RouteSource, TotalCents: e.TotalCents, CommissionCents: e.CommissionCents, // #nosec G115 -- a route fits in int32
		Currency: e.Currency, ExpiresAt: e.ExpiresAt, CreatedAt: e.CreatedAt, ScheduledFor: e.ScheduledFor, VehicleTypeID: e.VehicleTypeID, Details: detailsOrEmpty(e.Details), RouteGeometry: e.RouteGeometry,
		DemandFactorBps: int32(e.Demand().FactorBps), DemandCents: e.DemandCents, DemandCommissioned: e.DemandCommissioned, // #nosec G115 -- capped by the entity
	})
	if err != nil {
		return fmt.Errorf("create estimate: %w", err)
	}
	return nil
}

func (r *PricingRepository) FindEstimate(ctx context.Context, id uuid.UUID) (*entities.FareEstimate, error) {
	row, err := postgres.Queries(ctx, r.pool).GetFareEstimate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get estimate: %w", err)
	}
	estimate := &entities.FareEstimate{
		ID: row.ID, CustomerID: row.CustomerID, TariffID: row.TariffID, ServiceID: row.ServiceID,
		Origin: entities.GeoPoint{Lat: row.OriginLat, Lng: row.OriginLng}, DistanceMeters: int(row.DistanceM), DurationSeconds: int(row.DurationS),
		RouteSource: row.RouteSource, RouteGeometry: row.RouteGeometry, TotalCents: row.TotalCents, CommissionCents: row.CommissionCents, Currency: row.Currency,
		ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt, ScheduledFor: row.ScheduledFor, UsedAt: row.UsedAt, VehicleTypeID: row.VehicleTypeID, Details: row.Details,
		DemandFactorBps: int(row.DemandFactorBps), DemandCents: row.DemandCents, DemandCommissioned: row.DemandCommissioned,
	}
	if row.DestinationLat != nil && row.DestinationLng != nil {
		estimate.Destination = &entities.GeoPoint{Lat: *row.DestinationLat, Lng: *row.DestinationLng}
	}
	if err := json.Unmarshal(row.Stops, &estimate.Stops); err != nil {
		return nil, fmt.Errorf("decode stops: %w", err)
	}
	return estimate, nil
}

func (r *PricingRepository) UseEstimate(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).UseFareEstimate(ctx, gen.UseFareEstimateParams{ID: id, UsedAt: &now})
	if err != nil {
		return false, fmt.Errorf("use estimate: %w", err)
	}
	return rows > 0, nil
}

func (r *PricingRepository) RecordETA(ctx context.Context, originCell, destinationCell int64, hourBand int, duration time.Duration) error {
	err := postgres.Queries(ctx, r.pool).RecordETAObservation(ctx, gen.RecordETAObservationParams{
		OriginH3R8: originCell, DestinationH3R8: destinationCell, HourBand: int16(hourBand), TotalSeconds: int64(duration.Seconds()), // #nosec G115 -- 0 to 23
	})
	if err != nil {
		return fmt.Errorf("record eta: %w", err)
	}
	return nil
}

func (r *PricingRepository) LearnedETA(ctx context.Context, originCell, destinationCell int64, hourBand int) (time.Duration, int, error) {
	row, err := postgres.Queries(ctx, r.pool).GetETAObservation(ctx, gen.GetETAObservationParams{
		OriginH3R8: originCell, DestinationH3R8: destinationCell, HourBand: int16(hourBand), // #nosec G115 -- 0 to 23
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("learned eta: %w", err)
	}
	return time.Duration(row.TotalSeconds/int64(row.Samples)) * time.Second, int(row.Samples), nil
}

func toTariff(row *gen.ServiceTariff) *entities.ServiceTariff {
	return &entities.ServiceTariff{
		ID: row.ID, ServiceID: row.ServiceID, ZoneID: row.ZoneID, Currency: row.Currency, BaseFeeCents: row.BaseFeeCents, PerKmFeeCents: row.PerKmFeeCents,
		PerMinuteFeeCents: row.PerMinuteFeeCents, MinimumFeeCents: row.MinimumFeeCents, ZoneFeeCents: row.ZoneFeeCents, NightFeeCents: row.NightFeeCents, StopFeeCents: row.StopFeeCents,
		NightStartHour: int(row.NightStartHour), NightEndHour: int(row.NightEndHour), FreeWaitSeconds: int(row.FreeWaitSeconds), CommissionBps: int(row.CommissionBps),
		ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil,
	}
}

func detailsOrEmpty(details json.RawMessage) json.RawMessage {
	if len(details) == 0 {
		return json.RawMessage("{}")
	}
	return details
}
