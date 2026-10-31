package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	repomocks "github.com/Agora-Mobility/atlas/domain/repository/mocks"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/catalog"
	"github.com/Agora-Mobility/atlas/usecases/catalog/dtos"
)

// memoryCache stores values as JSON, like the Redis cache.
type memoryCache struct {
	values  map[string][]byte
	failGet error
	failDel error
	deleted []string
}

func newMemoryCache() *memoryCache { return &memoryCache{values: map[string][]byte{}} }

func (c *memoryCache) Get(_ context.Context, key string, dest any) (bool, error) {
	if c.failGet != nil {
		return false, c.failGet
	}
	raw, ok := c.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dest)
}

func (c *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	raw, err := json.Marshal(value)
	c.values[key] = raw
	return err
}

func (c *memoryCache) Delete(_ context.Context, keys ...string) error {
	c.deleted = append(c.deleted, keys...)
	if c.failDel != nil {
		return c.failDel
	}
	for _, k := range keys {
		delete(c.values, k)
	}
	return nil
}

type fixture struct {
	repo   *repomocks.MockICatalogRepository
	audit  *repomocks.MockIAuditLogRepository
	outbox *repomocks.MockIOutboxRepository
	cache  *memoryCache
	uc     *catalog.Usecase
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	tx := repomocks.NewMockITransactionManager(ctrl)
	tx.EXPECT().WithinTransaction(gomock.Any(), gomock.Any()).AnyTimes().
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	f := fixture{
		repo:   repomocks.NewMockICatalogRepository(ctrl),
		audit:  repomocks.NewMockIAuditLogRepository(ctrl),
		outbox: repomocks.NewMockIOutboxRepository(ctrl),
		cache:  newMemoryCache(),
	}
	f.uc = catalog.NewUsecase(f.repo, f.audit, f.outbox, tx, f.cache)
	return f
}

var (
	adminID    = uuid.New()
	platformID = uuid.New()
	admin      = entities.Actor{ID: &adminID, OrganizationID: &platformID, IPAddress: "10.0.0.1"}
)

func (f fixture) expectChange(t *testing.T, eventType, action string) {
	t.Helper()
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, eventType, env.EventType)
			return nil
		})
	f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
			assert.Equal(t, action, e.Action)
			assert.Equal(t, adminID, *e.Actor.ID)
			assert.Equal(t, platformID, *e.OrganizationID)
			return nil
		})
}

// primeCache puts a catalog in the cache, so a test can tell whether a
// change dropped it.
func (f fixture) primeCache() {
	f.cache.values[catalog.CacheKey] = []byte(`{"categories":[]}`)
}

func category(slug string, order int, active bool) *entities.ServiceCategory {
	return &entities.ServiceCategory{
		ID: uuid.New(), Slug: slug, IsActive: active,
		CatalogDetails: entities.CatalogDetails{Name: slug, DisplayOrder: order},
	}
}

func service(c *entities.ServiceCategory, slug string) *entities.Service {
	return &entities.Service{
		ID: uuid.New(), CategoryID: c.ID, Slug: slug, IsActive: true, RequiresDestination: true,
		CatalogDetails: entities.CatalogDetails{Name: slug},
		Pricing:        entities.ServicePricing{BaseFeeCents: 150_000},
	}
}

func TestCatalog(t *testing.T) {
	taxi := category("taxi", 1, true)
	maintenance := category("maintenance", 2, true)
	empty := category("cleaning", 3, true)
	taxiStandard := service(taxi, "taxi")
	mechanics := service(maintenance, "mechanics")
	electrics := service(maintenance, "electrics")

	t.Run("groups the offered services by category and leaves out empty categories", func(t *testing.T) {
		f := newFixture(t)
		f.repo.EXPECT().ListCategories(gomock.Any(), true).Return([]*entities.ServiceCategory{taxi, maintenance, empty}, nil)
		f.repo.EXPECT().ListOfferedServices(gomock.Any()).Return([]*entities.Service{taxiStandard, mechanics, electrics}, nil)

		out, err := f.uc.Catalog(context.Background())

		require.NoError(t, err)
		require.Len(t, out.Categories, 2)
		assert.Equal(t, "taxi", out.Categories[0].Slug)
		assert.Equal(t, "maintenance", out.Categories[1].Slug)
		assert.Equal(t, []string{"mechanics", "electrics"}, []string{out.Categories[1].Services[0].Slug, out.Categories[1].Services[1].Slug})
		assert.Equal(t, int64(150_000), out.Categories[0].Services[0].Pricing.BaseFeeCents)
	})

	t.Run("the second request is served from the cache", func(t *testing.T) {
		f := newFixture(t)
		f.repo.EXPECT().ListCategories(gomock.Any(), true).Return([]*entities.ServiceCategory{taxi}, nil).Times(1)
		f.repo.EXPECT().ListOfferedServices(gomock.Any()).Return([]*entities.Service{taxiStandard}, nil).Times(1)

		first, err := f.uc.Catalog(context.Background())
		require.NoError(t, err)
		second, err := f.uc.Catalog(context.Background())
		require.NoError(t, err)

		assert.Equal(t, first, second)
	})

	t.Run("a cache failure falls back to the database", func(t *testing.T) {
		f := newFixture(t)
		f.cache.failGet = errors.New("redis down")
		f.repo.EXPECT().ListCategories(gomock.Any(), true).Return([]*entities.ServiceCategory{taxi}, nil)
		f.repo.EXPECT().ListOfferedServices(gomock.Any()).Return([]*entities.Service{taxiStandard}, nil)

		out, err := f.uc.Catalog(context.Background())

		require.NoError(t, err)
		assert.Len(t, out.Categories, 1)
	})

	t.Run("an empty catalog is a list, not null", func(t *testing.T) {
		f := newFixture(t)
		f.repo.EXPECT().ListCategories(gomock.Any(), true).Return(nil, nil)
		f.repo.EXPECT().ListOfferedServices(gomock.Any()).Return(nil, nil)

		out, err := f.uc.Catalog(context.Background())

		require.NoError(t, err)
		raw, _ := json.Marshal(out)
		assert.JSONEq(t, `{"categories":[]}`, string(raw))
	})
}

