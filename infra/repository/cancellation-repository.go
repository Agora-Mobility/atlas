package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type CancellationRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ICancellationRepository = (*CancellationRepository)(nil)

func NewCancellationRepository(pool *pgxpool.Pool) *CancellationRepository {
	return &CancellationRepository{pool: pool}
}

func (r *CancellationRepository) Rule(ctx context.Context, serviceID uuid.UUID) (*entities.CancellationRule, error) {
	row, err := postgres.Queries(ctx, r.pool).GetCancellationRule(ctx, serviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cancellation rule: %w", err)
	}
	return &entities.CancellationRule{
		ServiceID: row.ServiceID, FeeCents: row.FeeCents, FreeAfterAssignSeconds: int(row.FreeAfterAssignSeconds), ScheduledFreeSeconds: int(row.ScheduledFreeSeconds), ChargeWithoutProvider: row.ChargeWithoutProvider,
	}, nil
}

func (r *CancellationRepository) Rules(ctx context.Context) ([]entities.CancellationRule, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListCancellationRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cancellation rules: %w", err)
	}
	out := make([]entities.CancellationRule, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, entities.CancellationRule{
			ServiceID: row.ServiceID, ServiceSlug: row.ServiceSlug, ServiceName: row.ServiceName, FeeCents: row.FeeCents, FreeAfterAssignSeconds: int(row.FreeAfterAssignSeconds),
			ScheduledFreeSeconds: int(row.ScheduledFreeSeconds), ChargeWithoutProvider: row.ChargeWithoutProvider,
		})
	}
	return out, nil
}

func (r *CancellationRepository) SaveRule(ctx context.Context, rule *entities.CancellationRule) error {
	err := postgres.Queries(ctx, r.pool).UpsertCancellationRule(ctx, gen.UpsertCancellationRuleParams{
		ServiceID: rule.ServiceID, FeeCents: rule.FeeCents, FreeAfterAssignSeconds: int32(rule.FreeAfterAssignSeconds), // #nosec G115 -- validated by the entity
		ScheduledFreeSeconds: int32(rule.ScheduledFreeSeconds), ChargeWithoutProvider: rule.ChargeWithoutProvider, // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("save cancellation rule: %w", err)
	}
	return nil
}

func (r *CancellationRepository) ReasonsFor(ctx context.Context, scope entities.RejectionScope, categoryID uuid.UUID) ([]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListReasonsForCategory(ctx, gen.ListReasonsForCategoryParams{Scope: string(scope), CategoryID: categoryID})
	if err != nil {
		return nil, fmt.Errorf("list reasons: %w", err)
	}
	out := make([]entities.RejectionReason, 0, len(rows))
	for i := range rows {
		out = append(out, toRejectionReason(&rows[i]))
	}
	return out, nil
}

func (r *CancellationRepository) ActiveReason(ctx context.Context, id uuid.UUID, scope entities.RejectionScope) (*entities.RejectionReason, error) {
	row, err := postgres.Queries(ctx, r.pool).GetActiveReason(ctx, gen.GetActiveReasonParams{ID: id, Scope: string(scope)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get reason: %w", err)
	}
	reason := toRejectionReason(&row)
	return &reason, nil
}

func (r *CancellationRepository) CountProviderCancellation(ctx context.Context, providerID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).IncrementProviderCancellations(ctx, providerID); err != nil {
		return fmt.Errorf("count provider cancellation: %w", err)
	}
	return nil
}

func (r *CancellationRepository) Report(ctx context.Context, f repository.CancellationReportFilter) ([]repository.CancellationReportRow, error) {
	queries := postgres.Queries(ctx, r.pool)
	if f.Scope == entities.RejectionScopeOfferReject {
		rows, err := queries.OfferRejectionReport(ctx, gen.OfferRejectionReportParams{Category: f.Category, CreatedFrom: f.From, CreatedTo: f.To})
		if err != nil {
			return nil, fmt.Errorf("offer rejection report: %w", err)
		}
		out := make([]repository.CancellationReportRow, 0, len(rows))
		for i := range rows {
			out = append(out, repository.CancellationReportRow{ReasonID: rows[i].ReasonID, Code: rows[i].Code, Label: rows[i].Label, Category: rows[i].Category, Total: rows[i].Total})
		}
		return out, nil
	}
	rows, err := queries.CancellationReport(ctx, gen.CancellationReportParams{Scope: string(f.Scope), Category: f.Category, CreatedFrom: f.From, CreatedTo: f.To})
	if err != nil {
		return nil, fmt.Errorf("cancellation report: %w", err)
	}
	out := make([]repository.CancellationReportRow, 0, len(rows))
	for i := range rows {
		out = append(out, repository.CancellationReportRow{ReasonID: rows[i].ReasonID, Code: rows[i].Code, Label: rows[i].Label, Category: rows[i].Category, Total: rows[i].Total, FeeCents: rows[i].FeeCents})
	}
	return out, nil
}
