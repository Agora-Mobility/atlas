package dtos

import (
	"time"

	"github.com/google/uuid"
)

type PayInput struct {
	Method      string `json:"method" binding:"required,oneof=gpo reference" example:"gpo"`
	PhoneNumber string `json:"phone_number" binding:"max=20" example:"923000000"`
}

type ChangeMethodInput struct {
	Method string `json:"method" binding:"required,oneof=cash gpo reference" example:"gpo"`
}

type RefundInput struct {
	AmountCents int64  `json:"amount_cents" binding:"min=0" example:"150000"`
	Reason      string `json:"reason" binding:"required,max=300" example:"Cobrança em duplicado"`
}

type SetMethodInput struct {
	CategoryID uuid.UUID `json:"category_id" binding:"required"`
	Method     string    `json:"method" binding:"required,oneof=cash gpo reference"`
	IsEnabled  bool      `json:"is_enabled"`
}

type ReferenceView struct {
	Number string     `json:"number" example:"968481213"`
	Entity string     `json:"entity" example:"00348"`
	DueAt  *time.Time `json:"due_at,omitempty"`
}

type PaymentView struct {
	ID             uuid.UUID      `json:"id"`
	RequestID      uuid.UUID      `json:"request_id"`
	Method         string         `json:"method"`
	Status         string         `json:"status"`
	AmountCents    int64          `json:"amount_cents"`
	RefundedCents  int64          `json:"refunded_cents"`
	Currency       string         `json:"currency"`
	Code           string         `json:"code"`
	PayerPhone     string         `json:"payer_phone,omitempty"`
	Reference      *ReferenceView `json:"reference,omitempty"`
	FailureMessage string         `json:"failure_message,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	PaidAt         *time.Time     `json:"paid_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type PaymentList struct {
	Items []PaymentView `json:"items"`
}

type MethodView struct {
	CategoryID   uuid.UUID `json:"category_id"`
	CategorySlug string    `json:"category_slug"`
	CategoryName string    `json:"category_name"`
	Method       string    `json:"method"`
	IsEnabled    bool      `json:"is_enabled"`
}

type MethodList struct {
	Items []MethodView `json:"items"`
}

type AdminFilter struct {
	Statuses  []string
	Method    string
	Category  string
	RequestID *uuid.UUID
	From      *time.Time
	To        *time.Time
}

type AdminPaymentView struct {
	PaymentView
	Category         string    `json:"category"`
	CustomerID       uuid.UUID `json:"customer_id"`
	CustomerName     string    `json:"customer_name"`
	ProviderChargeID *string   `json:"provider_charge_id,omitempty"`
}

type AdminPaymentList struct {
	Items []AdminPaymentView `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

type EventView struct {
	Source     string    `json:"source"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type RefundView struct {
	ID          uuid.UUID  `json:"id"`
	AmountCents int64      `json:"amount_cents"`
	Reason      string     `json:"reason"`
	ActorID     *uuid.UUID `json:"actor_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type AdminPaymentDetail struct {
	Payment          PaymentView  `json:"payment"`
	ProviderChargeID *string      `json:"provider_charge_id,omitempty"`
	Events           []EventView  `json:"events"`
	Refunds          []RefundView `json:"refunds"`
}
