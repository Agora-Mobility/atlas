package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type CancellationReportFilter struct {
	Scope    entities.RejectionScope
	Category string
	From     *time.Time
	To       *time.Time
}

type CancellationReportRow struct {
	ReasonID uuid.UUID
	Code     string
	Label    string
	Category string
	Total    int64
	FeeCents int64
}

type ICancellationRepository interface {
	Rule(ctx context.Context, serviceID uuid.UUID) (*entities.CancellationRule, error)
	Rules(ctx context.Context) ([]entities.CancellationRule, error)
	SaveRule(ctx context.Context, rule *entities.CancellationRule) error
	ReasonsFor(ctx context.Context, scope entities.RejectionScope, categoryID uuid.UUID) ([]entities.RejectionReason, error)
	ActiveReason(ctx context.Context, id uuid.UUID, scope entities.RejectionScope) (*entities.RejectionReason, error)
	CountProviderCancellation(ctx context.Context, providerID uuid.UUID) error
	Report(ctx context.Context, filter CancellationReportFilter) ([]CancellationReportRow, error)
}
