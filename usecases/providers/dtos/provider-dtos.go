package dtos

import (
	"time"

	"github.com/google/uuid"
)

type RegisterProviderInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923000000"`
}

type RegisteredProvider struct {
	ID         uuid.UUID `json:"id"`
	Phone      string    `json:"phone"`
	Status     string    `json:"status" example:"draft"`
	IsVerified bool      `json:"is_verified"`
}

type OpenCategory struct {
	ID       uuid.UUID     `json:"id"`
	Slug     string        `json:"slug"`
	Name     string        `json:"name"`
	Services []OpenService `json:"services"`
}

type OpenService struct {
	ID              uuid.UUID `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	RequiresVehicle bool      `json:"requires_vehicle"`
	MinVehicleSeats int       `json:"min_vehicle_seats"`
}

type OpenServices struct {
	Categories []OpenCategory `json:"categories"`
}

type ProfileInput struct {
	ServiceID    uuid.UUID `json:"service_id" binding:"required"`
	Name         string    `json:"name" binding:"required,max=120" example:"Domingos Mateus Kiala"`
	BirthDate    string    `json:"birth_date" binding:"required" example:"1988-03-14"`
	IDNumber     string    `json:"id_number" binding:"required,max=32" example:"005487123LA041"`
	Province     string    `json:"province" binding:"required,max=120" example:"Luanda"`
	Municipality string    `json:"municipality" binding:"required,max=120" example:"Talatona"`
	Email        string    `json:"email" binding:"omitempty,max=254" example:"domingos@exemplo.ao"`
}

type VehicleInput struct {
	Kind       string `json:"kind" binding:"omitempty,oneof=car motorcycle van truck tow_truck" example:"car"`
	CapacityKg *int   `json:"capacity_kg" binding:"omitempty,min=0,max=100000" example:"1500"`
	Brand      string `json:"brand" binding:"required,max=120" example:"Toyota"`
	Model      string `json:"model" binding:"required,max=120" example:"Corolla"`
	Year       int    `json:"year" binding:"required,min=1900,max=2200" example:"2020"`
	Color      string `json:"color" binding:"required,max=120" example:"Branco"`
	Plate      string `json:"plate" binding:"required,max=20" example:"LD-45-21-AF"`
	Seats      int    `json:"seats" binding:"required,min=1,max=100" example:"5"`
}

type UploadURLInput struct {
	DocumentType string `json:"document_type" binding:"required,max=64" example:"id_card"`
	Slot         string `json:"slot" binding:"omitempty,max=32" example:"front"`
	ContentType  string `json:"content_type" binding:"required,max=64" example:"image/jpeg"`
	Size         int64  `json:"size" binding:"required,min=1" example:"204800"`
}

type UploadURLOutput struct {
	UploadURL string            `json:"upload_url"`
	ObjectKey string            `json:"object_key"`
	ExpiresAt time.Time         `json:"expires_at"`
	Headers   map[string]string `json:"headers"`
}

type RegisterDocumentInput struct {
	DocumentType string `json:"document_type" binding:"required,max=64" example:"id_card"`
	Slot         string `json:"slot" binding:"omitempty,max=32" example:"front"`
	ObjectKey    string `json:"object_key" binding:"required,max=255"`
}

type DocumentView struct {
	ID               uuid.UUID             `json:"id"`
	Type             string                `json:"type"`
	Subject          string                `json:"subject" example:"provider"`
	Name             string                `json:"name"`
	Status           string                `json:"status"`
	RejectionReason  string                `json:"rejection_reason,omitempty"`
	RejectionReasons []RejectionReasonView `json:"rejection_reasons,omitempty"`
	ReviewFlags      []string              `json:"review_flags"`
	ContentType      string                `json:"content_type"`
	SizeBytes        int64                 `json:"size_bytes"`
	ReviewedAt       *time.Time            `json:"reviewed_at,omitempty"`
	UpdatedAt        time.Time             `json:"updated_at"`
	URL              string                `json:"url,omitempty"`
	URLExpiresAt     *time.Time            `json:"url_expires_at,omitempty"`
	Files            []DocumentFileView    `json:"files"`
}

type DocumentFileView struct {
	ID             uuid.UUID  `json:"id"`
	Slot           string     `json:"slot" example:"front"`
	Version        int        `json:"version" example:"1"`
	Status         string     `json:"status" example:"accepted"`
	ContentType    string     `json:"content_type"`
	SizeBytes      int64      `json:"size_bytes"`
	RejectionCodes []string   `json:"rejection_codes"`
	URL            string     `json:"url,omitempty"`
	URLExpiresAt   *time.Time `json:"url_expires_at,omitempty"`
}

type DocumentURLOutput struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type VehicleView struct {
	Kind       string `json:"kind"`
	CapacityKg *int   `json:"capacity_kg,omitempty"`
	Brand      string `json:"brand"`
	Model      string `json:"model"`
	Year       int    `json:"year"`
	Color      string `json:"color"`
	Plate      string `json:"plate"`
	Seats      int    `json:"seats"`
}

type RequiredDocumentView struct {
	Type                string     `json:"type"`
	Name                string     `json:"name"`
	Scope               string     `json:"scope"`
	AllowedContentTypes []string   `json:"allowed_content_types"`
	MaxSizeBytes        int64      `json:"max_size_bytes"`
	Slots               []SlotView `json:"slots"`
}

type SlotView struct {
	Slot                string   `json:"slot" example:"front"`
	Required            bool     `json:"required"`
	AllowedContentTypes []string `json:"allowed_content_types"`
}

type ProviderView struct {
	UserID             uuid.UUID              `json:"user_id"`
	Name               string                 `json:"name"`
	Phone              string                 `json:"phone,omitempty"`
	Email              string                 `json:"email,omitempty"`
	Status             string                 `json:"status"`
	StatusReason       string                 `json:"status_reason,omitempty"`
	StatusReasons      []RejectionReasonView  `json:"status_reasons,omitempty"`
	OrganizationID     *uuid.UUID             `json:"organization_id,omitempty"`
	RequestedServiceID *uuid.UUID             `json:"requested_service_id,omitempty"`
	ServiceID          *uuid.UUID             `json:"service_id,omitempty"`
	BirthDate          string                 `json:"birth_date,omitempty"`
	IDNumber           string                 `json:"id_number,omitempty"`
	Province           string                 `json:"province,omitempty"`
	Municipality       string                 `json:"municipality,omitempty"`
	Vehicle            *VehicleView           `json:"vehicle,omitempty"`
	SubmittedAt        *time.Time             `json:"submitted_at,omitempty"`
	ReviewedAt         *time.Time             `json:"reviewed_at,omitempty"`
	RequiredDocuments  []RequiredDocumentView `json:"required_documents"`
	Documents          []DocumentView         `json:"documents"`
	Missing            map[string]string      `json:"missing"`
}

type ReviewDocumentInput struct {
	Status    string      `json:"status" binding:"required,oneof=approved rejected"`
	ReasonIDs []uuid.UUID `json:"reason_ids" binding:"omitempty,max=10"`
	Note      string      `json:"note" binding:"max=400"`
	Subject   string      `json:"subject" binding:"omitempty,oneof=provider legal_representative vehicle_owner" example:"provider"`
}

type ReviewProviderInput struct {
	Status    string      `json:"status" binding:"required,oneof=approved rejected suspended"`
	ServiceID *uuid.UUID  `json:"service_id"`
	ReasonIDs []uuid.UUID `json:"reason_ids" binding:"omitempty,max=10"`
	Note      string      `json:"note" binding:"max=400"`
}

type RejectionReasonView struct {
	ID           uuid.UUID  `json:"id"`
	Scope        string     `json:"scope" example:"document"`
	Code         string     `json:"code" example:"illegible"`
	Label        string     `json:"label" example:"Documento ilegível"`
	RequiresNote bool       `json:"requires_note"`
	IsActive     bool       `json:"is_active"`
	DisplayOrder int        `json:"display_order"`
	CategoryID   *uuid.UUID `json:"category_id,omitempty"`
}

type RejectionReasonList struct {
	Items []RejectionReasonView `json:"items"`
}

type CreateRejectionReasonInput struct {
	Scope        string     `json:"scope" binding:"required,oneof=document provider suspension customer_cancel provider_cancel offer_reject"`
	Code         string     `json:"code" binding:"required,max=40" example:"blurry_photo"`
	Label        string     `json:"label" binding:"required,max=120" example:"Fotografia desfocada"`
	RequiresNote bool       `json:"requires_note"`
	DisplayOrder int        `json:"display_order" binding:"min=0,max=9999"`
	CategoryID   *uuid.UUID `json:"category_id"`
}

type UpdateRejectionReasonInput struct {
	Label        string `json:"label" binding:"required,max=120"`
	RequiresNote bool   `json:"requires_note"`
	IsActive     bool   `json:"is_active"`
	DisplayOrder int    `json:"display_order" binding:"min=0,max=9999"`
}

type ApplicationFilter struct {
	Status         string
	OrganizationID *uuid.UUID
	ServiceID      *uuid.UUID
	Category       string
	Search         string
	// Sort is empty (oldest submission first) or earned_desc, which needs WithMoney.
	Sort string
	// WithMoney adds what each provider earned; only for finance:read.
	WithMoney bool
}

type ApplicationView struct {
	UserID               uuid.UUID  `json:"user_id"`
	Name                 string     `json:"name"`
	Phone                string     `json:"phone,omitempty"`
	Status               string     `json:"status"`
	RequestedServiceID   *uuid.UUID `json:"requested_service_id,omitempty"`
	RequestedServiceName string     `json:"requested_service_name,omitempty"`
	ServiceID            *uuid.UUID `json:"service_id,omitempty"`
	VehiclePlate         string     `json:"vehicle_plate,omitempty"`
	OrganizationID       *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName     string     `json:"organization_name,omitempty"`
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
	ProviderActivity
	// Only with finance:read, since the provider joined.
	*ProviderMoneyView
}

// ProviderActivity is what the provider did: the requests assigned to them that
// were completed or cancelled (by anyone), the last one and the rating.
type ProviderActivity struct {
	CompletedRequests int64      `json:"completed_requests" example:"40"`
	CancelledRequests int64      `json:"cancelled_requests" example:"2"`
	LastRequestAt     *time.Time `json:"last_request_at,omitempty"`
	RatingAvg         float64    `json:"rating_avg" example:"4.8"`
	RatingCount       int        `json:"rating_count" example:"31"`
}

// ProviderMoneyView: gross_cents is what the customers paid for the requests of
// the provider (with tips, fees and extras, net of refunds); earned_cents is what
// the provider earned from them, after the commission of AGORA, cash kept in
// hand included; balance_cents is what is still to settle (positive: AGORA owes
// the provider; negative: the provider owes AGORA).
type ProviderMoneyView struct {
	GrossCents   int64 `json:"gross_cents" example:"8500000"`
	EarnedCents  int64 `json:"earned_cents" example:"6800000"`
	BalanceCents int64 `json:"balance_cents" example:"-120000"`
}

// ProviderActivityView is the summary of a provider in the backoffice. Total and
// Period carry money only with finance:read; Period only when from or to was given.
type ProviderActivityView struct {
	ProviderActivity
	Total  *ProviderMoneyView `json:"total,omitempty"`
	Period *ProviderMoneyView `json:"period,omitempty"`
}

type ApplicationList struct {
	Items []ApplicationView `json:"items"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}
