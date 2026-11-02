package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type VoucherRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IVoucherRepository = (*VoucherRepository)(nil)

func NewVoucherRepository(pool *pgxpool.Pool) *VoucherRepository {
	return &VoucherRepository{pool: pool}
}

func toVoucher(row *gen.Voucher) *entities.Voucher {
	voucher := &entities.Voucher{
		ID: row.ID, Code: row.Code, Description: row.Description, CategoryID: row.CategoryID, ServiceID: row.ServiceID, DiscountBps: int(row.DiscountBps), DiscountCents: row.DiscountCents,
		MaxDiscountCents: row.MaxDiscountCents, MinFareCents: row.MinFareCents, ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, PerCustomerLimit: int(row.PerCustomerLimit), IsPublic: row.IsPublic, IsActive: row.IsActive,
	}
	if row.MaxRedemptions != nil {
		value := int(*row.MaxRedemptions)
		voucher.MaxRedemptions = &value
	}
	return voucher
}

func voucherOrNil(row *gen.Voucher, err error, action string) (*entities.Voucher, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return toVoucher(row), nil
}

func (r *VoucherRepository) Find(ctx context.Context, id uuid.UUID) (*entities.Voucher, error) {
	row, err := postgres.Queries(ctx, r.pool).GetVoucher(ctx, id)
	return voucherOrNil(&row, err, "get voucher")
}

func (r *VoucherRepository) FindByCode(ctx context.Context, code string) (*entities.Voucher, error) {
	row, err := postgres.Queries(ctx, r.pool).GetVoucherByCode(ctx, code)
	return voucherOrNil(&row, err, "get voucher by code")
}

func (r *VoucherRepository) LockByCode(ctx context.Context, code string) (*entities.Voucher, error) {
	row, err := postgres.Queries(ctx, r.pool).GetVoucherByCodeForUpdate(ctx, code)
	return voucherOrNil(&row, err, "lock voucher")
}

func (r *VoucherRepository) List(ctx context.Context, limit, offset int) ([]entities.Voucher, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListVouchers(ctx, gen.ListVouchersParams{PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list vouchers: %w", err)
	}
	total, err := queries.CountVouchers(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count vouchers: %w", err)
	}
	out := make([]entities.Voucher, 0, len(rows))
	for i := range rows {
		out = append(out, *toVoucher(&rows[i]))
	}
	return out, total, nil
}

func (r *VoucherRepository) Public(ctx context.Context, category string, now time.Time) ([]entities.Voucher, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListPublicVouchers(ctx, gen.ListPublicVouchersParams{Now: now, Category: category})
	if err != nil {
		return nil, fmt.Errorf("list public vouchers: %w", err)
	}
	out := make([]entities.Voucher, 0, len(rows))
	for i := range rows {
		out = append(out, *toVoucher(&rows[i]))
	}
	return out, nil
}

func maxRedemptions(voucher *entities.Voucher) *int32 {
	if voucher.MaxRedemptions == nil {
		return nil
	}
	value := int32(*voucher.MaxRedemptions) // #nosec G115 -- validated by the entity
	return &value
}

func (r *VoucherRepository) Create(ctx context.Context, v *entities.Voucher) error {
	err := postgres.Queries(ctx, r.pool).InsertVoucher(ctx, gen.InsertVoucherParams{
		ID: v.ID, Code: v.Code, Description: v.Description, CategoryID: v.CategoryID, ServiceID: v.ServiceID, DiscountBps: int32(v.DiscountBps), DiscountCents: v.DiscountCents, // #nosec G115 -- validated by the entity
		MaxDiscountCents: v.MaxDiscountCents, MinFareCents: v.MinFareCents, ValidFrom: v.ValidFrom, ValidUntil: v.ValidUntil, MaxRedemptions: maxRedemptions(v),
		PerCustomerLimit: int32(v.PerCustomerLimit), IsPublic: v.IsPublic, IsActive: v.IsActive, // #nosec G115 -- validated by the entity
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return repository.ErrVoucherCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create voucher: %w", err)
	}
	return nil
}

func (r *VoucherRepository) Update(ctx context.Context, v *entities.Voucher) error {
	err := postgres.Queries(ctx, r.pool).UpdateVoucher(ctx, gen.UpdateVoucherParams{
		ID: v.ID, Description: v.Description, DiscountBps: int32(v.DiscountBps), DiscountCents: v.DiscountCents, MaxDiscountCents: v.MaxDiscountCents, MinFareCents: v.MinFareCents, // #nosec G115 -- validated by the entity
		ValidFrom: v.ValidFrom, ValidUntil: v.ValidUntil, MaxRedemptions: maxRedemptions(v), PerCustomerLimit: int32(v.PerCustomerLimit), IsPublic: v.IsPublic, IsActive: v.IsActive, // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("update voucher: %w", err)
	}
	return nil
}

func (r *VoucherRepository) CountRedemptions(ctx context.Context, voucherID uuid.UUID) (int64, error) {
	count, err := postgres.Queries(ctx, r.pool).CountVoucherRedemptions(ctx, voucherID)
	if err != nil {
		return 0, fmt.Errorf("count redemptions: %w", err)
	}
	return count, nil
}

func (r *VoucherRepository) CountCustomerRedemptions(ctx context.Context, voucherID, customerID uuid.UUID) (int64, error) {
	count, err := postgres.Queries(ctx, r.pool).CountCustomerVoucherRedemptions(ctx, gen.CountCustomerVoucherRedemptionsParams{VoucherID: voucherID, CustomerID: customerID})
	if err != nil {
		return 0, fmt.Errorf("count customer redemptions: %w", err)
	}
	return count, nil
}

func (r *VoucherRepository) Redeem(ctx context.Context, voucherID, customerID uuid.UUID, requestID *uuid.UUID, discountCents int64) error {
	err := postgres.Queries(ctx, r.pool).InsertVoucherRedemption(ctx, gen.InsertVoucherRedemptionParams{VoucherID: voucherID, CustomerID: customerID, RequestID: requestID, DiscountCents: discountCents})
	if err != nil {
		return fmt.Errorf("redeem voucher: %w", err)
	}
	return nil
}

func (r *VoucherRepository) Release(ctx context.Context, requestID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).ReleaseVoucherRedemption(ctx, &requestID); err != nil {
		return fmt.Errorf("release voucher: %w", err)
	}
	return nil
}
