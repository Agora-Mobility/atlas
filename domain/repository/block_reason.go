package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type IBlockReasonRepository interface {
	ListActive(ctx context.Context) ([]*entities.BlockReason, error)
	ListActiveByIDs(ctx context.Context, ids []uuid.UUID) ([]*entities.BlockReason, error)
	RecordBlock(ctx context.Context, block *entities.UserBlock) error
}
