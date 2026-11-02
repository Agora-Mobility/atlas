package dtos

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CatalogItemInput struct {
	CategoryID       *uuid.UUID      `json:"category_id"`
	Code             string          `json:"code" binding:"required,max=40" example:"fast"`
	Label            string          `json:"label" binding:"required,max=200" example:"Chegou rápido"`
	Description      string          `json:"description" binding:"max=4000"`
	AmountCents      int64           `json:"amount_cents" binding:"min=0,max=10000000000"`
	PromoAmountCents *int64          `json:"promo_amount_cents" binding:"omitempty,min=0,max=10000000000"`
	PromoUntil       *time.Time      `json:"promo_until"`
	Attrs            json.RawMessage `json:"attrs" swaggertype:"object"`
	DisplayOrder     int             `json:"display_order" binding:"min=0,max=9999"`
	IsActive         *bool           `json:"is_active"`
}

type CatalogItemView struct {
	ID               uuid.UUID       `json:"id"`
	Kind             string          `json:"kind"`
	CategoryID       *uuid.UUID      `json:"category_id,omitempty"`
	Category         string          `json:"category,omitempty"`
	Code             string          `json:"code"`
	Label            string          `json:"label"`
	Description      string          `json:"description,omitempty"`
	AmountCents      int64           `json:"amount_cents"`
	PromoAmountCents *int64          `json:"promo_amount_cents,omitempty"`
	PromoUntil       *time.Time      `json:"promo_until,omitempty"`
	Attrs            json.RawMessage `json:"attrs,omitempty" swaggertype:"object"`
	DisplayOrder     int             `json:"display_order"`
	IsActive         bool            `json:"is_active"`
}

type CatalogItemList struct {
	Items []CatalogItemView `json:"items"`
}

type SettingInput struct {
	Value string `json:"value" binding:"max=200" example:"5000000"`
}

type SettingList struct {
	Items map[string]string `json:"items"`
}

type RatingOption struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code"`
	Label string    `json:"label"`
}

type TipPreset struct {
	Label       string `json:"label"`
	AmountCents int64  `json:"amount_cents"`
}

type RatingOptions struct {
	Compliments []RatingOption `json:"compliments"`
	Tips        []TipPreset    `json:"tips"`
	MaxTipCents int64          `json:"max_tip_cents"`
}

type RateInput struct {
	Stars         int         `json:"stars" binding:"required,min=1,max=5" example:"5"`
	ComplimentIDs []uuid.UUID `json:"compliment_ids" binding:"max=20"`
	Comment       string      `json:"comment" binding:"max=1000"`
}

type RatingView struct {
	ID          uuid.UUID      `json:"id"`
	RequestID   *uuid.UUID     `json:"request_id,omitempty"`
	Stars       int            `json:"stars"`
	Compliments []RatingOption `json:"compliments"`
	Comment     string         `json:"comment,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type RatingList struct {
	Average float64      `json:"average"`
	Items   []RatingView `json:"items"`
	Total   int64        `json:"total"`
	Page    int          `json:"page"`
	Size    int          `json:"size"`
}
