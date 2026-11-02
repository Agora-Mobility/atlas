package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

const postgisInternalError = "XX000"

type DispatchConfigRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IDispatchConfigRepository = (*DispatchConfigRepository)(nil)

func NewDispatchConfigRepository(pool *pgxpool.Pool) *DispatchConfigRepository {
	return &DispatchConfigRepository{pool: pool}
}

func (r *DispatchConfigRepository) ListZones(ctx context.Context, onlyActive bool) ([]*entities.ServiceZone, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListServiceZones(ctx, onlyActive)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	zones := make([]*entities.ServiceZone, 0, len(rows))
	for i := range rows {
		row := rows[i]
		zone, err := toZone(row.ID, row.CountryCode, row.Name, row.Geojson, row.Timezone, row.Currency, row.IsActive)
		if err != nil {
			return nil, err
		}
		zone.CreatedAt, zone.UpdatedAt = row.CreatedAt, row.UpdatedAt
		zones = append(zones, zone)
	}
	return zones, nil
}

func (r *DispatchConfigRepository) FindZone(ctx context.Context, id uuid.UUID) (*entities.ServiceZone, error) {
	row, err := postgres.Queries(ctx, r.pool).GetServiceZone(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get zone: %w", err)
	}
	zone, err := toZone(row.ID, row.CountryCode, row.Name, row.Geojson, row.Timezone, row.Currency, row.IsActive)
	if err != nil {
		return nil, err
	}
	zone.CreatedAt, zone.UpdatedAt = row.CreatedAt, row.UpdatedAt
	return zone, nil
}

func (r *DispatchConfigRepository) CreateZone(ctx context.Context, zone *entities.ServiceZone) error {
	return geometryError(postgres.Queries(ctx, r.pool).CreateServiceZone(ctx, gen.CreateServiceZoneParams{
		ID: zone.ID, CountryCode: zone.CountryCode, Name: zone.Name, Geojson: zone.GeoJSON,
		Timezone: zone.Timezone, Currency: zone.Currency, IsActive: zone.IsActive,
	}))
}

func (r *DispatchConfigRepository) UpdateZone(ctx context.Context, zone *entities.ServiceZone) error {
	_, err := postgres.Queries(ctx, r.pool).UpdateServiceZone(ctx, gen.UpdateServiceZoneParams{
		ID: zone.ID, Name: zone.Name, Geojson: zone.GeoJSON, Timezone: zone.Timezone, Currency: zone.Currency, IsActive: zone.IsActive,
	})
	return geometryError(err)
}

func geometryError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgisInternalError {
		return repository.ErrInvalidGeometry
	}
	if err != nil {
		return fmt.Errorf("save zone: %w", err)
	}
	return nil
}

func (r *DispatchConfigRepository) ZoneCoveringPoint(ctx context.Context, point entities.GeoPoint) (*uuid.UUID, error) {
	id, err := postgres.Queries(ctx, r.pool).FindZoneCoveringPoint(ctx, gen.FindZoneCoveringPointParams{Lat: point.Lat, Lng: point.Lng})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find zone: %w", err)
	}
	return &id, nil
}

func (r *DispatchConfigRepository) IsValidGeometry(ctx context.Context, geojson string) (bool, error) {
	valid, err := postgres.Queries(ctx, r.pool).IsValidZoneGeometry(ctx, geojson)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate geometry: %w", err)
	}
	return valid, nil
}

