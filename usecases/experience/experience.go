package experience

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

var (
	ErrRequestNotFound = errors.New("request not found")
	ErrUnknownKind     = errors.New("unknown catalog kind")
	ErrKindNotPublic   = errors.New("this catalog is not public")
)

type Deps struct {
	Reference repository.IReferenceRepository
	Ratings   repository.IRatingRepository
	Requests  repository.IRequestRepository
	Providers repository.IProviderRepository
	Audit     repository.IAuditLogRepository
	Tx        repository.ITransactionManager
}

type Usecase struct {
	Deps
	now func() time.Time
}

func NewUsecase(deps Deps) *Usecase {
	return &Usecase{Deps: deps, now: func() time.Time { return time.Now().UTC() }}
}

func (u *Usecase) audit(ctx context.Context, actor entities.Actor, action, entityType, id string, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entityType, id, nil, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Audit.Record(ctx, entry)
}

func toItemView(item *entities.CatalogItem) dtos.CatalogItemView {
	return dtos.CatalogItemView{
		ID: item.ID, Kind: string(item.Kind), CategoryID: item.CategoryID, Category: item.CategorySlug, Code: item.Code, Label: item.Label, Description: item.Description,
		AmountCents: item.AmountCents, PromoAmountCents: item.PromoCents, PromoUntil: item.PromoUntil, Attrs: item.Attrs, DisplayOrder: item.DisplayOrder, IsActive: item.IsActive,
	}
}

func (u *Usecase) ListItems(ctx context.Context, kind, category string, includeInactive bool) (*dtos.CatalogItemList, error) {
	if !entities.IsCatalogKind(kind) {
		return nil, ErrUnknownKind
	}
	items, err := u.Reference.Items(ctx, entities.CatalogKind(kind), category, includeInactive)
	if err != nil {
		return nil, err
	}
	list := &dtos.CatalogItemList{Items: make([]dtos.CatalogItemView, 0, len(items))}
	for i := range items {
		list.Items = append(list.Items, toItemView(&items[i]))
	}
	return list, nil
}

func (u *Usecase) CreateItem(ctx context.Context, actor entities.Actor, kind string, in dtos.CatalogItemInput) (*dtos.CatalogItemView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	item := &entities.CatalogItem{
		ID: id, Kind: entities.CatalogKind(kind), CategoryID: in.CategoryID, Code: in.Code, Label: in.Label, Description: in.Description, AmountCents: in.AmountCents,
		PromoCents: in.PromoAmountCents, PromoUntil: in.PromoUntil, Attrs: in.Attrs, DisplayOrder: in.DisplayOrder, IsActive: in.IsActive == nil || *in.IsActive,
	}
	if err := item.Normalize(); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Reference.CreateItem(ctx, item); err != nil {
			return err
		}
		return u.audit(ctx, actor, "catalog_item.create", "catalog_item", id.String(), item)
	})
	if err != nil {
		return nil, err
	}
	view := toItemView(item)
	return &view, nil
}

func (u *Usecase) UpdateItem(ctx context.Context, actor entities.Actor, kind string, id uuid.UUID, in dtos.CatalogItemInput) (*dtos.CatalogItemView, error) {
	item, err := u.Reference.Item(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil || string(item.Kind) != kind {
		return nil, entities.ErrCatalogItemNotFound
	}
	item.Label, item.Description, item.AmountCents, item.PromoCents, item.PromoUntil, item.DisplayOrder = in.Label, in.Description, in.AmountCents, in.PromoAmountCents, in.PromoUntil, in.DisplayOrder
	if len(in.Attrs) > 0 {
		item.Attrs = in.Attrs
	}
	if in.IsActive != nil {
		item.IsActive = *in.IsActive
	}
	if err := item.Normalize(); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Reference.UpdateItem(ctx, item); err != nil {
			return err
		}
		return u.audit(ctx, actor, "catalog_item.update", "catalog_item", id.String(), item)
	})
	if err != nil {
		return nil, err
	}
	view := toItemView(item)
	return &view, nil
}

func (u *Usecase) Settings(ctx context.Context) (*dtos.SettingList, error) {
	values, err := u.Reference.Settings(ctx)
	if err != nil {
		return nil, err
	}
	return &dtos.SettingList{Items: values}, nil
}

func (u *Usecase) SetSetting(ctx context.Context, actor entities.Actor, key string, in dtos.SettingInput) (*dtos.SettingList, error) {
	if err := entities.ValidateSetting(key, in.Value); err != nil {
		return nil, err
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Reference.SetSetting(ctx, key, in.Value); err != nil {
			return err
		}
		return u.audit(ctx, actor, "platform_setting.set", "platform_setting", key, in)
	})
	if err != nil {
		return nil, err
	}
	return u.Settings(ctx)
}

