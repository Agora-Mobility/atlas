// Package catalog manages the service catalog (ADR 0008): the categories
// and services the backoffice maintains and the customer app lists.
package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/catalog/dtos"
)

var (
	ErrCategoryNotFound = errors.New("service category not found")
	// ErrUnknownCategory is a service that points at a category that does not exist.
	ErrUnknownCategory   = errors.New("the service category does not exist")
	ErrServiceNotFound   = errors.New("service not found")
	ErrCategorySlugTaken = repository.ErrCategorySlugTaken
	ErrServiceSlugTaken  = repository.ErrServiceSlugTaken
)

const (
	auditEntityCategory       = "service_category"
	auditEntityService        = "service"
	auditActionCategoryCreate = "service_category.create"
	auditActionCategoryUpdate = "service_category.update"
	auditActionServiceCreate  = "service.create"
	auditActionServiceUpdate  = "service.update"

	// CacheKey holds the public catalog. Every backoffice change deletes
	// it; the TTL only bounds how long a failed deletion can serve stale data.
	CacheKey = "catalog:v1"
	cacheTTL = 10 * time.Minute
)

// Cache is the subset of infra/cache.Cache the catalog needs.
type Cache interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

type Usecase struct {
	repo       repository.ICatalogRepository
	auditRepo  repository.IAuditLogRepository
	outboxRepo repository.IOutboxRepository
	txManager  repository.ITransactionManager
	cache      Cache
	now        func() time.Time
}

