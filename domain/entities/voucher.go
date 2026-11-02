package entities

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxVoucherDescription = 300

var (
	ErrVoucherNotFound      = errors.New("voucher not found")
	ErrVoucherExpired       = errors.New("this voucher is outside its validity")
	ErrVoucherNotApplicable = errors.New("this voucher does not apply to this service")
	ErrVoucherUsed          = errors.New("this voucher was already used")
	ErrVoucherBelowMinimum  = errors.New("the price is below the minimum of this voucher")

	voucherCodePattern = regexp.MustCompile(`^[A-Z0-9-]{3,30}$`)
)

type Voucher struct {
	ID               uuid.UUID
	Code             *string
	Description      string
	CategoryID       *uuid.UUID
	ServiceID        *uuid.UUID
	DiscountBps      int
	DiscountCents    int64
	MaxDiscountCents *int64
	MinFareCents     int64
	ValidFrom        time.Time
	ValidUntil       time.Time
	MaxRedemptions   *int
	PerCustomerLimit int
	IsPublic         bool
	IsActive         bool
}

func (v *Voucher) Normalize() error {
	if v.Code != nil {
		code := strings.ToUpper(strings.TrimSpace(*v.Code))
		if !voucherCodePattern.MatchString(code) {
			return validationError("code", "O código tem de 3 a 30 letras, números ou hífens.")
		}
		v.Code = &code
	}
	v.Description = strings.TrimSpace(v.Description)
	if len([]rune(v.Description)) > maxVoucherDescription {
		return validationError("description", "A descrição tem no máximo 300 caracteres.")
	}
	if (v.DiscountBps > 0) == (v.DiscountCents > 0) {
		return validationError("discount", "Indica uma percentagem ou um valor, não os dois.")
	}
	if v.DiscountBps < 0 || v.DiscountBps > bpsDenominator {
		return validationError("discount_bps", "A percentagem tem de estar entre 0 e 10000.")
	}
	for _, cents := range []int64{v.DiscountCents, v.MinFareCents} {
		if cents < 0 || cents > MaxFeeCents {
			return validationError("discount_cents", "O valor não é válido.")
		}
	}
	if v.MaxDiscountCents != nil && (*v.MaxDiscountCents < 0 || *v.MaxDiscountCents > MaxFeeCents) {
		return validationError("max_discount_cents", "O valor não é válido.")
	}
	if !v.ValidUntil.After(v.ValidFrom) {
		return validationError("valid_until", "O voucher tem de terminar depois de começar.")
	}
	if v.PerCustomerLimit < 1 || v.PerCustomerLimit > maxReasonOrder {
		return validationError("per_customer_limit", "O limite por cliente tem de estar entre 1 e 9999.")
	}
	if v.MaxRedemptions != nil && (*v.MaxRedemptions < 1 || *v.MaxRedemptions > 1_000_000_000) {
		return validationError("max_redemptions", "O limite total não é válido.")
	}
	return nil
}

func (v *Voucher) CheckApplies(service *DispatchService, now time.Time) error {
	if !v.IsActive || now.Before(v.ValidFrom) || !now.Before(v.ValidUntil) {
		return ErrVoucherExpired
	}
	if service == nil {
		return nil
	}
	if v.ServiceID != nil && *v.ServiceID != service.ID {
		return ErrVoucherNotApplicable
	}
	if v.CategoryID != nil && *v.CategoryID != service.CategoryID {
		return ErrVoucherNotApplicable
	}
	return nil
}

// DiscountFor always leaves something to pay, because a gateway does not take a
// charge of zero.
func (v *Voucher) DiscountFor(fareCents int64) (int64, error) {
	if fareCents < v.MinFareCents {
		return 0, ErrVoucherBelowMinimum
	}
	discount := v.DiscountCents
	if v.DiscountBps > 0 {
		discount = roundedDivide(fareCents*int64(v.DiscountBps), bpsDenominator)
	}
	if v.MaxDiscountCents != nil && discount > *v.MaxDiscountCents {
		discount = *v.MaxDiscountCents
	}
	if limit := fareCents - MinGatewayCents; discount > limit {
		discount = max(limit, 0)
	}
	return discount, nil
}
