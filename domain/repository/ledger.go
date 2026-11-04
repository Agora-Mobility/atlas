package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type LedgerLine struct {
	ID            int64
	TransactionID uuid.UUID
	Kind          entities.LedgerKind
	AmountCents   int64
	Currency      string
	RequestID     *uuid.UUID
	PaymentID     *uuid.UUID
	Note          string
	CreatedAt     time.Time
}

type LedgerTotal struct {
	AccountType entities.LedgerAccountType
	TotalCents  int64
}

type ProviderBalance struct {
	ProviderID   uuid.UUID
	Name         string
	BalanceCents int64
}

type ILedgerRepository interface {
	Post(ctx context.Context, transaction *entities.LedgerTransaction) (bool, error)
	LockAccount(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID) error
	Balance(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID) (int64, error)
	Lines(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID, limit, offset int) ([]LedgerLine, int64, error)
	Totals(ctx context.Context) ([]LedgerTotal, error)
	UnbalancedCount(ctx context.Context) (int64, error)
	ProviderBalances(ctx context.Context, limit, offset int) ([]ProviderBalance, error)
	SetGatewayFee(ctx context.Context, paymentID uuid.UUID, cents int64) (bool, error)
}
