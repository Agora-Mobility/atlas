package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type ActivityRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IActivityRepository = (*ActivityRepository)(nil)

func NewActivityRepository(pool *pgxpool.Pool) *ActivityRepository {
	return &ActivityRepository{pool: pool}
}

func (r *ActivityRepository) Activity(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]repository.AccountActivity, error) {
	out := make(map[uuid.UUID]repository.AccountActivity, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	queries := postgres.Queries(ctx, r.pool)
	counts, err := queries.AccountActivity(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("account activity: %w", err)
	}
	categories, err := queries.ProviderCategories(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("provider categories: %w", err)
	}
	ratings, err := queries.ProviderRatings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("provider ratings: %w", err)
	}
	for _, id := range ids {
		out[id] = repository.AccountActivity{}
	}
	for _, row := range counts {
		activity := out[row.UserID]
		last := row.LastRequestAt
		activity.CompletedRequests, activity.CancelledRequests, activity.LastRequestAt = row.CompletedRequests, row.CancelledRequests, &last
		out[row.UserID] = activity
	}
	for _, row := range categories {
		activity := out[row.UserID]
		activity.CategorySlug, activity.CategoryName = row.Slug, row.Name
		out[row.UserID] = activity
	}
	for _, row := range ratings {
		activity := out[row.UserID]
		activity.RatingSum, activity.RatingCount = row.RatingSum, int(row.RatingCount)
		out[row.UserID] = activity
	}
	return out, nil
}

func (r *ActivityRepository) Money(ctx context.Context, ids []uuid.UUID, from, to *time.Time) (map[uuid.UUID]repository.ProviderMoney, error) {
	out := make(map[uuid.UUID]repository.ProviderMoney, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	queries := postgres.Queries(ctx, r.pool)
	totals, err := queries.ProviderMoney(ctx, gen.ProviderMoneyParams{Ids: ids, FromAt: from, ToAt: to})
	if err != nil {
		return nil, fmt.Errorf("provider money: %w", err)
	}
	balances, err := queries.ProviderLedgerBalances(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("provider balances: %w", err)
	}
	for _, id := range ids {
		out[id] = repository.ProviderMoney{}
	}
	for _, row := range totals {
		money := out[row.ProviderID]
		money.GrossCents, money.EarnedCents = row.GrossCents, row.EarnedCents
		out[row.ProviderID] = money
	}
	for _, row := range balances {
		money := out[row.ProviderID]
		money.BalanceCents = row.BalanceCents
		out[row.ProviderID] = money
	}
	return out, nil
}
