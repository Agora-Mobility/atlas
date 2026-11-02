package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type WorkshopDirectory struct {
	pool *pgxpool.Pool
}

var _ repository.IWorkshopDirectory = (*WorkshopDirectory)(nil)

func NewWorkshopDirectory(pool *pgxpool.Pool) *WorkshopDirectory {
	return &WorkshopDirectory{pool: pool}
}

func (r *WorkshopDirectory) Nearest(ctx context.Context, point entities.GeoPoint) (*repository.NearbyWorkshop, error) {
	row, err := postgres.Queries(ctx, r.pool).NearestWorkshop(ctx, gen.NearestWorkshopParams{Lat: point.Lat, Lng: point.Lng})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("nearest workshop: %w", err)
	}
	return &repository.NearbyWorkshop{ID: row.ID, Name: row.Name, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Line: row.Line, DistanceM: row.DistanceM}, nil
}
