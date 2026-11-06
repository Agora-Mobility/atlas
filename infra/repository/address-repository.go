package repository

import (
	"context"
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

type AddressRepository struct {
	pool *pgxpool.Pool
	tx   *postgres.TxManager
}

var _ repository.IAddressRepository = (*AddressRepository)(nil)

func NewAddressRepository(pool *pgxpool.Pool) *AddressRepository {
	return &AddressRepository{pool: pool, tx: postgres.NewTxManager(pool)}
}

func saveAddress(ctx context.Context, queries *gen.Queries, a *entities.Address) error {
	err := queries.SaveAddress(ctx, gen.SaveAddressParams{
		ID: a.ID, Lat: a.Point.Lat, Lng: a.Point.Lng, Line: a.Line, Reference: a.Reference, Neighborhood: a.Neighborhood,
		City: a.City, Province: a.Province, CountryCode: a.CountryCode,
	})
	if err != nil {
		return fmt.Errorf("save address: %w", err)
	}
	return nil
}

func saveAddressOfPlace(ctx context.Context, queries *gen.Queries, place *entities.RequestPlace) error {
	return saveAddress(ctx, queries, &entities.Address{ID: place.AddressID, Point: place.Point, Line: place.Address, Reference: place.Reference})
}

func findAddress(ctx context.Context, queries *gen.Queries, id uuid.UUID) (*entities.Address, error) {
	row, err := queries.GetAddress(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get address: %w", err)
	}
	return &entities.Address{
		ID: row.ID, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Line: row.Line, Reference: row.Reference, Neighborhood: row.Neighborhood,
		City: row.City, Province: row.Province, CountryCode: row.CountryCode,
	}, nil
}

func (r *AddressRepository) ListOfUser(ctx context.Context, userID uuid.UUID) ([]entities.UserAddress, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListUserAddresses(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list addresses: %w", err)
	}
	out := make([]entities.UserAddress, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, entities.UserAddress{
			Address: entities.Address{
				ID: row.ID, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Line: row.Line, Reference: row.Reference, Neighborhood: row.Neighborhood,
				City: row.City, Province: row.Province, CountryCode: row.CountryCode,
			},
			Kind: entities.AddressKind(row.Kind), Label: row.Label,
		})
	}
	return out, nil
}

func (r *AddressRepository) FindOfUser(ctx context.Context, userID, addressID uuid.UUID) (*entities.UserAddress, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserAddress(ctx, gen.GetUserAddressParams{UserID: userID, AddressID: addressID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user address: %w", err)
	}
	return &entities.UserAddress{
		Address: entities.Address{
			ID: row.ID, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Line: row.Line, Reference: row.Reference, Neighborhood: row.Neighborhood,
			City: row.City, Province: row.Province, CountryCode: row.CountryCode,
		},
		Kind: entities.AddressKind(row.Kind), Label: row.Label,
	}, nil
}

func (r *AddressRepository) SaveForUser(ctx context.Context, userID uuid.UUID, address entities.UserAddress) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		if err := saveAddress(ctx, queries, &address.Address); err != nil {
			return err
		}
		err := queries.SaveUserAddress(ctx, gen.SaveUserAddressParams{AddressID: address.ID, UserID: userID, Kind: string(address.Kind), Label: address.Label})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return entities.ErrAddressKindTaken
		}
		if err != nil {
			return fmt.Errorf("save user address: %w", err)
		}
		return nil
	})
}

func (r *AddressRepository) Delete(ctx context.Context, addressID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).DeleteAddress(ctx, addressID); err != nil {
		return fmt.Errorf("delete address: %w", err)
	}
	return nil
}
