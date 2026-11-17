package dtos

import (
	"time"

	"github.com/google/uuid"
)

type HoursInput struct {
	Weekday     int `json:"weekday" binding:"min=0,max=6" example:"1"`
	OpenMinute  int `json:"open_minute" binding:"min=0,max=1439" example:"480"`
	CloseMinute int `json:"close_minute" binding:"min=1,max=1440" example:"1080"`
}

type ScheduleInput struct {
	Capacity int          `json:"capacity" binding:"required,min=1,max=500" example:"2"`
	About    string       `json:"about" binding:"max=4000"`
	Warranty string       `json:"warranty" binding:"max=4000" example:"3 meses em peças e mão de obra"`
	Hours    []HoursInput `json:"hours" binding:"max=7,dive"`
}

type ClosureInput struct {
	Date   string `json:"date" binding:"required,len=10" example:"2026-12-25"`
	Reason string `json:"reason" binding:"max=200" example:"Feriado"`
}

type MechanicInput struct {
	Name string `json:"name" binding:"required,max=120" example:"Domingos Paulo"`
}

type MechanicView struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type MechanicList struct {
	Items []MechanicView `json:"items"`
}

type PromotionView struct {
	ID            uuid.UUID  `json:"id"`
	Description   string     `json:"description"`
	DiscountBps   int        `json:"discount_bps,omitempty"`
	DiscountCents int64      `json:"discount_cents,omitempty"`
	ServiceID     *uuid.UUID `json:"service_id,omitempty"`
	ValidUntil    time.Time  `json:"valid_until"`
}

type WorkshopCardView struct {
	ID                  uuid.UUID       `json:"id"`
	Name                string          `json:"name"`
	Address             string          `json:"address"`
	Lat                 float64         `json:"lat"`
	Lng                 float64         `json:"lng"`
	DistanceM           int             `json:"distance_m"`
	Rating              float64         `json:"rating"`
	RatingCount         int             `json:"rating_count"`
	OffersHomeService   bool            `json:"offers_home_service"`
	HomeServiceFeeCents int64           `json:"home_service_fee_cents"`
	Currency            string          `json:"currency"`
	IsOpen              bool            `json:"is_open"`
	OpenUntil           *time.Time      `json:"open_until,omitempty"`
	IsFavorite          bool            `json:"is_favorite"`
	CoverURL            string          `json:"cover_url,omitempty"`
	Promotions          []PromotionView `json:"promotions"`
}

type WorkshopPhotoView struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
}

type WorkshopPhotoList struct {
	Items []WorkshopPhotoView `json:"items"`
}

type WorkshopPhotosInput struct {
	PhotoIDs []uuid.UUID `json:"photo_ids" binding:"max=50,unique"`
}

