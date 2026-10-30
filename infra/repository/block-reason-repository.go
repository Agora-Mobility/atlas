package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type BlockReasonRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IBlockReasonRepository = (*BlockReasonRepository)(nil)

func NewBlockReasonRepository(pool *pgxpool.Pool) *BlockReasonRepository {
	return &BlockReasonRepository{pool: pool}
}

func (r *BlockReasonRepository) ListActive(ctx context.Context) ([]*entities.BlockReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListActiveBlockReasons(ctx)
	if err != nil {
		return nil, fmt.Errorf("list block reasons: %w", err)
	}
	return toBlockReasons(rows), nil
}

func (r *BlockReasonRepository) ListActiveByIDs(ctx context.Context, ids []uuid.UUID) ([]*entities.BlockReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListActiveBlockReasonsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list block reasons by id: %w", err)
	}
	return toBlockReasons(rows), nil
}

func (r *BlockReasonRepository) RecordBlock(ctx context.Context, block *entities.UserBlock) error {
	q := postgres.Queries(ctx, r.pool)
	err := q.CreateUserBlock(ctx, gen.CreateUserBlockParams{
		ID:        block.ID,
		UserID:    block.UserID,
		BlockedBy: block.BlockedBy,
		Note:      block.Note,
		BlockedAt: block.BlockedAt,
	})
	if err != nil {
		return fmt.Errorf("insert user block: %w", err)
	}
	err = q.CreateUserBlockReasons(ctx, gen.CreateUserBlockReasonsParams{
		UserBlockID:    block.ID,
		BlockReasonIds: block.ReasonIDs(),
	})
	if err != nil {
		return fmt.Errorf("insert user block reasons: %w", err)
	}
	return nil
}

func toBlockReasons(rows []gen.BlockReason) []*entities.BlockReason {
	reasons := make([]*entities.BlockReason, 0, len(rows))
	for _, row := range rows {
		reasons = append(reasons, &entities.BlockReason{
			ID:        row.ID,
			Code:      row.Code,
			Label:     row.Label,
			IsActive:  row.IsActive,
			CreatedAt: row.CreatedAt.UTC(),
			UpdatedAt: row.UpdatedAt.UTC(),
		})
	}
	return reasons
}
