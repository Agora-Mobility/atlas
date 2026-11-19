package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type AccountRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IAccountRepository = (*AccountRepository)(nil)

func NewAccountRepository(pool *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{pool: pool}
}

func (r *AccountRepository) Preferences(ctx context.Context, userID uuid.UUID) (*entities.Preferences, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserPreferences(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get preferences: %w", err)
	}
	return &entities.Preferences{NotifyStatus: row.NotifyStatus, NotifyPromotions: row.NotifyPromotions, NotifySMS: row.NotifySms, ShareLocation: row.ShareLocation, Language: row.Language, Currency: row.Currency, NavigationApp: row.NavigationApp}, nil
}

func (r *AccountRepository) SavePreferences(ctx context.Context, userID uuid.UUID, p *entities.Preferences) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).UpdateUserPreferences(ctx, gen.UpdateUserPreferencesParams{ID: userID, NotifyStatus: p.NotifyStatus, NotifyPromotions: p.NotifyPromotions, NotifySms: p.NotifySMS, ShareLocation: p.ShareLocation, Language: p.Language, Currency: p.Currency, NavigationApp: p.NavigationApp})
	if err != nil {
		return false, fmt.Errorf("save preferences: %w", err)
	}
	return rows > 0, nil
}

func (r *AccountRepository) SetAvatar(ctx context.Context, userID, uploadID uuid.UUID) error {
	id := uploadID
	if err := postgres.Queries(ctx, r.pool).SetUserAvatar(ctx, gen.SetUserAvatarParams{ID: userID, AvatarUploadID: &id}); err != nil {
		return fmt.Errorf("set avatar: %w", err)
	}
	return nil
}

func (r *AccountRepository) Avatar(ctx context.Context, userID uuid.UUID) (string, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserAvatar(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get avatar: %w", err)
	}
	return row.ObjectKey, nil
}

func (r *AccountRepository) ReferralCode(ctx context.Context, userID uuid.UUID) (string, error) {
	code, err := postgres.Queries(ctx, r.pool).GetReferralCode(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get referral code: %w", err)
	}
	if code == nil {
		return "", nil
	}
	return *code, nil
}

func (r *AccountRepository) SetReferralCode(ctx context.Context, userID uuid.UUID, code string) error {
	value := code
	if err := postgres.Queries(ctx, r.pool).SetReferralCode(ctx, gen.SetReferralCodeParams{ID: userID, ReferralCode: &value}); err != nil {
		return fmt.Errorf("set referral code: %w", err)
	}
	return nil
}

func (r *AccountRepository) UserByReferralCode(ctx context.Context, code string) (*uuid.UUID, error) {
	value := code
	id, err := postgres.Queries(ctx, r.pool).FindUserByReferralCode(ctx, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find referral code: %w", err)
	}
	return &id, nil
}

func (r *AccountRepository) AddReferral(ctx context.Context, id, referrerID, referredID uuid.UUID, now time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).InsertReferral(ctx, gen.InsertReferralParams{ID: id, ReferrerID: referrerID, ReferredID: referredID, CreatedAt: now})
	if err != nil {
		return false, fmt.Errorf("add referral: %w", err)
	}
	return rows > 0, nil
}

func (r *AccountRepository) ClaimReward(ctx context.Context, referredID uuid.UUID, cents int64, now time.Time) (*uuid.UUID, *uuid.UUID, error) {
	row, err := postgres.Queries(ctx, r.pool).ClaimReferralReward(ctx, gen.ClaimReferralRewardParams{ReferredID: referredID, RewardCents: cents, RewardedAt: &now})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("claim referral: %w", err)
	}
	return &row.ID, &row.ReferrerID, nil
}

func (r *AccountRepository) ReferralStats(ctx context.Context, referrerID uuid.UUID) (*repository.ReferralStats, error) {
	row, err := postgres.Queries(ctx, r.pool).CountReferrals(ctx, referrerID)
	if err != nil {
		return nil, fmt.Errorf("count referrals: %w", err)
	}
	return &repository.ReferralStats{Total: row.Total, Rewarded: row.Rewarded, EarnedCents: row.EarnedCents}, nil
}

func (r *AccountRepository) HasPaidRequest(ctx context.Context, customerID uuid.UUID) (bool, error) {
	paid, err := postgres.Queries(ctx, r.pool).HasPaidRequest(ctx, customerID)
	if err != nil {
		return false, fmt.Errorf("has paid request: %w", err)
	}
	return paid, nil
}

func (r *AccountRepository) AddInboxItem(ctx context.Context, i *entities.InboxItem) error {
	err := postgres.Queries(ctx, r.pool).InsertInboxItem(ctx, gen.InsertInboxItemParams{ID: i.ID, UserID: i.UserID, Kind: i.Kind, Title: i.Title, Body: i.Body, LinkType: i.LinkType, LinkID: i.LinkID, EventID: i.EventID, CreatedAt: i.CreatedAt})
	if err != nil {
		return fmt.Errorf("add inbox item: %w", err)
	}
	return nil
}

func (r *AccountRepository) Inbox(ctx context.Context, userID uuid.UUID, onlyUnread bool, limit, offset int) ([]entities.InboxItem, int64, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListInboxItems(ctx, gen.ListInboxItemsParams{UserID: userID, OnlyUnread: onlyUnread, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, 0, fmt.Errorf("list inbox: %w", err)
	}
	counts, err := queries.CountInboxItems(ctx, userID)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("count inbox: %w", err)
	}
	out := make([]entities.InboxItem, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, entities.InboxItem{ID: row.ID, UserID: row.UserID, Kind: row.Kind, Title: row.Title, Body: row.Body, LinkType: row.LinkType, LinkID: row.LinkID, EventID: row.EventID, ReadAt: row.ReadAt, CreatedAt: row.CreatedAt})
	}
	return out, counts.Total, counts.Unread, nil
}

func (r *AccountRepository) MarkRead(ctx context.Context, userID, id uuid.UUID, at time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).MarkInboxItemRead(ctx, gen.MarkInboxItemReadParams{ID: id, UserID: userID, At: &at})
	if err != nil {
		return false, fmt.Errorf("mark read: %w", err)
	}
	return rows > 0, nil
}

func (r *AccountRepository) MarkAllRead(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error) {
	rows, err := postgres.Queries(ctx, r.pool).MarkAllInboxRead(ctx, gen.MarkAllInboxReadParams{UserID: userID, At: &at})
	if err != nil {
		return 0, fmt.Errorf("mark all read: %w", err)
	}
	return rows, nil
}

func (r *AccountRepository) HelpTopics(ctx context.Context, search string) ([]entities.CatalogItem, error) {
	rows, err := postgres.Queries(ctx, r.pool).SearchHelpTopics(ctx, search)
	if err != nil {
		return nil, fmt.Errorf("list help topics: %w", err)
	}
	out := make([]entities.CatalogItem, 0, len(rows))
	for i := range rows {
		out = append(out, entities.CatalogItem{ID: rows[i].ID, Kind: entities.CatalogKind(rows[i].Kind), Code: rows[i].Code, Label: rows[i].Label, Description: rows[i].Description, IsActive: rows[i].IsActive})
	}
	return out, nil
}

func (r *AccountRepository) RecentRequest(ctx context.Context, customerID uuid.UUID) (*repository.RecentRequest, error) {
	row, err := postgres.Queries(ctx, r.pool).RecentRequestOfUser(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recent request: %w", err)
	}
	return &repository.RecentRequest{ID: row.ID, Code: row.PublicCode, Category: row.CategorySlug, Status: row.Status}, nil
}
