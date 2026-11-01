package repository

import (
	"context"
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

type ProviderRepository struct {
	pool *pgxpool.Pool
	tx   *postgres.TxManager
}

var _ repository.IProviderRepository = (*ProviderRepository)(nil)

func NewProviderRepository(pool *pgxpool.Pool) *ProviderRepository {
	return &ProviderRepository{pool: pool, tx: postgres.NewTxManager(pool)}
}

func (r *ProviderRepository) FindProfile(ctx context.Context, userID uuid.UUID) (*entities.ProviderProfile, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	profile := &entities.ProviderProfile{
		UserID: row.UserID, Status: entities.ProviderStatus(row.Status), VehiclePlate: row.VehiclePlate, VehicleDescription: row.VehicleDescription,
		ServiceRadiusKm: row.ServiceRadiusKm, LastJobCompletedAt: row.LastJobCompletedAt, CreatedAt: row.CreatedAt,
	}
	if row.BaseLat != nil && row.BaseLng != nil {
		profile.Base = &entities.GeoPoint{Lat: *row.BaseLat, Lng: *row.BaseLng}
	}
	return profile, nil
}

func (r *ProviderRepository) SaveProfile(ctx context.Context, p *entities.ProviderProfile, serviceIDs []uuid.UUID) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		err := queries.UpsertProviderProfile(ctx, gen.UpsertProviderProfileParams{
			UserID: p.UserID, Status: string(p.Status), VehiclePlate: p.VehiclePlate, VehicleDescription: p.VehicleDescription,
		})
		if err != nil {
			return fmt.Errorf("save provider: %w", err)
		}
		if err := queries.DeleteProviderServices(ctx, p.UserID); err != nil {
			return fmt.Errorf("clear provider services: %w", err)
		}
		for _, serviceID := range serviceIDs {
			if err := queries.AddProviderService(ctx, gen.AddProviderServiceParams{ProviderID: p.UserID, ServiceID: serviceID}); err != nil {
				return fmt.Errorf("add provider service: %w", err)
			}
		}
		return nil
	})
}

func (r *ProviderRepository) ListServiceIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).ListProviderServiceIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list provider services: %w", err)
	}
	return ids, nil
}

func (r *ProviderRepository) ListCategories(ctx context.Context, userID uuid.UUID) ([]entities.LiveIndex, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListProviderCategories(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list provider categories: %w", err)
	}
	out := make([]entities.LiveIndex, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.LiveIndex{CategoryID: row.ID, Slug: row.Slug})
	}
	return out, nil
}

func (r *ProviderRepository) FilterEligible(ctx context.Context, serviceID uuid.UUID, providerIDs []uuid.UUID) ([]entities.EligibleProvider, error) {
	rows, err := postgres.Queries(ctx, r.pool).FilterEligibleProviders(ctx, gen.FilterEligibleProvidersParams{ServiceID: serviceID, ProviderIds: providerIDs})
	if err != nil {
		return nil, fmt.Errorf("filter eligible: %w", err)
	}
	out := make([]entities.EligibleProvider, 0, len(rows))
	for i := range rows {
		row := rows[i]
		provider := entities.EligibleProvider{UserID: row.UserID, ServiceRadiusKm: row.ServiceRadiusKm, LastJobCompletedAt: row.LastJobCompletedAt, CreatedAt: row.CreatedAt}
		if row.Phone != nil {
			provider.Phone = *row.Phone
		}
		if row.BaseLat != nil && row.BaseLng != nil {
			provider.Base = &entities.GeoPoint{Lat: *row.BaseLat, Lng: *row.BaseLng}
		}
		out = append(out, provider)
	}
	return out, nil
}

func (r *ProviderRepository) WithActiveJob(ctx context.Context, providerIDs []uuid.UUID, busyUntil time.Time) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).ProvidersWithActiveJob(ctx, gen.ProvidersWithActiveJobParams{ProviderIds: providerIDs, BusyUntil: busyUntil})
	if err != nil {
		return nil, fmt.Errorf("providers with active job: %w", err)
	}
	return ids, nil
}

