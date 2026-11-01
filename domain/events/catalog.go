package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	ServiceCategoryAggregate = "service_category"
	ServiceAggregate         = "service"

	ServiceCategoryCreated = "catalog.category-created"
	ServiceCategoryUpdated = "catalog.category-updated"
	ServiceCreated         = "catalog.service-created"
	ServiceUpdated         = "catalog.service-updated"
)

type ServiceCategoryChange struct {
	CategoryID uuid.UUID  `json:"category_id"`
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	IsActive   bool       `json:"is_active"`
	ChangedBy  *uuid.UUID `json:"changed_by,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

func (e ServiceCategoryChange) AggregateID() string { return e.CategoryID.String() }

type ServiceCategoryCreatedV1 struct{ ServiceCategoryChange }

func (ServiceCategoryCreatedV1) EventType() string     { return ServiceCategoryCreated }
func (ServiceCategoryCreatedV1) EventVersion() int     { return 1 }
func (ServiceCategoryCreatedV1) AggregateType() string { return ServiceCategoryAggregate }

type ServiceCategoryUpdatedV1 struct{ ServiceCategoryChange }

func (ServiceCategoryUpdatedV1) EventType() string     { return ServiceCategoryUpdated }
func (ServiceCategoryUpdatedV1) EventVersion() int     { return 1 }
func (ServiceCategoryUpdatedV1) AggregateType() string { return ServiceCategoryAggregate }

// ServiceChange carries the whole service so consumers can keep a
// denormalised copy without calling back (ADR 0008).
type ServiceChange struct {
	ServiceID           uuid.UUID  `json:"service_id"`
	CategoryID          uuid.UUID  `json:"category_id"`
	Slug                string     `json:"slug"`
	Name                string     `json:"name"`
	RequiresDestination bool       `json:"requires_destination"`
	BaseFeeCents        int64      `json:"base_fee_cents"`
	PerKmFeeCents       int64      `json:"per_km_fee_cents"`
	PerMinuteFeeCents   int64      `json:"per_minute_fee_cents"`
	IsActive            bool       `json:"is_active"`
	ChangedBy           *uuid.UUID `json:"changed_by,omitempty"`
	OccurredAt          time.Time  `json:"occurred_at"`
}

func (e ServiceChange) AggregateID() string { return e.ServiceID.String() }

type ServiceCreatedV1 struct{ ServiceChange }

func (ServiceCreatedV1) EventType() string     { return ServiceCreated }
func (ServiceCreatedV1) EventVersion() int     { return 1 }
func (ServiceCreatedV1) AggregateType() string { return ServiceAggregate }

type ServiceUpdatedV1 struct{ ServiceChange }

func (ServiceUpdatedV1) EventType() string     { return ServiceUpdated }
func (ServiceUpdatedV1) EventVersion() int     { return 1 }
func (ServiceUpdatedV1) AggregateType() string { return ServiceAggregate }
