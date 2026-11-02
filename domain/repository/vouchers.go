package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrVoucherCodeTaken = errors.New("this voucher code already exists")

type IVoucherRepository interface {
	Find(ctx context.Context, id uuid.UUID) (*entities.Voucher, error)
	FindByCode(ctx context.Context, code string) (*entities.Voucher, error)
	LockByCode(ctx context.Context, code string) (*entities.Voucher, error)
	List(ctx context.Context, limit, offset int) ([]entities.Voucher, int64, error)
	Public(ctx context.Context, category string, now time.Time) ([]entities.Voucher, error)
	Create(ctx context.Context, voucher *entities.Voucher) error
	Update(ctx context.Context, voucher *entities.Voucher) error
	CountRedemptions(ctx context.Context, voucherID uuid.UUID) (int64, error)
	CountCustomerRedemptions(ctx context.Context, voucherID, customerID uuid.UUID) (int64, error)
	Redeem(ctx context.Context, voucherID, customerID uuid.UUID, requestID *uuid.UUID, discountCents int64) error
	Release(ctx context.Context, requestID uuid.UUID) error
}
