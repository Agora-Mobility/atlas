package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type IDemandRepository interface {
	ListDemandPricing(ctx context.Context) ([]*entities.DemandPricing, error)
	FindDemandPricing(ctx context.Context, zoneID, categoryID uuid.UUID) (*entities.DemandPricing, error)
	SaveDemandPricing(ctx context.Context, pricing *entities.DemandPricing) error
	PendingCustomers(ctx context.Context, zoneID, categoryID uuid.UUID) (map[int64][]uuid.UUID, error)
	SaveSnapshots(ctx context.Context, snapshots []entities.DemandSnapshot) error
	ListSnapshots(ctx context.Context, zoneID, categoryID uuid.UUID, since time.Time, limit int) ([]entities.DemandSnapshot, error)
	PurgeSnapshots(ctx context.Context, before time.Time) (int64, error)
}

type DemandKey struct {
	ZoneID   uuid.UUID
	Category string
}

type IDemandSignals interface {
	ClaimRefresh(ctx context.Context, every time.Duration) (bool, error)
	RecordQuote(ctx context.Context, key DemandKey, area int64, customerID uuid.UUID, at time.Time) error
	RecentQuotes(ctx context.Context, key DemandKey, since, until time.Time) (map[int64][]uuid.UUID, error)
	FreeProviders(ctx context.Context, category string, resolution int, cells []int64) (int, error)
	Ratios(ctx context.Context, key DemandKey) (map[int64]int, error)
	Publish(ctx context.Context, key DemandKey, ratios, factors map[int64]int, factorTTL time.Duration) error
	Factor(ctx context.Context, key DemandKey, area int64) (int, error)
	Factors(ctx context.Context, key DemandKey) (map[int64]int, error)
}
