// Package dtos holds the request and response shapes of the service catalog.
package dtos

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

// Backoffice

type CategoryView struct {
	ID           uuid.UUID `json:"id"`
	Slug         string    `json:"slug" example:"taxi"`
	Name         string    `json:"name" example:"Táxi"`
	Description  string    `json:"description" example:"Transporte de passageiros até ao destino."`
	Icon         string    `json:"icon" example:"taxi"`
	DisplayOrder int       `json:"display_order" example:"1"`
	IsActive     bool      `json:"is_active" example:"true"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func ToCategoryView(c *entities.ServiceCategory) CategoryView {
	return CategoryView{
		ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, Icon: c.Icon,
		DisplayOrder: c.DisplayOrder, IsActive: c.IsActive, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

type CategoryList struct {
	Items []CategoryView `json:"items"`
}

type CreateCategoryInput struct {
	Slug         string `json:"slug" binding:"required,max=40" example:"cleaning"`
	Name         string `json:"name" binding:"required,max=80" example:"Limpeza"`
	Description  string `json:"description" binding:"max=500" example:"Limpeza de casas e escritórios."`
	Icon         string `json:"icon" binding:"max=255" example:"cleaning"`
	DisplayOrder int    `json:"display_order" binding:"min=0,max=9999" example:"5"`
}

// UpdateCategoryInput changes only the fields sent. The slug never changes.
type UpdateCategoryInput struct {
	Name         *string `json:"name" binding:"omitempty,max=80" example:"Táxi"`
	Description  *string `json:"description" binding:"omitempty,max=500"`
	Icon         *string `json:"icon" binding:"omitempty,max=255"`
	DisplayOrder *int    `json:"display_order" binding:"omitempty,min=0,max=9999"`
	IsActive     *bool   `json:"is_active" example:"false"`
}

// PricingView holds the tariff parameters in cêntimos of kwanza (1 Kz = 100).
type PricingView struct {
	BaseFeeCents      int64 `json:"base_fee_cents" example:"150000"`
	PerKmFeeCents     int64 `json:"per_km_fee_cents" example:"20000"`
	PerMinuteFeeCents int64 `json:"per_minute_fee_cents" example:"0"`
}

func toPricingView(p entities.ServicePricing) PricingView {
	return PricingView{BaseFeeCents: p.BaseFeeCents, PerKmFeeCents: p.PerKmFeeCents, PerMinuteFeeCents: p.PerMinuteFeeCents}
}

type ServiceView struct {
	ID                  uuid.UUID   `json:"id"`
	CategoryID          uuid.UUID   `json:"category_id"`
	Slug                string      `json:"slug" example:"taxi"`
	Name                string      `json:"name" example:"Táxi"`
	Description         string      `json:"description" example:"Viagem até ao destino indicado."`
	Icon                string      `json:"icon" example:"taxi"`
	RequiresDestination bool        `json:"requires_destination" example:"true"`
	Pricing             PricingView `json:"pricing"`
	DisplayOrder        int         `json:"display_order" example:"1"`
	IsActive            bool        `json:"is_active" example:"true"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

func ToServiceView(s *entities.Service) ServiceView {
	return ServiceView{
		ID: s.ID, CategoryID: s.CategoryID, Slug: s.Slug, Name: s.Name, Description: s.Description, Icon: s.Icon,
		RequiresDestination: s.RequiresDestination, Pricing: toPricingView(s.Pricing),
		DisplayOrder: s.DisplayOrder, IsActive: s.IsActive, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

type ServicePage struct {
	Items      []ServiceView `json:"items"`
	Page       int           `json:"page"`
	Size       int           `json:"size"`
	Total      int64         `json:"total"`
	TotalPages int64         `json:"total_pages"`
}

type ServiceListQuery struct {
	CategoryID *uuid.UUID `form:"category_id,parser=encoding.TextUnmarshaler"`
	IsActive   *bool      `form:"is_active"`
	Search     string     `form:"search" binding:"max=120"`
	Page       int        `form:"page" binding:"omitempty,min=1"`
	Size       int        `form:"size" binding:"omitempty,min=1,max=100"`
}

type PricingInput struct {
	BaseFeeCents      int64 `json:"base_fee_cents" binding:"min=0" example:"150000"`
	PerKmFeeCents     int64 `json:"per_km_fee_cents" binding:"min=0" example:"20000"`
	PerMinuteFeeCents int64 `json:"per_minute_fee_cents" binding:"min=0" example:"0"`
}

func (p PricingInput) toPricing() entities.ServicePricing {
	return entities.ServicePricing{BaseFeeCents: p.BaseFeeCents, PerKmFeeCents: p.PerKmFeeCents, PerMinuteFeeCents: p.PerMinuteFeeCents}
}

type CreateServiceInput struct {
	CategoryID          uuid.UUID    `json:"category_id" binding:"required"`
	Slug                string       `json:"slug" binding:"required,max=40" example:"taxi-executive"`
	Name                string       `json:"name" binding:"required,max=80" example:"Táxi Executivo"`
	Description         string       `json:"description" binding:"max=500" example:"Viatura de gama alta."`
	Icon                string       `json:"icon" binding:"max=255" example:"taxi-executive"`
	RequiresDestination *bool        `json:"requires_destination" binding:"required" example:"true"`
	Pricing             PricingInput `json:"pricing"`
	DisplayOrder        int          `json:"display_order" binding:"min=0,max=9999" example:"2"`
	// IsActive defaults to true.
	IsActive *bool `json:"is_active" example:"true"`
}

func (in CreateServiceInput) Attributes() entities.ServiceAttributes {
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	return entities.ServiceAttributes{
		CategoryID: in.CategoryID,
		Details: entities.CatalogDetails{
			Name: in.Name, Description: in.Description, Icon: in.Icon, DisplayOrder: in.DisplayOrder,
		},
		RequiresDestination: in.RequiresDestination != nil && *in.RequiresDestination,
		Pricing:             in.Pricing.toPricing(),
		IsActive:            active,
	}
}

// UpdateServiceInput changes only the fields sent. The slug never changes.
type UpdateServiceInput struct {
	CategoryID          *uuid.UUID    `json:"category_id"`
	Name                *string       `json:"name" binding:"omitempty,max=80" example:"Táxi Executivo"`
	Description         *string       `json:"description" binding:"omitempty,max=500"`
	Icon                *string       `json:"icon" binding:"omitempty,max=255"`
	RequiresDestination *bool         `json:"requires_destination"`
	Pricing             *PricingInput `json:"pricing"`
	DisplayOrder        *int          `json:"display_order" binding:"omitempty,min=0,max=9999"`
	IsActive            *bool         `json:"is_active" example:"false"`
}

// Apply returns attrs with the fields of the input that were sent.
func (in UpdateServiceInput) Apply(attrs entities.ServiceAttributes) entities.ServiceAttributes {
	if in.CategoryID != nil {
		attrs.CategoryID = *in.CategoryID
	}
	if in.Name != nil {
		attrs.Details.Name = *in.Name
	}
	if in.Description != nil {
		attrs.Details.Description = *in.Description
	}
	if in.Icon != nil {
		attrs.Details.Icon = *in.Icon
	}
	if in.DisplayOrder != nil {
		attrs.Details.DisplayOrder = *in.DisplayOrder
	}
	if in.RequiresDestination != nil {
		attrs.RequiresDestination = *in.RequiresDestination
	}
	if in.Pricing != nil {
		attrs.Pricing = in.Pricing.toPricing()
	}
	if in.IsActive != nil {
		attrs.IsActive = *in.IsActive
	}
	return attrs
}

// Customer app

type CatalogService struct {
	ID                  uuid.UUID   `json:"id"`
	Slug                string      `json:"slug" example:"taxi"`
	Name                string      `json:"name" example:"Táxi"`
	Description         string      `json:"description" example:"Viagem até ao destino indicado."`
	Icon                string      `json:"icon" example:"taxi"`
	RequiresDestination bool        `json:"requires_destination" example:"true"`
	Pricing             PricingView `json:"pricing"`
}

type CatalogCategory struct {
	ID          uuid.UUID        `json:"id"`
	Slug        string           `json:"slug" example:"taxi"`
	Name        string           `json:"name" example:"Táxi"`
	Description string           `json:"description" example:"Transporte de passageiros até ao destino."`
	Icon        string           `json:"icon" example:"taxi"`
	Services    []CatalogService `json:"services"`
}

// Catalog is what the customer app shows: active categories, in display
// order, each with its active services. Categories without services are left
// out because nothing can be requested from them.
type Catalog struct {
	Categories []CatalogCategory `json:"categories"`
}

func NewCatalog(categories []*entities.ServiceCategory, services []*entities.Service) Catalog {
	byCategory := make(map[uuid.UUID][]CatalogService, len(categories))
	for _, s := range services {
		byCategory[s.CategoryID] = append(byCategory[s.CategoryID], CatalogService{
			ID: s.ID, Slug: s.Slug, Name: s.Name, Description: s.Description, Icon: s.Icon,
			RequiresDestination: s.RequiresDestination, Pricing: toPricingView(s.Pricing),
		})
	}
	out := Catalog{Categories: make([]CatalogCategory, 0, len(categories))}
	for _, c := range categories {
		offered := byCategory[c.ID]
		if len(offered) == 0 {
			continue
		}
		out.Categories = append(out.Categories, CatalogCategory{
			ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, Icon: c.Icon, Services: offered,
		})
	}
	return out
}
