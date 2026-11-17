package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type AuditLogRepository struct {
	pool *pgxpool.Pool
}

var (
	_ repository.IAuditLogRepository = (*AuditLogRepository)(nil)
	_ repository.IAuditLogReader     = (*AuditLogRepository)(nil)
)

func NewAuditLogRepository(pool *pgxpool.Pool) *AuditLogRepository {
	return &AuditLogRepository{pool: pool}
}

func (r *AuditLogRepository) Record(ctx context.Context, e *entities.AuditEntry) error {
	var actorRole *enums.Role
	if e.Actor.Role != "" {
		actorRole = &e.Actor.Role
	}
	err := postgres.Queries(ctx, r.pool).CreateAuditLog(ctx, gen.CreateAuditLogParams{
		ID:             e.ID,
		ActorID:        e.Actor.ID,
		ActorRole:      actorRole,
		OrganizationID: e.OrganizationID,
		Action:         e.Action,
		EntityType:     e.EntityType,
		EntityID:       e.EntityID,
		Before:         e.Before,
		After:          e.After,
		Reason:         e.Reason,
		CorrelationID:  e.CorrelationID,
		IpAddress:      e.Actor.IPAddress,
		UserAgent:      e.Actor.UserAgent,
		CreatedAt:      e.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func (r *AuditLogRepository) List(ctx context.Context, f repository.AuditFilter) ([]repository.AuditRecord, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListAuditLog(ctx, gen.ListAuditLogParams{
		ActorID: f.ActorID, EntityType: f.EntityType, EntityID: f.EntityID, Action: f.Action, CreatedFrom: f.From, CreatedTo: f.To,
		PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list audit log: %w", err)
	}
	total, err := queries.CountAuditLog(ctx, gen.CountAuditLogParams{
		ActorID: f.ActorID, EntityType: f.EntityType, EntityID: f.EntityID, Action: f.Action, CreatedFrom: f.From, CreatedTo: f.To,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count audit log: %w", err)
	}
	out := make([]repository.AuditRecord, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		record := repository.AuditRecord{
			ID: row.ID, ActorID: row.ActorID, OrganizationID: row.OrganizationID, Action: row.Action, EntityType: row.EntityType, EntityID: row.EntityID,
			Before: row.Before, After: row.After, Reason: row.Reason, CorrelationID: row.CorrelationID, CreatedAt: row.CreatedAt,
		}
		if row.ActorRole != nil {
			record.ActorRole = string(*row.ActorRole)
		}
		if row.ActorName != nil {
			record.ActorName = *row.ActorName
		}
		out = append(out, record)
	}
	return out, total, nil
}
