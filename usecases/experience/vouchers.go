package experience

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func toVoucherView(v *entities.Voucher) dtos.VoucherView {
	view := dtos.VoucherView{ID: v.ID, Description: v.Description, CategoryID: v.CategoryID, ServiceID: v.ServiceID, DiscountBps: v.DiscountBps, DiscountCents: v.DiscountCents, MaxDiscountCents: v.MaxDiscountCents,
		MinFareCents: v.MinFareCents, ValidFrom: v.ValidFrom, ValidUntil: v.ValidUntil, MaxRedemptions: v.MaxRedemptions, PerCustomerLimit: v.PerCustomerLimit, IsPublic: v.IsPublic, IsActive: v.IsActive, WorkshopID: v.WorkshopID, WorkshopServiceID: v.WorkshopService, AutoApply: v.AutoApply}
	if v.Code != nil {
		view.Code = *v.Code
	}
	return view
}

func voucherFrom(in dtos.VoucherInput, id uuid.UUID) *entities.Voucher {
	voucher := &entities.Voucher{ID: id, Description: in.Description, CategoryID: in.CategoryID, ServiceID: in.ServiceID, DiscountBps: in.DiscountBps, DiscountCents: in.DiscountCents, MaxDiscountCents: in.MaxDiscountCents,
		MinFareCents: in.MinFareCents, ValidFrom: in.ValidFrom, ValidUntil: in.ValidUntil, MaxRedemptions: in.MaxRedemptions, PerCustomerLimit: in.PerCustomerLimit, IsPublic: in.IsPublic, IsActive: in.IsActive == nil || *in.IsActive, WorkshopID: in.WorkshopID, WorkshopService: in.WorkshopServiceID, AutoApply: in.AutoApply}
	if voucher.PerCustomerLimit == 0 {
		voucher.PerCustomerLimit = 1
	}
	if in.Code != "" {
		code := in.Code
		voucher.Code = &code
	}
	return voucher
}

func (u *Usecase) CreateVoucher(ctx context.Context, actor entities.Actor, in dtos.VoucherInput) (*dtos.VoucherView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	voucher := voucherFrom(in, id)
	if err := voucher.Normalize(); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Vouchers.Create(ctx, voucher); err != nil {
			return err
		}
		return u.audit(ctx, actor, "voucher.create", "voucher", id.String(), voucher)
	})
	if err != nil {
		return nil, err
	}
	view := toVoucherView(voucher)
	return &view, nil
}

func (u *Usecase) UpdateVoucher(ctx context.Context, actor entities.Actor, id uuid.UUID, in dtos.VoucherInput) (*dtos.VoucherView, error) {
	current, err := u.Vouchers.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, entities.ErrVoucherNotFound
	}
	voucher := voucherFrom(in, id)
	voucher.Code, voucher.CategoryID, voucher.ServiceID = current.Code, current.CategoryID, current.ServiceID
	if err := voucher.Normalize(); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Vouchers.Update(ctx, voucher); err != nil {
			return err
		}
		return u.audit(ctx, actor, "voucher.update", "voucher", id.String(), voucher)
	})
	if err != nil {
		return nil, err
	}
	view := toVoucherView(voucher)
	return &view, nil
}

func (u *Usecase) ListVouchers(ctx context.Context, page pagination.Params) (*dtos.VoucherList, error) {
	page = page.Normalize()
	vouchers, total, err := u.Vouchers.List(ctx, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.VoucherList{Items: make([]dtos.VoucherView, 0, len(vouchers)), Total: total, Page: page.Page, Size: page.Size}
	for i := range vouchers {
		list.Items = append(list.Items, toVoucherView(&vouchers[i]))
	}
	return list, nil
}

func (u *Usecase) PublicVouchers(ctx context.Context, category string) (*dtos.VoucherList, error) {
	vouchers, err := u.Vouchers.Public(ctx, category, u.now())
	if err != nil {
		return nil, err
	}
	list := &dtos.VoucherList{Items: make([]dtos.VoucherView, 0, len(vouchers)), Total: int64(len(vouchers))}
	for i := range vouchers {
		view := toVoucherView(&vouchers[i])
		view.MaxRedemptions, view.IsPublic = nil, true
		list.Items = append(list.Items, view)
	}
	return list, nil
}
