package dtos

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AuditListQuery struct {
	ActorID  string `form:"actor_id" binding:"omitempty,uuid"`
	Entity   string `form:"entity" binding:"max=80"`
	EntityID string `form:"entity_id" binding:"max=120"`
	Action   string `form:"action" binding:"max=120"`
	From     string `form:"from"`
	To       string `form:"to"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	Size     int    `form:"size" binding:"omitempty,min=1,max=100"`
}

type AuditActorView struct {
	ID   *uuid.UUID `json:"id,omitempty"`
	Name string     `json:"name,omitempty"`
	Type string     `json:"type" enums:"staff,customer,provider,system"`
}

type AuditEntryView struct {
	ID             uuid.UUID       `json:"id"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Actor          AuditActorView  `json:"actor"`
	OrganizationID *uuid.UUID      `json:"organization_id,omitempty"`
	Action         string          `json:"action" example:"service_tariff.publish"`
	Entity         string          `json:"entity" example:"service_tariff"`
	EntityID       string          `json:"entity_id"`
	Before         json.RawMessage `json:"before,omitempty" swaggertype:"object"`
	After          json.RawMessage `json:"after,omitempty" swaggertype:"object"`
	Reason         string          `json:"reason,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
}

type AuditPage struct {
	Items      []AuditEntryView `json:"items"`
	Page       int              `json:"page"`
	Size       int              `json:"size"`
	Total      int64            `json:"total"`
	TotalPages int64            `json:"total_pages"`
}
