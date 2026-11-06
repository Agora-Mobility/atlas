package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type GarageRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IGarageRepository = (*GarageRepository)(nil)

func NewGarageRepository(pool *pgxpool.Pool) *GarageRepository {
	return &GarageRepository{pool: pool}
}

func plateConflict(err error, action string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return repository.ErrCustomerPlateTaken
	}
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (r *GarageRepository) CreateVehicle(ctx context.Context, v *entities.CustomerVehicle) error {
	err := postgres.Queries(ctx, r.pool).InsertCustomerVehicle(ctx, gen.InsertCustomerVehicleParams{ID: v.ID, CustomerID: v.CustomerID, Brand: v.Brand, Model: v.Model, Plate: v.Plate, Year: int32(v.Year), Fuel: v.Fuel, CreatedAt: v.CreatedAt}) // #nosec G115 -- validated by the entity
	return plateConflict(err, "create customer vehicle")
}

func (r *GarageRepository) UpdateVehicle(ctx context.Context, v *entities.CustomerVehicle, now time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).UpdateCustomerVehicle(ctx, gen.UpdateCustomerVehicleParams{ID: v.ID, CustomerID: v.CustomerID, Brand: v.Brand, Model: v.Model, Plate: v.Plate, Year: int32(v.Year), Fuel: v.Fuel, UpdatedAt: now}) // #nosec G115 -- validated by the entity
	if err := plateConflict(err, "update customer vehicle"); err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *GarageRepository) DeactivateVehicle(ctx context.Context, id, customerID uuid.UUID, now time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).DeactivateCustomerVehicle(ctx, gen.DeactivateCustomerVehicleParams{ID: id, CustomerID: customerID, UpdatedAt: now})
	if err != nil {
		return false, fmt.Errorf("deactivate customer vehicle: %w", err)
	}
	return rows > 0, nil
}

func toCustomerVehicle(row *gen.CustomerVehicle) entities.CustomerVehicle {
	return entities.CustomerVehicle{ID: row.ID, CustomerID: row.CustomerID, Brand: row.Brand, Model: row.Model, Plate: row.Plate, Year: int(row.Year), Fuel: row.Fuel, CreatedAt: row.CreatedAt}
}

func (r *GarageRepository) Vehicle(ctx context.Context, id, customerID uuid.UUID) (*entities.CustomerVehicle, error) {
	row, err := postgres.Queries(ctx, r.pool).GetCustomerVehicle(ctx, gen.GetCustomerVehicleParams{ID: id, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get customer vehicle: %w", err)
	}
	vehicle := toCustomerVehicle(&row)
	return &vehicle, nil
}

func (r *GarageRepository) Vehicles(ctx context.Context, customerID uuid.UUID) ([]entities.CustomerVehicle, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListCustomerVehicles(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("list customer vehicles: %w", err)
	}
	out := make([]entities.CustomerVehicle, 0, len(rows))
	for i := range rows {
		out = append(out, toCustomerVehicle(&rows[i]))
	}
	return out, nil
}

func toUpload(row *gen.MediaUpload) entities.MediaUpload {
	return entities.MediaUpload{ID: row.ID, OwnerID: row.OwnerID, ObjectKey: row.ObjectKey, ContentType: row.ContentType, SizeBytes: row.SizeBytes, RequestID: row.RequestID, CreatedAt: row.CreatedAt}
}

func (r *GarageRepository) CreateUpload(ctx context.Context, u *entities.MediaUpload) error {
	err := postgres.Queries(ctx, r.pool).InsertMediaUpload(ctx, gen.InsertMediaUploadParams{ID: u.ID, OwnerID: u.OwnerID, ObjectKey: u.ObjectKey, ContentType: u.ContentType, SizeBytes: u.SizeBytes, CreatedAt: u.CreatedAt})
	if err != nil {
		return fmt.Errorf("create upload: %w", err)
	}
	return nil
}

func (r *GarageRepository) Uploads(ctx context.Context, ids []uuid.UUID) ([]entities.MediaUpload, error) {
	rows, err := postgres.Queries(ctx, r.pool).GetMediaUploads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get uploads: %w", err)
	}
	out := make([]entities.MediaUpload, 0, len(rows))
	for i := range rows {
		out = append(out, toUpload(&rows[i]))
	}
	return out, nil
}

func (r *GarageRepository) Upload(ctx context.Context, id uuid.UUID) (*entities.MediaUpload, error) {
	row, err := postgres.Queries(ctx, r.pool).GetMediaUpload(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get upload: %w", err)
	}
	upload := toUpload(&row)
	return &upload, nil
}

func (r *GarageRepository) AttachToRequest(ctx context.Context, ids []uuid.UUID, ownerID, requestID uuid.UUID) (int64, error) {
	rows, err := postgres.Queries(ctx, r.pool).AttachMediaToRequest(ctx, gen.AttachMediaToRequestParams{RequestID: &requestID, Ids: ids, OwnerID: ownerID})
	if err != nil {
		return 0, fmt.Errorf("attach uploads: %w", err)
	}
	return rows, nil
}

func (r *GarageRepository) OfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.MediaUpload, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListMediaOfRequest(ctx, &requestID)
	if err != nil {
		return nil, fmt.Errorf("list request uploads: %w", err)
	}
	out := make([]entities.MediaUpload, 0, len(rows))
	for i := range rows {
		out = append(out, toUpload(&rows[i]))
	}
	return out, nil
}