func NewUsecase(
	repo repository.ICatalogRepository,
	auditRepo repository.IAuditLogRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	cache Cache,
) *Usecase {
	return &Usecase{
		repo: repo, auditRepo: auditRepo, outboxRepo: outboxRepo, txManager: txManager, cache: cache,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Catalog is the public listing of the customer app, served from the cache
// when possible. A cache failure falls back to the database.
func (uc *Usecase) Catalog(ctx context.Context) (*dtos.Catalog, error) {
	var cached dtos.Catalog
	hit, err := uc.cache.Get(ctx, CacheKey, &cached)
	if err != nil {
		logger.FromContext(ctx).Warn("catalog cache unavailable", zap.Error(err))
	}
	if hit {
		return &cached, nil
	}

	categories, err := uc.repo.ListCategories(ctx, true)
	if err != nil {
		return nil, err
	}
	services, err := uc.repo.ListOfferedServices(ctx)
	if err != nil {
		return nil, err
	}
	catalog := dtos.NewCatalog(categories, services)
	if err := uc.cache.Set(ctx, CacheKey, catalog, cacheTTL); err != nil {
		logger.FromContext(ctx).Warn("catalog not cached", zap.Error(err))
	}
	return &catalog, nil
}

func (uc *Usecase) ListCategories(ctx context.Context) (*dtos.CategoryList, error) {
	categories, err := uc.repo.ListCategories(ctx, false)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.CategoryView, 0, len(categories))
	for _, c := range categories {
		views = append(views, dtos.ToCategoryView(c))
	}
	return &dtos.CategoryList{Items: views}, nil
}

func (uc *Usecase) GetCategory(ctx context.Context, id uuid.UUID) (*dtos.CategoryView, error) {
	category, err := uc.category(ctx, id)
	if err != nil {
		return nil, err
	}
	view := dtos.ToCategoryView(category)
	return &view, nil
}

func (uc *Usecase) CreateCategory(ctx context.Context, actor entities.Actor, req dtos.CreateCategoryInput) (*dtos.CategoryView, error) {
	category, err := entities.NewServiceCategory(req.Slug, entities.CatalogDetails{
		Name: req.Name, Description: req.Description, Icon: req.Icon, DisplayOrder: req.DisplayOrder,
	}, uc.now())
	if err != nil {
		return nil, err
	}
	view := dtos.ToCategoryView(category)
	err = uc.change(ctx, func(ctx context.Context) error {
		if err := uc.repo.CreateCategory(ctx, category); err != nil {
			return err
		}
		if err := uc.publish(ctx, events.ServiceCategoryCreatedV1{ServiceCategoryChange: categoryChange(category, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionCategoryCreate, auditEntityCategory, category.ID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (uc *Usecase) UpdateCategory(ctx context.Context, actor entities.Actor, id uuid.UUID, req dtos.UpdateCategoryInput) (*dtos.CategoryView, error) {
	category, err := uc.category(ctx, id)
	if err != nil {
		return nil, err
	}
	before := dtos.ToCategoryView(category)
	details, active := category.CatalogDetails, category.IsActive
	if req.Name != nil {
		details.Name = *req.Name
	}
	if req.Description != nil {
		details.Description = *req.Description
	}
	if req.Icon != nil {
		details.Icon = *req.Icon
	}
	if req.DisplayOrder != nil {
		details.DisplayOrder = *req.DisplayOrder
	}
	if req.IsActive != nil {
		active = *req.IsActive
	}
	if err := category.Change(details, active, uc.now()); err != nil {
		return nil, err
	}
	after := dtos.ToCategoryView(category)
	err = uc.change(ctx, func(ctx context.Context) error {
		if err := uc.repo.UpdateCategory(ctx, category); err != nil {
			return err
		}
		if err := uc.publish(ctx, events.ServiceCategoryUpdatedV1{ServiceCategoryChange: categoryChange(category, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionCategoryUpdate, auditEntityCategory, category.ID, before, after)
	})
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (uc *Usecase) ListServices(ctx context.Context, query dtos.ServiceListQuery) (*dtos.ServicePage, error) {
	params := pagination.Params{Page: query.Page, Size: query.Size}
	services, total, err := uc.repo.ListServices(ctx, repository.ServiceFilter{
		CategoryID: query.CategoryID, IsActive: query.IsActive, Search: query.Search,
	}, params)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.ServiceView, 0, len(services))
	for _, s := range services {
		views = append(views, dtos.ToServiceView(s))
	}
	page := pagination.NewPage(views, params, total)
	return &dtos.ServicePage{Items: page.Items, Page: page.Page, Size: page.Size, Total: page.Total, TotalPages: page.TotalPages}, nil
}

func (uc *Usecase) GetService(ctx context.Context, id uuid.UUID) (*dtos.ServiceView, error) {
	service, err := uc.service(ctx, id)
	if err != nil {
		return nil, err
	}
	view := dtos.ToServiceView(service)
	return &view, nil
}

func (uc *Usecase) CreateService(ctx context.Context, actor entities.Actor, req dtos.CreateServiceInput) (*dtos.ServiceView, error) {
	service, err := entities.NewService(req.Slug, req.Attributes(), uc.now())
	if err != nil {
		return nil, err
	}
	if err := uc.requireCategory(ctx, service.CategoryID); err != nil {
		return nil, err
	}
	view := dtos.ToServiceView(service)
	err = uc.change(ctx, func(ctx context.Context) error {
		if err := uc.repo.CreateService(ctx, service); err != nil {
			return categoryReferenceError(err)
		}
		if err := uc.publish(ctx, events.ServiceCreatedV1{ServiceChange: serviceChange(service, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionServiceCreate, auditEntityService, service.ID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (uc *Usecase) UpdateService(ctx context.Context, actor entities.Actor, id uuid.UUID, req dtos.UpdateServiceInput) (*dtos.ServiceView, error) {
	service, err := uc.service(ctx, id)
	if err != nil {
		return nil, err
	}
	before := dtos.ToServiceView(service)
	if err := service.Change(req.Apply(service.Attributes()), uc.now()); err != nil {
		return nil, err
	}
	if service.CategoryID != before.CategoryID {
		if err := uc.requireCategory(ctx, service.CategoryID); err != nil {
			return nil, err
		}
	}
	after := dtos.ToServiceView(service)
	err = uc.change(ctx, func(ctx context.Context) error {
		if err := uc.repo.UpdateService(ctx, service); err != nil {
			return categoryReferenceError(err)
		}
		if err := uc.publish(ctx, events.ServiceUpdatedV1{ServiceChange: serviceChange(service, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionServiceUpdate, auditEntityService, service.ID, before, after)
	})
	if err != nil {
		return nil, err
	}
	return &after, nil
}

// change runs fn in a transaction and, once it commits, drops the public
// catalog so the customer app sees the change on its next request.
func (uc *Usecase) change(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := uc.txManager.WithinTransaction(ctx, fn); err != nil {
		return err
	}
	if err := uc.cache.Delete(ctx, CacheKey); err != nil {
		logger.FromContext(ctx).Error("catalog cache not invalidated: stale until it expires",
			zap.Duration("ttl", cacheTTL), zap.Error(err))
	}
	return nil
}

func (uc *Usecase) category(ctx context.Context, id uuid.UUID) (*entities.ServiceCategory, error) {
	category, err := uc.repo.FindCategory(ctx, id)
	if err != nil {
		return nil, err
	}
	if category == nil {
		return nil, ErrCategoryNotFound
	}
	return category, nil
}

func (uc *Usecase) requireCategory(ctx context.Context, id uuid.UUID) error {
	if _, err := uc.category(ctx, id); err != nil {
		if errors.Is(err, ErrCategoryNotFound) {
			return ErrUnknownCategory
		}
		return err
	}
	return nil
}

func (uc *Usecase) service(ctx context.Context, id uuid.UUID) (*entities.Service, error) {
	service, err := uc.repo.FindService(ctx, id)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, ErrServiceNotFound
	}
	return service, nil
}

func (uc *Usecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.outboxRepo.Save(ctx, envelope)
}

func (uc *Usecase) audit(ctx context.Context, actor entities.Actor, action, entityType string, id uuid.UUID, before, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entityType, id.String(), before, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.auditRepo.Record(ctx, entry)
}

// categoryReferenceError covers a category that disappeared between the
// check and the write.
func categoryReferenceError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrUnknownCategory
	}
	return err
}

func categoryChange(c *entities.ServiceCategory, actor entities.Actor) events.ServiceCategoryChange {
	return events.ServiceCategoryChange{
		CategoryID: c.ID, Slug: c.Slug, Name: c.Name, IsActive: c.IsActive,
		ChangedBy: actor.ID, OccurredAt: c.UpdatedAt,
	}
}

func serviceChange(s *entities.Service, actor entities.Actor) events.ServiceChange {
	return events.ServiceChange{
		ServiceID: s.ID, CategoryID: s.CategoryID, Slug: s.Slug, Name: s.Name,
		RequiresDestination: s.RequiresDestination,
		BaseFeeCents:        s.Pricing.BaseFeeCents,
		PerKmFeeCents:       s.Pricing.PerKmFeeCents,
		PerMinuteFeeCents:   s.Pricing.PerMinuteFeeCents,
		IsActive:            s.IsActive,
		ChangedBy:           actor.ID,
		OccurredAt:          s.UpdatedAt,
	}
}
