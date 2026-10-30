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

var _ repository.IAuditLogRepository = (*AuditLogRepository)(nil)

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