func TestCreateCategory(t *testing.T) {
	t.Run("creates an active category, audits it and drops the cached catalog", func(t *testing.T) {
		f := newFixture(t)
		f.primeCache()
		f.repo.EXPECT().CreateCategory(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, c *entities.ServiceCategory) error {
				assert.Equal(t, "cleaning", c.Slug)
				assert.True(t, c.IsActive)
				return nil
			})
		f.expectChange(t, events.ServiceCategoryCreated, "service_category.create")

		out, err := f.uc.CreateCategory(context.Background(), admin, dtos.CreateCategoryInput{Slug: "cleaning", Name: "Limpeza", DisplayOrder: 5})

		require.NoError(t, err)
		assert.Equal(t, "Limpeza", out.Name)
		assert.NotContains(t, f.cache.values, catalog.CacheKey)
	})

	t.Run("a taken slug is a conflict and keeps the cache", func(t *testing.T) {
		f := newFixture(t)
		f.primeCache()
		f.repo.EXPECT().CreateCategory(gomock.Any(), gomock.Any()).Return(repository.ErrCategorySlugTaken)

		_, err := f.uc.CreateCategory(context.Background(), admin, dtos.CreateCategoryInput{Slug: "taxi", Name: "Táxi"})

		require.ErrorIs(t, err, catalog.ErrCategorySlugTaken)
		assert.Contains(t, f.cache.values, catalog.CacheKey)
	})

	t.Run("an invalid slug never reaches the database", func(t *testing.T) {
		f := newFixture(t)

		_, err := f.uc.CreateCategory(context.Background(), admin, dtos.CreateCategoryInput{Slug: "Limpeza Geral", Name: "Limpeza"})

		_, ok := customerrors.AsValidation(err)
		assert.True(t, ok)
	})
}

func TestUpdateCategory(t *testing.T) {
	t.Run("changes only the fields sent", func(t *testing.T) {
		f := newFixture(t)
		taxi := category("taxi", 1, true)
		taxi.Description = "Transporte"
		f.repo.EXPECT().FindCategory(gomock.Any(), taxi.ID).Return(taxi, nil)
		f.repo.EXPECT().UpdateCategory(gomock.Any(), taxi).Return(nil)
		f.expectChange(t, events.ServiceCategoryUpdated, "service_category.update")
		inactive := false

		out, err := f.uc.UpdateCategory(context.Background(), admin, taxi.ID, dtos.UpdateCategoryInput{IsActive: &inactive})

		require.NoError(t, err)
		assert.False(t, out.IsActive)
		assert.Equal(t, "Transporte", out.Description)
		assert.Equal(t, "taxi", out.Slug)
	})

	t.Run("an unknown category is not found", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.repo.EXPECT().FindCategory(gomock.Any(), id).Return(nil, nil)

		_, err := f.uc.UpdateCategory(context.Background(), admin, id, dtos.UpdateCategoryInput{})

		require.ErrorIs(t, err, catalog.ErrCategoryNotFound)
	})
}