func (r *DispatchConfigRepository) ResolvePolicy(ctx context.Context, categoryID, zoneID uuid.UUID) (*entities.DispatchPolicy, error) {
	row, err := postgres.Queries(ctx, r.pool).ResolveDispatchPolicy(ctx, gen.ResolveDispatchPolicyParams{CategoryID: categoryID, ZoneID: &zoneID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve policy: %w", err)
	}
	return toPolicy(&row), nil
}

func (r *DispatchConfigRepository) FindPolicy(ctx context.Context, id uuid.UUID) (*entities.DispatchPolicy, error) {
	row, err := postgres.Queries(ctx, r.pool).GetDispatchPolicy(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}
	return toPolicy(&row), nil
}

func (r *DispatchConfigRepository) ListPolicies(ctx context.Context) ([]*entities.DispatchPolicy, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDispatchPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	policies := make([]*entities.DispatchPolicy, 0, len(rows))
	for i := range rows {
		policies = append(policies, toPolicy(&rows[i]))
	}
	return policies, nil
}

func (r *DispatchConfigRepository) CreatePolicy(ctx context.Context, p *entities.DispatchPolicy) error {
	err := postgres.Queries(ctx, r.pool).CreateDispatchPolicy(ctx, gen.CreateDispatchPolicyParams{
		ID: p.ID, CategoryID: p.CategoryID, ZoneID: p.ZoneID, CandidateSource: string(p.CandidateSource),
		H3Resolution: int16(p.H3Resolution), RadiiKm: p.RadiiKm, OfferMode: string(p.OfferMode), // #nosec G115 -- validated by the entity
		BatchSize: int16(p.BatchSize), OfferTtlSeconds: int32(p.OfferTTLSeconds), SearchTtlSeconds: int32(p.SearchTTLSeconds), // #nosec G115 -- validated by the entity
		AckTimeoutSeconds: int32(p.AckTimeoutSeconds), AckGiveupSeconds: int32(p.AckGiveUpSeconds), // #nosec G115 -- validated by the entity
		InitialChannels: p.InitialChannels, FallbackChannels: p.FallbackChannels, Ranking: string(p.Ranking),
		TrackingStatuses: statusStrings(p.TrackingStatuses), OnExhausted: string(p.OnExhausted), ReserveProvider: p.ReserveProvider,
		ScheduleMaxDays: int32(p.ScheduleMaxDays), SearchLeadSeconds: int32(p.SearchLeadSeconds), PricingMode: string(p.PricingMode), // #nosec G115 -- validated by the entity
		AllowBoost: p.AllowBoost, RankByEta: p.RankByETA, SmsOnAccept: p.SMSOnAccept, MaxStops: int16(p.MaxStops), ScheduleSlotMinutes: int16(p.ScheduleSlotMin), // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("create policy: %w", err)
	}
	return nil
}

func (r *DispatchConfigRepository) UpdatePolicy(ctx context.Context, p *entities.DispatchPolicy) error {
	_, err := postgres.Queries(ctx, r.pool).UpdateDispatchPolicy(ctx, gen.UpdateDispatchPolicyParams{
		ID: p.ID, CandidateSource: string(p.CandidateSource), H3Resolution: int16(p.H3Resolution), RadiiKm: p.RadiiKm, // #nosec G115 -- validated by the entity
		OfferMode: string(p.OfferMode), BatchSize: int16(p.BatchSize), OfferTtlSeconds: int32(p.OfferTTLSeconds), // #nosec G115 -- validated by the entity
		SearchTtlSeconds: int32(p.SearchTTLSeconds), AckTimeoutSeconds: int32(p.AckTimeoutSeconds), AckGiveupSeconds: int32(p.AckGiveUpSeconds), // #nosec G115 -- validated by the entity
		InitialChannels: p.InitialChannels, FallbackChannels: p.FallbackChannels, Ranking: string(p.Ranking),
		TrackingStatuses: statusStrings(p.TrackingStatuses), OnExhausted: string(p.OnExhausted), ReserveProvider: p.ReserveProvider,
		ScheduleMaxDays: int32(p.ScheduleMaxDays), SearchLeadSeconds: int32(p.SearchLeadSeconds), PricingMode: string(p.PricingMode), // #nosec G115 -- validated by the entity
		AllowBoost: p.AllowBoost, RankByEta: p.RankByETA, SmsOnAccept: p.SMSOnAccept, MaxStops: int16(p.MaxStops), ScheduleSlotMinutes: int16(p.ScheduleSlotMin), // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("update policy: %w", err)
	}
	return nil
}

func (r *DispatchConfigRepository) ListLiveIndexes(ctx context.Context) ([]entities.LiveIndex, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListLiveIndexes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list live indexes: %w", err)
	}
	indexes := make([]entities.LiveIndex, 0, len(rows))
	for _, row := range rows {
		indexes = append(indexes, entities.LiveIndex{CategoryID: row.CategoryID, Slug: row.Slug, Resolution: int(row.H3Resolution)})
	}
	return indexes, nil
}

func (r *DispatchConfigRepository) FindService(ctx context.Context, id uuid.UUID) (*entities.DispatchService, error) {
	row, err := postgres.Queries(ctx, r.pool).GetServiceForDispatch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	return &entities.DispatchService{
		ID: row.ID, CategoryID: row.CategoryID, CategorySlug: row.CategorySlug,
		RequiresDestination: row.RequiresDestination, IsOffered: row.ServiceActive && row.CategoryActive,
	}, nil
}

func (r *DispatchConfigRepository) ListServicesOfCategory(ctx context.Context, slug string) ([]entities.DispatchService, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListActiveServicesOfCategory(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	out := make([]entities.DispatchService, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.DispatchService{
			ID: row.ID, Name: row.Name, Slug: row.Slug, CategoryID: row.CategoryID, CategorySlug: row.CategorySlug,
			RequiresDestination: row.RequiresDestination, IsOffered: true,
		})
	}
	return out, nil
}

func (r *DispatchConfigRepository) FindCategoryIDBySlug(ctx context.Context, slug string) (*uuid.UUID, error) {
	row, err := postgres.Queries(ctx, r.pool).GetCategoryBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get category: %w", err)
	}
	return &row.ID, nil
}

func toPolicy(row *gen.DispatchPolicy) *entities.DispatchPolicy {
	tracking := make([]entities.RequestStatus, 0, len(row.TrackingStatuses))
	for _, status := range row.TrackingStatuses {
		tracking = append(tracking, entities.RequestStatus(status))
	}
	return &entities.DispatchPolicy{
		ID: row.ID, CategoryID: row.CategoryID, ZoneID: row.ZoneID, CandidateSource: entities.CandidateSource(row.CandidateSource),
		H3Resolution: int(row.H3Resolution), RadiiKm: row.RadiiKm, OfferMode: entities.OfferMode(row.OfferMode), BatchSize: int(row.BatchSize),
		OfferTTLSeconds: int(row.OfferTtlSeconds), SearchTTLSeconds: int(row.SearchTtlSeconds), AckTimeoutSeconds: int(row.AckTimeoutSeconds),
		AckGiveUpSeconds: int(row.AckGiveupSeconds), InitialChannels: row.InitialChannels, FallbackChannels: row.FallbackChannels,
		Ranking: entities.Ranking(row.Ranking), TrackingStatuses: tracking, OnExhausted: entities.ExhaustedAction(row.OnExhausted),
		ReserveProvider: row.ReserveProvider, ScheduleMaxDays: int(row.ScheduleMaxDays), SearchLeadSeconds: int(row.SearchLeadSeconds),
		PricingMode: entities.PricingMode(row.PricingMode), AllowBoost: row.AllowBoost, RankByETA: row.RankByEta, SMSOnAccept: row.SmsOnAccept, MaxStops: int(row.MaxStops), ScheduleSlotMin: int(row.ScheduleSlotMinutes),
	}
}

func statusStrings(statuses []entities.RequestStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, string(status))
	}
	return out
}