func (u *Usecase) settingInt(ctx context.Context, key string) (int64, error) {
	raw, err := u.Reference.Setting(ctx, key)
	if err != nil || raw == "" {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (u *Usecase) RatingOptions(ctx context.Context, category string) (*dtos.RatingOptions, error) {
	compliments, err := u.Reference.Items(ctx, entities.CatalogCompliment, category, false)
	if err != nil {
		return nil, err
	}
	presets, err := u.Reference.Items(ctx, entities.CatalogTipPreset, category, false)
	if err != nil {
		return nil, err
	}
	maxTip, err := u.settingInt(ctx, entities.SettingMaxTipCents)
	if err != nil {
		return nil, err
	}
	view := &dtos.RatingOptions{Compliments: make([]dtos.RatingOption, 0, len(compliments)), Tips: make([]dtos.TipPreset, 0, len(presets)), MaxTipCents: maxTip}
	for i := range compliments {
		view.Compliments = append(view.Compliments, dtos.RatingOption{ID: compliments[i].ID, Code: compliments[i].Code, Label: compliments[i].Label})
	}
	for i := range presets {
		view.Tips = append(view.Tips, dtos.TipPreset{Label: presets[i].Label, AmountCents: presets[i].AmountCents})
	}
	return view, nil
}

func (u *Usecase) checkCompliments(ctx context.Context, ids []uuid.UUID, categoryID uuid.UUID) ([]dtos.RatingOption, error) {
	options := make([]dtos.RatingOption, 0, len(ids))
	if len(ids) == 0 {
		return options, nil
	}
	items, err := u.Reference.ItemsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(items) != len(ids) {
		return nil, entities.ErrComplimentNotValid
	}
	for i := range items {
		item := &items[i]
		if item.Kind != entities.CatalogCompliment || !item.IsActive || (item.CategoryID != nil && *item.CategoryID != categoryID) {
			return nil, entities.ErrComplimentNotValid
		}
		options = append(options, dtos.RatingOption{ID: item.ID, Code: item.Code, Label: item.Label})
	}
	return options, nil
}

func (u *Usecase) RateRequest(ctx context.Context, customerID, requestID uuid.UUID, in dtos.RateInput) (*dtos.RatingView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	rating := &entities.Rating{ID: id, RequestID: &requestID, CustomerID: customerID, Stars: in.Stars, ComplimentIDs: in.ComplimentIDs, Comment: in.Comment, CreatedAt: u.now()}
	if rating.ComplimentIDs == nil {
		rating.ComplimentIDs = []uuid.UUID{}
	}
	if err := rating.Normalize(); err != nil {
		return nil, err
	}
	var options []dtos.RatingOption
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestCompleted {
			return entities.ErrRatingNotAllowed
		}
		rating.ProviderID = request.ProviderID
		if options, err = u.checkCompliments(ctx, rating.ComplimentIDs, request.CategoryID); err != nil {
			return err
		}
		created, err := u.Ratings.Create(ctx, rating)
		if err != nil {
			return err
		}
		if !created {
			return entities.ErrAlreadyRated
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dtos.RatingView{ID: rating.ID, RequestID: rating.RequestID, Stars: rating.Stars, Compliments: options, Comment: rating.Comment, CreatedAt: rating.CreatedAt}, nil
}

func (u *Usecase) ProviderRatings(ctx context.Context, providerID uuid.UUID, page pagination.Params) (*dtos.RatingList, error) {
	page = page.Normalize()
	ratings, total, err := u.Ratings.OfProvider(ctx, providerID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	profile, err := u.Providers.Find(ctx, providerID)
	if err != nil {
		return nil, err
	}
	list := &dtos.RatingList{Items: make([]dtos.RatingView, 0, len(ratings)), Total: total, Page: page.Page, Size: page.Size}
	if profile != nil {
		list.Average = entities.AverageRating(profile.RatingSum, profile.RatingCount)
	}
	for i := range ratings {
		rating := &ratings[i]
		items, err := u.Reference.ItemsByIDs(ctx, rating.ComplimentIDs)
		if err != nil {
			return nil, err
		}
		options := make([]dtos.RatingOption, 0, len(items))
		for j := range items {
			options = append(options, dtos.RatingOption{ID: items[j].ID, Code: items[j].Code, Label: items[j].Label})
		}
		list.Items = append(list.Items, dtos.RatingView{ID: rating.ID, RequestID: rating.RequestID, Stars: rating.Stars, Compliments: options, Comment: rating.Comment, CreatedAt: rating.CreatedAt})
	}
	return list, nil
}
