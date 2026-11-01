package entities_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

func requireFieldError(t *testing.T, err error, field string) {
	t.Helper()
	validationErr, ok := customerrors.AsValidation(err)
	require.True(t, ok, "want a validation error, got %v", err)
	assert.Contains(t, validationErr.Fields, field)
}

func TestNewServiceCategory(t *testing.T) {
	now := time.Now().UTC()

	c, err := entities.NewServiceCategory(" cleaning ", entities.CatalogDetails{Name: "  Limpeza ", Icon: "broom", DisplayOrder: 5}, now)

	require.NoError(t, err)
	assert.Equal(t, "cleaning", c.Slug)
	assert.Equal(t, "Limpeza", c.Name)
	assert.True(t, c.IsActive, "a new category starts active")
	assert.Equal(t, now, c.CreatedAt)

	cases := []struct {
		name, slug string
		details    entities.CatalogDetails
		field      string
	}{
		{"slug with capitals", "Cleaning", entities.CatalogDetails{Name: "Limpeza"}, "slug"},
		{"slug starting with a digit", "1clean", entities.CatalogDetails{Name: "Limpeza"}, "slug"},
		{"slug too short", "c", entities.CatalogDetails{Name: "Limpeza"}, "slug"},
		{"blank name", "cleaning", entities.CatalogDetails{Name: "   "}, "name"},
		{"name too long", "cleaning", entities.CatalogDetails{Name: strings.Repeat("a", 81)}, "name"},
		{"description too long", "cleaning", entities.CatalogDetails{Name: "L", Description: strings.Repeat("a", 501)}, "description"},
		{"negative order", "cleaning", entities.CatalogDetails{Name: "L", DisplayOrder: -1}, "display_order"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := entities.NewServiceCategory(tc.slug, tc.details, now)
			requireFieldError(t, err, tc.field)
		})
	}
}

func TestServiceChange(t *testing.T) {
	now := time.Now().UTC()
	attrs := entities.ServiceAttributes{
		CategoryID:          uuid.New(),
		Details:             entities.CatalogDetails{Name: "Táxi Executivo"},
		RequiresDestination: true,
		Pricing:             entities.ServicePricing{BaseFeeCents: 150_000, PerKmFeeCents: 20_000},
		IsActive:            true,
	}
	s, err := entities.NewService("taxi-executive", attrs, now)
	require.NoError(t, err)
	assert.Equal(t, attrs, s.Attributes())

	later := now.Add(time.Minute)
	changed := s.Attributes()
	changed.IsActive = false
	changed.Pricing.BaseFeeCents = 200_000
	require.NoError(t, s.Change(changed, later))
	assert.False(t, s.IsActive)
	assert.Equal(t, int64(200_000), s.Pricing.BaseFeeCents)
	assert.Equal(t, later, s.UpdatedAt)
	assert.Equal(t, now, s.CreatedAt)

	t.Run("rejects invalid values and keeps the service unchanged", func(t *testing.T) {
		for field, mutate := range map[string]func(*entities.ServiceAttributes){
			"category_id":          func(a *entities.ServiceAttributes) { a.CategoryID = uuid.Nil },
			"name":                 func(a *entities.ServiceAttributes) { a.Details.Name = "" },
			"base_fee_cents":       func(a *entities.ServiceAttributes) { a.Pricing.BaseFeeCents = -1 },
			"per_km_fee_cents":     func(a *entities.ServiceAttributes) { a.Pricing.PerKmFeeCents = entities.MaxFeeCents + 1 },
			"per_minute_fee_cents": func(a *entities.ServiceAttributes) { a.Pricing.PerMinuteFeeCents = -5 },
		} {
			bad := s.Attributes()
			mutate(&bad)
			before := *s

			requireFieldError(t, s.Change(bad, later.Add(time.Minute)), field)
			assert.Equal(t, before, *s, field)
		}
	})
}
