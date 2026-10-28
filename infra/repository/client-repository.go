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
)

type ClientRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IClientRepository = (*ClientRepository)(nil)

func NewClientRepository(pool *pgxpool.Pool) *ClientRepository {
	return &ClientRepository{pool: pool}
}

func (r *ClientRepository) FindActiveByClientID(ctx context.Context, clientID string) (*entities.Client, error) {
	row, err := postgres.Queries(ctx, r.pool).GetActiveClientByClientID(ctx, clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	return &entities.Client{
		ID:        row.ID,
		ClientID:  row.ClientID,
		Name:      row.Name,
		Type:      row.Type,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}, nil
}
