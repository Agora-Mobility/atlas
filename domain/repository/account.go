package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type RecentRequest struct {
	ID       uuid.UUID
	Code     string
	Category string
	Status   string
}

type ReferralStats struct {
	Total       int64
	Rewarded    int64
	EarnedCents int64
}

type IAccountRepository interface {
	Preferences(ctx context.Context, userID uuid.UUID) (*entities.Preferences, error)
	SavePreferences(ctx context.Context, userID uuid.UUID, preferences *entities.Preferences) (bool, error)
	SetAvatar(ctx context.Context, userID, uploadID uuid.UUID) error
	Avatar(ctx context.Context, userID uuid.UUID) (string, error)
	ReferralCode(ctx context.Context, userID uuid.UUID) (string, error)
	SetReferralCode(ctx context.Context, userID uuid.UUID, code string) error
	UserByReferralCode(ctx context.Context, code string) (*uuid.UUID, error)
	AddReferral(ctx context.Context, id, referrerID, referredID uuid.UUID, now time.Time) (bool, error)
	ClaimReward(ctx context.Context, referredID uuid.UUID, cents int64, now time.Time) (*uuid.UUID, *uuid.UUID, error)
	ReferralStats(ctx context.Context, referrerID uuid.UUID) (*ReferralStats, error)
	HasPaidRequest(ctx context.Context, customerID uuid.UUID) (bool, error)
	AddInboxItem(ctx context.Context, item *entities.InboxItem) error
	Inbox(ctx context.Context, userID uuid.UUID, onlyUnread bool, limit, offset int) ([]entities.InboxItem, int64, int64, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID, at time.Time) (bool, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error)
	HelpTopics(ctx context.Context, search string) ([]entities.CatalogItem, error)
	RecentRequest(ctx context.Context, customerID uuid.UUID) (*RecentRequest, error)
}
