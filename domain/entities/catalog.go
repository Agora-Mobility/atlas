package entities

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Category slugs created by the catalog migration (ADR 0008). Code may rely
// on them; any other category exists only as data.
const (
	CategorySlugTaxi        = "taxi"
	CategorySlugMaintenance = "maintenance"
	CategorySlugTowing      = "towing"
	CategorySlugRemovals    = "removals"
)

const (
	maxCatalogNameLength        = 80
	maxCatalogDescriptionLength = 500
	maxCatalogIconLength        = 255
	maxDisplayOrder             = 9999
	// MaxFeeCents caps a single fee at 100 million kwanzas.
	MaxFeeCents = 10_000_000_000
)

var catalogSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

// CatalogDetails are the descriptive fields shared by categories and services.
type CatalogDetails struct {
	Name         string
	Description  string
	Icon         string
	DisplayOrder int
}

func (d CatalogDetails) normalized() (CatalogDetails, error) {
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	d.Icon = strings.TrimSpace(d.Icon)
	if d.Name == "" || len([]rune(d.Name)) > maxCatalogNameLength {
		return d, validationError("name", "O nome é obrigatório e tem no máximo 80 caracteres.")
	}
	if len([]rune(d.Description)) > maxCatalogDescriptionLength {
		return d, validationError("description", "A descrição tem no máximo 500 caracteres.")
	}
	if len(d.Icon) > maxCatalogIconLength {
		return d, validationError("icon", "O ícone tem no máximo 255 caracteres.")
	}
	if d.DisplayOrder < 0 || d.DisplayOrder > maxDisplayOrder {
		return d, validationError("display_order", "A ordem deve estar entre 0 e 9999.")
	}
	return d, nil
}

func normalizeSlug(slug string) (string, error) {
	slug = strings.TrimSpace(slug)
	if !catalogSlugPattern.MatchString(slug) {
		return "", validationError("slug", "O slug deve ter 2 a 40 letras minúsculas, números ou -, e começar por uma letra.")
	}
	return slug, nil
}

type ServiceCategory struct {
	ID uuid.UUID
	// Slug identifies the category in code and URLs and never changes.
	Slug string
	CatalogDetails
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewServiceCategory(slug string, details CatalogDetails, now time.Time) (*ServiceCategory, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	if slug, err = normalizeSlug(slug); err != nil {
		return nil, err
	}
	category := &ServiceCategory{ID: id, Slug: slug, IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := category.Change(details, true, now); err != nil {
		return nil, err
	}
	return category, nil
}

func (c *ServiceCategory) Change(details CatalogDetails, active bool, now time.Time) error {
	details, err := details.normalized()
	if err != nil {
		return err
	}
	c.CatalogDetails = details
	c.IsActive = active
	c.UpdatedAt = now
	return nil
}

// ServicePricing holds the tariff parameters in cêntimos of kwanza. How they
// combine into a price is decided with the tariff rules, not here.
type ServicePricing struct {
	BaseFeeCents      int64
	PerKmFeeCents     int64
	PerMinuteFeeCents int64
}

func (p ServicePricing) validate() error {
	fees := []struct {
		field string
		value int64
	}{
		{"base_fee_cents", p.BaseFeeCents},
		{"per_km_fee_cents", p.PerKmFeeCents},
		{"per_minute_fee_cents", p.PerMinuteFeeCents},
	}
	for _, fee := range fees {
		if fee.value < 0 || fee.value > MaxFeeCents {
			return validationError(fee.field, "O valor deve estar entre 0 e 10000000000 cêntimos.")
		}
	}
	return nil
}

type Service struct {
	ID         uuid.UUID
	CategoryID uuid.UUID
	// Slug identifies the service in code and URLs and never changes.
	Slug string
	CatalogDetails
	// RequiresDestination is true when a request needs a destination besides
	// the pickup location (Taxi, Towing, Removals), false when the location
	// is enough (Maintenance).
	RequiresDestination bool
	Pricing             ServicePricing
	IsActive            bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ServiceAttributes are the fields of a service that can change.
type ServiceAttributes struct {
	CategoryID          uuid.UUID
	Details             CatalogDetails
	RequiresDestination bool
	Pricing             ServicePricing
	IsActive            bool
}

func NewService(slug string, attrs ServiceAttributes, now time.Time) (*Service, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	if slug, err = normalizeSlug(slug); err != nil {
		return nil, err
	}
	service := &Service{ID: id, Slug: slug, CreatedAt: now}
	if err := service.Change(attrs, now); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) Attributes() ServiceAttributes {
	return ServiceAttributes{
		CategoryID:          s.CategoryID,
		Details:             s.CatalogDetails,
		RequiresDestination: s.RequiresDestination,
		Pricing:             s.Pricing,
		IsActive:            s.IsActive,
	}
}

func (s *Service) Change(attrs ServiceAttributes, now time.Time) error {
	if attrs.CategoryID == uuid.Nil {
		return validationError("category_id", "Indique a categoria.")
	}
	details, err := attrs.Details.normalized()
	if err != nil {
		return err
	}
	if err := attrs.Pricing.validate(); err != nil {
		return err
	}
	s.CategoryID = attrs.CategoryID
	s.CatalogDetails = details
	s.RequiresDestination = attrs.RequiresDestination
	s.Pricing = attrs.Pricing
	s.IsActive = attrs.IsActive
	s.UpdatedAt = now
	return nil
}
