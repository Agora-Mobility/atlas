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

func (r *ProviderRepository) Create(ctx context.Context, userID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).CreateProvider(ctx, userID); err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	return nil
}

func (r *ProviderRepository) Find(ctx context.Context, userID uuid.UUID) (*entities.Provider, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetProvider(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	provider := &entities.Provider{
		UserID: row.UserID, Status: entities.ProviderStatus(row.Status), OrganizationID: row.OrganizationID, ServiceID: row.ServiceID, RequestedServiceID: row.RequestedServiceID,
		BirthDate: row.BirthDate, IDNumber: row.IDNumber, Province: row.Province, Municipality: row.Municipality, StatusReason: row.StatusReason, SubmittedAt: row.SubmittedAt, ReviewedAt: row.ReviewedAt,
		ServiceRadiusKm: row.ServiceRadiusKm, LastJobCompletedAt: row.LastJobCompletedAt, CreatedAt: row.CreatedAt, RatingSum: row.RatingSum, RatingCount: int(row.RatingCount), CompletedRequests: int(row.CompletedRequests),
	}
	if row.BaseAddressID != nil {
		base, err := findAddress(ctx, queries, *row.BaseAddressID)
		if err != nil {
			return nil, err
		}
		if base != nil {
			provider.Base, provider.BaseAddressID = &base.Point, &base.ID
		}
	}
	vehicle, err := queries.GetVehicleOfProvider(ctx, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get vehicle: %w", err)
	}
	provider.Vehicle = vehicleFromRow(vehicle.Kind, vehicle.Brand, vehicle.Model, vehicle.Color, vehicle.Plate, vehicle.Year, vehicle.Seats, vehicle.CapacityKg)
	return provider, nil
}

func (r *ProviderRepository) Save(ctx context.Context, p *entities.Provider) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		err := queries.UpsertProvider(ctx, gen.UpsertProviderParams{
			UserID: p.UserID, Status: string(p.Status), ServiceID: p.ServiceID, RequestedServiceID: p.RequestedServiceID, BirthDate: p.BirthDate,
			IDNumber: p.IDNumber, Province: p.Province, Municipality: p.Municipality, StatusReason: p.StatusReason, SubmittedAt: p.SubmittedAt, ReviewedAt: p.ReviewedAt, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return fmt.Errorf("save provider: %w", err)
		}
		if p.Vehicle.Plate == "" {
			return nil
		}
		year, seats := int32(p.Vehicle.Year), int32(p.Vehicle.Seats) // #nosec G115 -- validated by the entity
		err = queries.UpsertVehicle(ctx, gen.UpsertVehicleParams{
			ProviderID: &p.UserID, Kind: p.Vehicle.Kind, Brand: p.Vehicle.Brand, Model: p.Vehicle.Model, Year: &year, Color: p.Vehicle.Color, Plate: p.Vehicle.Plate, Seats: &seats, CapacityKg: toInt32Pointer(p.Vehicle.CapacityKg),
		})
		if err != nil {
			return fmt.Errorf("save vehicle: %w", err)
		}
		return nil
	})
}

func (r *ProviderRepository) Applications(ctx context.Context, f repository.ProviderApplicationFilter) ([]repository.ProviderApplication, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	search := likePattern(f.Search)
	rows, err := queries.ListProviderApplications(ctx, gen.ListProviderApplicationsParams{
		Status: f.Status, OrganizationID: f.OrganizationID, ServiceID: f.ServiceID, Category: f.Category, Search: search, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list provider applications: %w", err)
	}
	total, err := queries.CountProviderApplications(ctx, gen.CountProviderApplicationsParams{Status: f.Status, OrganizationID: f.OrganizationID, ServiceID: f.ServiceID, Category: f.Category, Search: search})
	if err != nil {
		return nil, 0, fmt.Errorf("count provider applications: %w", err)
	}
	out := make([]repository.ProviderApplication, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		application := repository.ProviderApplication{
			UserID: row.UserID, Name: row.Name, Status: entities.ProviderStatus(row.Status), RequestedServiceID: row.RequestedServiceID, ServiceID: row.ServiceID,
			OrganizationID: row.OrganizationID, SubmittedAt: row.SubmittedAt,
		}
		if row.Phone != nil {
			application.Phone = *row.Phone
		}
		if row.RequestedServiceName != nil {
			application.RequestedServiceName = *row.RequestedServiceName
		}
		if row.VehiclePlate != nil {
			application.VehiclePlate = *row.VehiclePlate
		}
		if row.OrganizationName != nil {
			application.OrganizationName = *row.OrganizationName
		}
		out = append(out, application)
	}
	return out, total, nil
}

func (r *ProviderRepository) CategoryOf(ctx context.Context, userID uuid.UUID) (*entities.LiveIndex, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderCategory(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider category: %w", err)
	}
	return &entities.LiveIndex{CategoryID: row.ID, Slug: row.Slug}, nil
}

func (r *ProviderRepository) ClearCoverage(ctx context.Context, providerID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).DeleteProviderCoverage(ctx, providerID); err != nil {
		return fmt.Errorf("clear coverage: %w", err)
	}
	return nil
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
		capacity := int(row.CapacityKg)
		provider.CapacityKg = &capacity
		if row.CapacityKg == 0 {
			provider.CapacityKg = nil
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
		current, err := queries.GetProvider(ctx, providerID)
		if err != nil {
			return fmt.Errorf("get provider: %w", err)
		}
		addressID := uuid.Nil
		if current.BaseAddressID != nil {
			addressID = *current.BaseAddressID
		}
		address, err := entities.Address{ID: addressID, Point: base}.Normalized("base")
		if err != nil {
			return err
		}
		if err := saveAddress(ctx, queries, &address); err != nil {
			return err
		}
		rows, err := queries.UpdateProviderBase(ctx, gen.UpdateProviderBaseParams{UserID: providerID, BaseAddressID: &address.ID, ServiceRadiusKm: &radiusKm})
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