func (r *ProviderRepository) ReplaceCoverage(ctx context.Context, providerID uuid.UUID, base entities.GeoPoint, radiusKm float64, cellsByCategory map[uuid.UUID][]int64) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		rows, err := queries.UpdateProviderBase(ctx, gen.UpdateProviderBaseParams{UserID: providerID, BaseLat: &base.Lat, BaseLng: &base.Lng, ServiceRadiusKm: &radiusKm})
		if err != nil {
			return fmt.Errorf("update provider base: %w", err)
		}
		if rows == 0 {
			return pgx.ErrNoRows
		}
		if err := queries.DeleteProviderCoverage(ctx, providerID); err != nil {
			return fmt.Errorf("clear coverage: %w", err)
		}
		for categoryID, cells := range cellsByCategory {
			if err := queries.InsertProviderCoverage(ctx, gen.InsertProviderCoverageParams{CategoryID: categoryID, Cells: cells, ProviderID: providerID}); err != nil {
				return fmt.Errorf("insert coverage: %w", err)
			}
		}
		return nil
	})
}

func (r *ProviderRepository) CoveringCell(ctx context.Context, categoryID uuid.UUID, cell int64) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).ProvidersCoveringCell(ctx, gen.ProvidersCoveringCellParams{CategoryID: categoryID, H3R6: cell})
	if err != nil {
		return nil, fmt.Errorf("providers covering cell: %w", err)
	}
	return ids, nil
}

func (r *ProviderRepository) TouchJobCompleted(ctx context.Context, providerID uuid.UUID, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).TouchProviderJobCompleted(ctx, gen.TouchProviderJobCompletedParams{UserID: providerID, LastJobCompletedAt: &at})
	if err != nil {
		return fmt.Errorf("touch provider: %w", err)
	}
	return nil
}

func (r *ProviderRepository) SaveLocationSample(ctx context.Context, providerID, requestID uuid.UUID, point entities.GeoPoint, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).InsertLocationSample(ctx, gen.InsertLocationSampleParams{
		ProviderID: providerID, RequestID: requestID, Lat: point.Lat, Lng: point.Lng, RecordedAt: at,
	})
	if err != nil {
		return fmt.Errorf("save location sample: %w", err)
	}
	return nil
}

func (r *ProviderRepository) PurgeLocationSamples(ctx context.Context, before time.Time) (int64, error) {
	rows, err := postgres.Queries(ctx, r.pool).PurgeLocationSamples(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("purge location samples: %w", err)
	}
	return rows, nil
}

type DeviceRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IDeviceRepository = (*DeviceRepository)(nil)

func NewDeviceRepository(pool *pgxpool.Pool) *DeviceRepository {
	return &DeviceRepository{pool: pool}
}

func (r *DeviceRepository) Register(ctx context.Context, userID uuid.UUID, token, platform string) error {
	err := postgres.Queries(ctx, r.pool).UpsertDeviceToken(ctx, gen.UpsertDeviceTokenParams{Token: token, UserID: userID, Platform: platform})
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

func (r *DeviceRepository) Unregister(ctx context.Context, userID uuid.UUID, token string) error {
	err := postgres.Queries(ctx, r.pool).DeleteUserDeviceToken(ctx, gen.DeleteUserDeviceTokenParams{Token: token, UserID: userID})
	if err != nil {
		return fmt.Errorf("unregister device: %w", err)
	}
	return nil
}

func (r *DeviceRepository) Remove(ctx context.Context, token string) error {
	if err := postgres.Queries(ctx, r.pool).DeleteDeviceToken(ctx, token); err != nil {
		return fmt.Errorf("remove device: %w", err)
	}
	return nil
}

func (r *DeviceRepository) Tokens(ctx context.Context, userID uuid.UUID) ([]string, error) {
	tokens, err := postgres.Queries(ctx, r.pool).ListDeviceTokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list device tokens: %w", err)
	}
	return tokens, nil
}

func (r *DeviceRepository) Contact(ctx context.Context, userID uuid.UUID) (name, phone string, err error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserContact(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get contact: %w", err)
	}
	if row.Phone != nil {
		phone = *row.Phone
	}
	return row.Name, phone, nil
}

func (r *DeviceRepository) RecordNotification(ctx context.Context, record repository.NotificationRecord) error {
	err := postgres.Queries(ctx, r.pool).RecordNotification(ctx, gen.RecordNotificationParams{
		UserID: record.UserID, Channel: record.Channel, Template: record.Template, Status: record.Status,
		ProviderMessageID: record.ProviderMessageID, Error: record.Error,
	})
	if err != nil {
		return fmt.Errorf("record notification: %w", err)
	}
	return nil
}
