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

type VoucherInput struct {
	Code              string     `json:"code" binding:"max=30" example:"TAXI10"`
	Description       string     `json:"description" binding:"max=300"`
	CategoryID        *uuid.UUID `json:"category_id"`
	ServiceID         *uuid.UUID `json:"service_id"`
	DiscountBps       int        `json:"discount_bps" binding:"min=0,max=10000" example:"1000"`
	DiscountCents     int64      `json:"discount_cents" binding:"min=0,max=10000000000"`
	MaxDiscountCents  *int64     `json:"max_discount_cents" binding:"omitempty,min=0,max=10000000000"`
	MinFareCents      int64      `json:"min_fare_cents" binding:"min=0,max=10000000000"`
	ValidFrom         time.Time  `json:"valid_from" binding:"required"`
	ValidUntil        time.Time  `json:"valid_until" binding:"required"`
	MaxRedemptions    *int       `json:"max_redemptions" binding:"omitempty,min=1,max=1000000000"`
	PerCustomerLimit  int        `json:"per_customer_limit" binding:"min=0,max=9999"`
	IsPublic          bool       `json:"is_public"`
	IsActive          *bool      `json:"is_active"`
	WorkshopID        *uuid.UUID `json:"workshop_id,omitempty"`
	WorkshopServiceID *uuid.UUID `json:"workshop_service_id,omitempty"`
	AutoApply         bool       `json:"auto_apply,omitempty"`
}

type VoucherView struct {
	ID                uuid.UUID  `json:"id"`
	Code              string     `json:"code,omitempty"`
	Description       string     `json:"description,omitempty"`
	CategoryID        *uuid.UUID `json:"category_id,omitempty"`
	ServiceID         *uuid.UUID `json:"service_id,omitempty"`
	DiscountBps       int        `json:"discount_bps"`
	DiscountCents     int64      `json:"discount_cents"`
	MaxDiscountCents  *int64     `json:"max_discount_cents,omitempty"`
	MinFareCents      int64      `json:"min_fare_cents"`
	ValidFrom         time.Time  `json:"valid_from"`
	ValidUntil        time.Time  `json:"valid_until"`
	MaxRedemptions    *int       `json:"max_redemptions,omitempty"`
	PerCustomerLimit  int        `json:"per_customer_limit"`
	IsPublic          bool       `json:"is_public"`
	IsActive          bool       `json:"is_active"`
	WorkshopID        *uuid.UUID `json:"workshop_id,omitempty"`
	WorkshopServiceID *uuid.UUID `json:"workshop_service_id,omitempty"`
	AutoApply         bool       `json:"auto_apply,omitempty"`
}

type VoucherList struct {
	Items []VoucherView `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

type OpenTicketInput struct {
	Subject string     `json:"subject" binding:"required,max=160" example:"Fui cobrado a mais"`
	Message string     `json:"message" binding:"required,max=4000"`
	TopicID *uuid.UUID `json:"topic_id"`
}

type TicketReplyInput struct {
	Message string `json:"message" binding:"required,max=4000"`
}

type TicketView struct {
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code" example:"TK-1001"`
	RequestID *uuid.UUID `json:"request_id,omitempty"`
	TopicID   *uuid.UUID `json:"topic_id,omitempty"`
	Subject   string     `json:"subject"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type TicketMessage struct {
	Sender    string    `json:"sender"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type TicketDetail struct {
	TicketView
	Messages []TicketMessage `json:"messages"`
}

type TicketList struct {
	Items []TicketView `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

type CustomerVehicleInput struct {
	Brand string `json:"brand" binding:"required,max=60" example:"Toyota"`
	Model string `json:"model" binding:"required,max=60" example:"Hilux"`
	Plate string `json:"plate" binding:"required,max=20" example:"LD-45-21-AF"`
	Year  int    `json:"year" binding:"required,min=1900,max=2200" example:"2019"`
	Fuel  string `json:"fuel" binding:"required,oneof=petrol diesel hybrid electric gas" example:"diesel"`
}

type CustomerVehicleView struct {
	ID    uuid.UUID `json:"id"`
	Brand string    `json:"brand"`
	Model string    `json:"model"`
	Plate string    `json:"plate"`
	Year  int       `json:"year"`
	Fuel  string    `json:"fuel"`
}

type CustomerVehicleList struct {
	Items []CustomerVehicleView `json:"items"`
}

type PhotoUploadInput struct {
	ContentType string `json:"content_type" binding:"required,max=60" example:"image/jpeg"`
	SizeBytes   int64  `json:"size_bytes" binding:"required,min=1,max=100000000" example:"204800"`
}

type PhotoUploadView struct {
	ID        uuid.UUID         `json:"id"`
	UploadURL string            `json:"upload_url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type AttachPhotosInput struct {
	PhotoIDs []uuid.UUID `json:"photo_ids" binding:"required,min=1,max=50"`
}

type PhotoView struct {
	ID          uuid.UUID `json:"id"`
	ContentType string    `json:"content_type"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type PhotoList struct {
	Items []PhotoView `json:"items"`
}

type RecommendedWorkshop struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Address   string    `json:"address"`
	DistanceM int       `json:"distance_m"`
}

type TowingOptions struct {
	VehicleTypes        []CatalogItemView    `json:"vehicle_types"`
	Incidents           []CatalogItemView    `json:"incidents"`
	RecommendedWorkshop *RecommendedWorkshop `json:"recommended_workshop,omitempty"`
}

type PreferencesView struct {
	NotifyStatus     bool   `json:"notify_status"`
	NotifyPromotions bool   `json:"notify_promotions"`
	NotifySMS        bool   `json:"notify_sms"`
	ShareLocation    bool   `json:"share_location"`
	Language         string `json:"language" binding:"required,oneof=pt en fr" enums:"pt,en,fr"`
	Currency         string `json:"currency" binding:"required,oneof=AOA USD EUR" enums:"AOA,USD,EUR"`
}

type AvatarInput struct {
	PhotoID uuid.UUID `json:"photo_id" binding:"required"`
}

type AvatarView struct {
	URL       string     `json:"url,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ReferralView struct {
	Code        string `json:"code" example:"AB2CD3EF"`
	RewardCents int64  `json:"reward_cents" example:"100000"`
	Invited     int64  `json:"invited"`
	Rewarded    int64  `json:"rewarded"`
	EarnedCents int64  `json:"earned_cents"`
}

type RedeemReferralInput struct {
	Code string `json:"code" binding:"required,max=20" example:"AB2CD3EF"`
}

type InboxItemView struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	LinkType  string     `json:"link_type,omitempty" enums:"request,offer,booking,ticket,wallet"`
	LinkID    *uuid.UUID `json:"link_id,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type InboxList struct {
	Items  []InboxItemView `json:"items"`
	Total  int64           `json:"total"`
	Unread int64           `json:"unread"`
	Page   int             `json:"page"`
	Size   int             `json:"size"`
}

type MarkedView struct {
	Marked int64 `json:"marked"`
}

type HelpTopicView struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
}

type RecentRequestView struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Category string    `json:"category"`
	Status   string    `json:"status"`
}

type HelpView struct {
	Topics        []HelpTopicView    `json:"topics"`
	SupportPhone  string             `json:"support_phone"`
	RecentRequest *RecentRequestView `json:"recent_request,omitempty"`
}
