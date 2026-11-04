package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type RemovalRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRemovalRepository = (*RemovalRepository)(nil)

func NewRemovalRepository(pool *pgxpool.Pool) *RemovalRepository {
	return &RemovalRepository{pool: pool}
}

func (r *RemovalRepository) ReplaceTeam(ctx context.Context, requestID uuid.UUID, members []entities.TeamMember) error {
	queries := postgres.Queries(ctx, r.pool)
	if err := queries.DeleteRequestTeam(ctx, requestID); err != nil {
		return fmt.Errorf("clear team: %w", err)
	}
	for i := range members {
		member := &members[i]
		err := queries.InsertRequestTeamMember(ctx, gen.InsertRequestTeamMemberParams{ID: member.ID, RequestID: requestID, Name: member.Name, Role: member.Role, Verified: member.Verified, CreatedAt: member.CreatedAt})
		if err != nil {
			return fmt.Errorf("save team member: %w", err)
		}
	}
	return nil
}

func (r *RemovalRepository) Team(ctx context.Context, requestID uuid.UUID) ([]entities.TeamMember, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRequestTeam(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list team: %w", err)
	}
	out := make([]entities.TeamMember, 0, len(rows))
	for i := range rows {
		out = append(out, entities.TeamMember{ID: rows[i].ID, RequestID: requestID, Name: rows[i].Name, Role: rows[i].Role, Verified: rows[i].Verified, CreatedAt: rows[i].CreatedAt})
	}
	return out, nil
}

func (r *RemovalRepository) LockVehicle(ctx context.Context, vehicleID uuid.UUID) error {
	if _, err := postgres.Queries(ctx, r.pool).LockCatalogItem(ctx, vehicleID); err != nil {
		return fmt.Errorf("lock vehicle type: %w", err)
	}
	return nil
}

func (r *RemovalRepository) CountOfDay(ctx context.Context, vehicleID uuid.UUID, day time.Time) (int64, error) {
	id := vehicleID
	total, err := postgres.Queries(ctx, r.pool).CountRemovalsOfDay(ctx, gen.CountRemovalsOfDayParams{VehicleTypeID: &id, Day: day})
	if err != nil {
		return 0, fmt.Errorf("count removals: %w", err)
	}
	return total, nil
}