func TestCreateService(t *testing.T) {
	taxi := category("taxi", 1, true)
	yes := true

	t.Run("creates the service in its category", func(t *testing.T) {
		f := newFixture(t)
		f.primeCache()
		f.repo.EXPECT().FindCategory(gomock.Any(), taxi.ID).Return(taxi, nil)
		f.repo.EXPECT().CreateService(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *entities.Service) error {
				assert.Equal(t, taxi.ID, s.CategoryID)
				assert.True(t, s.IsActive, "is_active defaults to true")
				assert.True(t, s.RequiresDestination)
				assert.Equal(t, int64(20_000), s.Pricing.PerKmFeeCents)
				return nil
			})
		f.expectChange(t, events.ServiceCreated, "service.create")

		out, err := f.uc.CreateService(context.Background(), admin, dtos.CreateServiceInput{
			CategoryID: taxi.ID, Slug: "taxi-executive", Name: "Táxi Executivo", RequiresDestination: &yes,
			Pricing: dtos.PricingInput{BaseFeeCents: 150_000, PerKmFeeCents: 20_000},
		})

		require.NoError(t, err)
		assert.Equal(t, "taxi-executive", out.Slug)
		assert.NotContains(t, f.cache.values, catalog.CacheKey)
	})

	t.Run("an unknown category is a client error, not a missing service", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.repo.EXPECT().FindCategory(gomock.Any(), id).Return(nil, nil)

		_, err := f.uc.CreateService(context.Background(), admin, dtos.CreateServiceInput{
			CategoryID: id, Slug: "ghost", Name: "Fantasma", RequiresDestination: &yes,
		})

		require.ErrorIs(t, err, catalog.ErrUnknownCategory)
	})

	t.Run("a category deleted between the check and the write is unknown", func(t *testing.T) {
		f := newFixture(t)
		f.repo.EXPECT().FindCategory(gomock.Any(), taxi.ID).Return(taxi, nil)
		f.repo.EXPECT().CreateService(gomock.Any(), gomock.Any()).Return(repository.ErrNotFound)

		_, err := f.uc.CreateService(context.Background(), admin, dtos.CreateServiceInput{
			CategoryID: taxi.ID, Slug: "taxi-executive", Name: "Táxi Executivo", RequiresDestination: &yes,
		})

		require.ErrorIs(t, err, catalog.ErrUnknownCategory)
	})
}

func TestUpdateService(t *testing.T) {
	taxi := category("taxi", 1, true)
	towing := category("towing", 3, true)

	t.Run("replaces the pricing and keeps the other fields", func(t *testing.T) {
		f := newFixture(t)
		s := service(taxi, "taxi")
		s.Description = "Viagem"
		f.repo.EXPECT().FindService(gomock.Any(), s.ID).Return(s, nil)
		f.repo.EXPECT().UpdateService(gomock.Any(), s).Return(nil)
		f.expectChange(t, events.ServiceUpdated, "service.update")

		out, err := f.uc.UpdateService(context.Background(), admin, s.ID, dtos.UpdateServiceInput{
			Pricing: &dtos.PricingInput{BaseFeeCents: 100_000, PerMinuteFeeCents: 5_000},
		})

		require.NoError(t, err)
		assert.Equal(t, dtos.PricingView{BaseFeeCents: 100_000, PerMinuteFeeCents: 5_000}, out.Pricing)
		assert.Equal(t, "Viagem", out.Description)
		assert.True(t, out.RequiresDestination)
	})

	t.Run("moving to another category checks that it exists", func(t *testing.T) {
		f := newFixture(t)
		s := service(taxi, "taxi")
		f.repo.EXPECT().FindService(gomock.Any(), s.ID).Return(s, nil)
		f.repo.EXPECT().FindCategory(gomock.Any(), towing.ID).Return(nil, nil)

		_, err := f.uc.UpdateService(context.Background(), admin, s.ID, dtos.UpdateServiceInput{CategoryID: &towing.ID})

		require.ErrorIs(t, err, catalog.ErrUnknownCategory)
	})

	t.Run("an unknown service is not found", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.repo.EXPECT().FindService(gomock.Any(), id).Return(nil, nil)

		_, err := f.uc.UpdateService(context.Background(), admin, id, dtos.UpdateServiceInput{})

		require.ErrorIs(t, err, catalog.ErrServiceNotFound)
	})

	t.Run("a failed cache invalidation does not fail the change", func(t *testing.T) {
		f := newFixture(t)
		f.cache.failDel = errors.New("redis down")
		s := service(taxi, "taxi")
		f.repo.EXPECT().FindService(gomock.Any(), s.ID).Return(s, nil)
		f.repo.EXPECT().UpdateService(gomock.Any(), s).Return(nil)
		f.expectChange(t, events.ServiceUpdated, "service.update")
		inactive := false

		_, err := f.uc.UpdateService(context.Background(), admin, s.ID, dtos.UpdateServiceInput{IsActive: &inactive})

		require.NoError(t, err)
		assert.Equal(t, []string{catalog.CacheKey}, f.cache.deleted)
	})
}

func TestListServices_PassesFiltersAndPaginates(t *testing.T) {
	f := newFixture(t)
	taxi := category("taxi", 1, true)
	active := true
	f.repo.EXPECT().ListServices(gomock.Any(), repository.ServiceFilter{CategoryID: &taxi.ID, IsActive: &active, Search: "exec"}, pagination.Params{Page: 2, Size: 1}).
		Return([]*entities.Service{service(taxi, "taxi-executive")}, int64(3), nil)

	out, err := f.uc.ListServices(context.Background(), dtos.ServiceListQuery{CategoryID: &taxi.ID, IsActive: &active, Search: "exec", Page: 2, Size: 1})

	require.NoError(t, err)
	assert.Equal(t, int64(3), out.Total)
	assert.Equal(t, int64(3), out.TotalPages)
	assert.Len(t, out.Items, 1)
}