type geoJSONMultiPolygon struct {
	Coordinates [][][][2]float64 `json:"coordinates"`
}

func toZone(id uuid.UUID, country, name, geojson, timezone, currency string, active bool) (*entities.ServiceZone, error) {
	polygons, err := ParseMultiPolygon(geojson)
	if err != nil {
		return nil, err
	}
	return &entities.ServiceZone{
		ID: id, CountryCode: country, Name: name, GeoJSON: geojson, Timezone: timezone, Currency: currency,
		IsActive: active, Polygons: polygons,
	}, nil
}

func ParseMultiPolygon(geojson string) ([]entities.GeoPolygon, error) {
	var parsed geoJSONMultiPolygon
	if err := json.Unmarshal([]byte(geojson), &parsed); err != nil {
		return nil, fmt.Errorf("%w: %w", repository.ErrInvalidGeometry, err)
	}
	polygons := make([]entities.GeoPolygon, 0, len(parsed.Coordinates))
	for _, rings := range parsed.Coordinates {
		var polygon entities.GeoPolygon
		for i, ring := range rings {
			points := make([]entities.GeoPoint, 0, len(ring))
			for _, position := range ring {
				points = append(points, entities.GeoPoint{Lng: position[0], Lat: position[1]})
			}
			if i == 0 {
				polygon.Outer = points
				continue
			}
			polygon.Holes = append(polygon.Holes, points)
		}
		polygons = append(polygons, polygon)
	}
	return polygons, nil
}
