package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ILedgerRepository = (*LedgerRepository)(nil)

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) Post(ctx context.Context, t *entities.LedgerTransaction) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	queries := postgres.Queries(ctx, r.pool)
	inserted, err := queries.InsertLedgerTransaction(ctx, gen.InsertLedgerTransactionParams{
		ID: t.ID, IdempotencyKey: t.IdempotencyKey, Kind: string(t.Kind), RequestID: t.RequestID, PaymentID: t.PaymentID, Note: t.Note, ActorID: t.ActorID, Currency: t.Currency, CreatedAt: t.CreatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("post ledger transaction: %w", err)
	}
	if inserted == 0 {
		return false, nil
	}
	for _, leg := range t.Legs {
		err := queries.InsertLedgerEntry(ctx, gen.InsertLedgerEntryParams{TransactionID: t.ID, AccountType: string(leg.AccountType), AccountID: leg.AccountID, AmountCents: leg.AmountCents, CreatedAt: t.CreatedAt})
		if err != nil {
			return false, fmt.Errorf("post ledger entry: %w", err)
		}
	}
	return true, nil
}

func (r *LedgerRepository) LockAccount(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).LockLedgerAccount(ctx, string(accountType)+id.String()); err != nil {
		return fmt.Errorf("lock ledger account: %w", err)
	}
	return nil
}

func (r *LedgerRepository) Balance(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID) (int64, error) {
	balance, err := postgres.Queries(ctx, r.pool).LedgerBalance(ctx, gen.LedgerBalanceParams{AccountType: string(accountType), AccountID: &id})
	if err != nil {
		return 0, fmt.Errorf("ledger balance: %w", err)
	}
	return balance, nil
}

func (r *LedgerRepository) Lines(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID, limit, offset int) ([]repository.LedgerLine, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListLedgerEntries(ctx, gen.ListLedgerEntriesParams{AccountType: string(accountType), AccountID: &id, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list ledger entries: %w", err)
	}
	total, err := queries.CountLedgerEntries(ctx, gen.CountLedgerEntriesParams{AccountType: string(accountType), AccountID: &id})
	if err != nil {
		return nil, 0, fmt.Errorf("count ledger entries: %w", err)
	}
	out := make([]repository.LedgerLine, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.LedgerLine{ID: row.ID, TransactionID: row.TransactionID, Kind: entities.LedgerKind(row.Kind), AmountCents: row.AmountCents, Currency: row.Currency, RequestID: row.RequestID, PaymentID: row.PaymentID, Note: row.Note, CreatedAt: row.CreatedAt})
	}
	return out, total, nil
}

func (r *LedgerRepository) Totals(ctx context.Context) ([]repository.LedgerTotal, error) {
	rows, err := postgres.Queries(ctx, r.pool).LedgerTotals(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger totals: %w", err)
	}
	out := make([]repository.LedgerTotal, 0, len(rows))
	for i := range rows {
		out = append(out, repository.LedgerTotal{AccountType: entities.LedgerAccountType(rows[i].AccountType), TotalCents: rows[i].TotalCents})
	}
	return out, nil
}

func (r *LedgerRepository) UnbalancedCount(ctx context.Context) (int64, error) {
	count, err := postgres.Queries(ctx, r.pool).UnbalancedLedgerTransactions(ctx)
	if err != nil {
		return 0, fmt.Errorf("unbalanced ledger transactions: %w", err)
	}
	return count, nil
}

func (r *LedgerRepository) ProviderBalances(ctx context.Context, limit, offset int) ([]repository.ProviderBalance, error) {
	rows, err := postgres.Queries(ctx, r.pool).LedgerProviderBalances(ctx, gen.LedgerProviderBalancesParams{PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, fmt.Errorf("provider balances: %w", err)
	}
	out := make([]repository.ProviderBalance, 0, len(rows))
	for i := range rows {
		if rows[i].AccountID != nil {
			out = append(out, repository.ProviderBalance{ProviderID: *rows[i].AccountID, Name: rows[i].Name, BalanceCents: rows[i].BalanceCents})
		}
	}
	return out, nil
}

func (r *LedgerRepository) SetGatewayFee(ctx context.Context, paymentID uuid.UUID, cents int64) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).SetPaymentGatewayFee(ctx, gen.SetPaymentGatewayFeeParams{ID: paymentID, GatewayFeeCents: &cents})
	if err != nil {
		return false, fmt.Errorf("set gateway fee: %w", err)
	}
	return rows > 0, nil
}