type WorkshopCardList struct {
	Items []WorkshopCardView `json:"items"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

type WorkshopServiceView struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	PriceCents      int64     `json:"price_cents"`
	DurationMinutes int       `json:"duration_minutes"`
}

type WorkshopDetailView struct {
	WorkshopCardView
	Phone    string                `json:"-"`
	About    string                `json:"about"`
	Warranty string                `json:"warranty"`
	Capacity int                   `json:"capacity"`
	Hours    []HoursInput          `json:"hours"`
	Services []WorkshopServiceView `json:"services"`
	Photos   []WorkshopPhotoView   `json:"photos"`
}

type ReviewView struct {
	ID        uuid.UUID `json:"id"`
	Stars     int       `json:"stars"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ReviewList struct {
	Average float64      `json:"average"`
	Items   []ReviewView `json:"items"`
	Total   int64        `json:"total"`
	Page    int          `json:"page"`
	Size    int          `json:"size"`
}

type SlotView struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type DayView struct {
	Date   string     `json:"date"`
	Status string     `json:"status" enums:"free,closed,full"`
	Slots  []SlotView `json:"slots,omitempty"`
}

type CalendarView struct {
	DurationMinutes int       `json:"duration_minutes"`
	Days            []DayView `json:"days"`
}

type CreateBookingInput struct {
	WorkshopID    uuid.UUID   `json:"workshop_id" binding:"required"`
	ServiceIDs    []uuid.UUID `json:"service_ids" binding:"required,min=1,max=20"`
	VehicleID     uuid.UUID   `json:"vehicle_id" binding:"required"`
	StartsAt      time.Time   `json:"starts_at" binding:"required"`
	AtHome        bool        `json:"at_home"`
	HomeLat       *float64    `json:"home_lat" binding:"omitempty,latitude"`
	HomeLng       *float64    `json:"home_lng" binding:"omitempty,longitude"`
	HomeAddress   string      `json:"home_address" binding:"max=200"`
	Description   string      `json:"description" binding:"max=2000"`
	MileageKm     *int        `json:"mileage_km" binding:"omitempty,min=0,max=5000000"`
	PaymentMethod string      `json:"payment_method" binding:"omitempty,oneof=gpo reference wallet" enums:"gpo,reference,wallet"`
}

type BookingItemView struct {
	ServiceID       uuid.UUID `json:"service_id"`
	Name            string    `json:"name"`
	PriceCents      int64     `json:"price_cents"`
	DurationMinutes int       `json:"duration_minutes"`
}

type ExtraView struct {
	ID          uuid.UUID  `json:"id"`
	Description string     `json:"description"`
	AmountCents int64      `json:"amount_cents"`
	Status      string     `json:"status" enums:"pending,approved,declined"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

type VehicleSummary struct {
	Brand string `json:"brand"`
	Model string `json:"model"`
	Plate string `json:"plate"`
}

type BookingView struct {
	ID              uuid.UUID         `json:"id"`
	RequestID       uuid.UUID         `json:"request_id"`
	Code            string            `json:"code"`
	Status          string            `json:"status" enums:"awaiting_payment,confirmed,vehicle_received,in_service,ready,completed,cancelled"`
	WorkshopID      uuid.UUID         `json:"workshop_id"`
	WorkshopName    string            `json:"workshop_name"`
	Vehicle         *VehicleSummary   `json:"vehicle,omitempty"`
	AtHome          bool              `json:"at_home"`
	StartsAt        time.Time         `json:"starts_at"`
	EndsAt          time.Time         `json:"ends_at"`
	ExpectedReadyAt *time.Time        `json:"expected_ready_at,omitempty"`
	Mechanic        string            `json:"mechanic,omitempty"`
	Description     string            `json:"description,omitempty"`
	MileageKm       *int              `json:"mileage_km,omitempty"`
	Items           []BookingItemView `json:"items"`
	Extras          []ExtraView       `json:"extras"`
	ServicesCents   int64             `json:"services_cents"`
	HomeFeeCents    int64             `json:"home_fee_cents"`
	ExtrasCents     int64             `json:"extras_cents"`
	DiscountCents   int64             `json:"discount_cents"`
	Promotion       string            `json:"promotion,omitempty"`
	TotalCents      int64             `json:"total_cents"`
	AmountDueCents  int64             `json:"amount_due_cents"`
	Currency        string            `json:"currency"`
	PaymentMethod   string            `json:"payment_method"`
	PaymentStatus   string            `json:"payment_status"`
	CreatedAt       time.Time         `json:"created_at"`
}

type BookingList struct {
	Items []BookingView `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

type CancelBookingInput struct {
	ReasonID uuid.UUID `json:"reason_id" binding:"required"`
	Note     string    `json:"note" binding:"max=600"`
}

type MoveBookingInput struct {
	Status string `json:"status" binding:"required,oneof=vehicle_received in_service ready" enums:"vehicle_received,in_service,ready"`
}

type AssignInput struct {
	MechanicID      *uuid.UUID `json:"mechanic_id"`
	ExpectedReadyAt *time.Time `json:"expected_ready_at"`
}

type ExtraInput struct {
	Description string `json:"description" binding:"required,max=300" example:"Trocar pastilhas de travão"`
	AmountCents int64  `json:"amount_cents" binding:"required,min=1,max=10000000000" example:"1200000"`
}

type AnswerExtraInput struct {
	Approve *bool `json:"approve" binding:"required"`
}

type MessageInput struct {
	Body string `json:"body" binding:"required,max=1000" example:"A viatura já está pronta."`
}

type MessageView struct {
	ID        uuid.UUID `json:"id"`
	Sender    string    `json:"sender" enums:"customer,workshop"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type MessageList struct {
	Items []MessageView `json:"items"`
}

type CallView struct {
	Number    string     `json:"number" example:"+244923000111"`
	Masked    bool       `json:"masked"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
