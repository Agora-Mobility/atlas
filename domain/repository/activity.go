package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AccountActivity is what the backoffice shows of how much an account uses the
// platform: the requests it made (customer) or did (provider), the category of
// the provider and the rating it received.
type AccountActivity struct {
	CompletedRequests int64
	CancelledRequests int64
	LastRequestAt     *time.Time
	CategorySlug      string
	CategoryName      string
	RatingSum         int64
	RatingCount       int
}

// ProviderMoney is what the customers paid for the requests of a provider, what
// the provider earned from them and what is still to be settled.
type ProviderMoney struct {
	GrossCents   int64
	EarnedCents  int64
	BalanceCents int64
}

type IActivityRepository interface {
	// Activity has an entry for every id, also for accounts without requests.
	Activity(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]AccountActivity, error)
	// Money counts the movements in [from, to) when given; the balance is always
	// the current one. It has an entry for every id.
	Money(ctx context.Context, ids []uuid.UUID, from, to *time.Time) (map[uuid.UUID]ProviderMoney, error)
}
