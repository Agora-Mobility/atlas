package dtos

import (
	"time"

	"github.com/google/uuid"
)

type ProgressView struct {
	Completed int `json:"completed" example:"1"`
	Total     int `json:"total" example:"8"`
	Percent   int `json:"percent" example:"12"`
}

type ItemView struct {
	Key    string `json:"key" example:"identity_card"`
	Status string `json:"status" example:"completed"`
}

type GroupView struct {
	Key              string     `json:"key" example:"about_you"`
	Completed        int        `json:"completed" example:"1"`
	Total            int        `json:"total" example:"4"`
	EstimatedMinutes int        `json:"estimated_minutes" example:"4"`
	Items            []ItemView `json:"items"`
}

type VerificationView struct {
	ID                   uuid.UUID    `json:"id"`
	Status               string       `json:"status" example:"in_progress"`
	Type                 string       `json:"type" example:"driver"`
	Locked               bool         `json:"locked"`
	Progress             ProgressView `json:"progress"`
	EstimatedMinutesLeft int          `json:"estimated_minutes_left" example:"10"`
	NextItem             *string      `json:"next_item" example:"selfie"`
	Groups               []GroupView  `json:"groups"`
	SubmittedAt          *time.Time   `json:"submitted_at,omitempty"`
}

type StartUploadInput struct {
	DocumentType string `json:"document_type" binding:"required,max=64" example:"id_card"`
	Slot         string `json:"slot" binding:"required,max=32" example:"front"`
	Subject      string `json:"subject" binding:"omitempty,oneof=provider legal_representative vehicle_owner" example:"provider"`
	ContentType  string `json:"content_type" binding:"required,max=64" example:"image/jpeg"`
	Size         int64  `json:"size" binding:"required,min=1" example:"204800"`
}

type StartedUpload struct {
	UploadID  uuid.UUID         `json:"upload_id"`
	UploadURL string            `json:"upload_url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
	Version   int               `json:"version" example:"1"`
}

type QualityView struct {
	TextLegible     *bool `json:"text_legible,omitempty"`
	NoShadowOnData  *bool `json:"no_shadow_on_data,omitempty"`
	CornersVisible  *bool `json:"corners_visible,omitempty"`
	NoGlare         *bool `json:"no_glare,omitempty"`
	Sharp           *bool `json:"sharp,omitempty"`
	WellLit         *bool `json:"well_lit,omitempty"`
	MetadataRemoved bool  `json:"metadata_removed"`
}

type UploadView struct {
	ID           uuid.UUID   `json:"id"`
	DocumentType string      `json:"document_type" example:"id_card"`
	Subject      string      `json:"subject" example:"provider"`
	Slot         string      `json:"slot" example:"front"`
	Version      int         `json:"version" example:"1"`
	Status       string      `json:"status" example:"accepted"`
	ContentType  string      `json:"content_type" example:"image/jpeg"`
	SizeBytes    int64       `json:"size_bytes"`
	Width        *int        `json:"width,omitempty"`
	Height       *int        `json:"height,omitempty"`
	Quality      QualityView `json:"quality"`
	Reasons      []string    `json:"reasons"`
	CreatedAt    time.Time   `json:"created_at"`
}

type ReadURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SubjectInput struct {
	Subject string `json:"subject" binding:"omitempty,oneof=provider legal_representative" example:"provider"`
}

type ExtractionView struct {
	Status              string             `json:"status" example:"processing"`
	Attempts            int                `json:"attempts" example:"1"`
	MaxAttempts         int                `json:"max_attempts" example:"3"`
	ManualEntryAllowed  bool               `json:"manual_entry_allowed"`
	Fields              map[string]string  `json:"fields,omitempty"`
	Confidence          map[string]float64 `json:"confidence,omitempty"`
	Failure             string             `json:"failure,omitempty"`
	RequiresRenewalSlip bool               `json:"requires_renewal_receipt"`
}

type IdentityInput struct {
	Subject   string `json:"subject" binding:"omitempty,oneof=provider legal_representative vehicle_owner" example:"provider"`
	Source    string `json:"source" binding:"required,oneof=ocr manual" example:"ocr"`
	FullName  string `json:"full_name" binding:"required,max=160" example:"Domingos Mateus Kiala"`
	Number    string `json:"number" binding:"required,max=32" example:"005487123LA041"`
	BirthDate string `json:"birth_date" binding:"required" example:"1988-03-14"`
	IssuedAt  string `json:"issued_at" binding:"required" example:"2022-01-10"`
	ExpiresAt string `json:"expires_at" binding:"required" example:"2032-01-10"`
}

type LivenessSessionView struct {
	SessionID   string    `json:"session_id"`
	Challenge   string    `json:"challenge" example:"BLINK"`
	ExpiresAt   time.Time `json:"expires_at"`
	Attempts    int       `json:"attempts" example:"0"`
	MaxAttempts int       `json:"max_attempts" example:"3"`
}

type SelfieInput struct {
	Subject   string    `json:"subject" binding:"omitempty,oneof=provider legal_representative" example:"provider"`
	SessionID string    `json:"session_id" binding:"required,max=128"`
	UploadID  uuid.UUID `json:"upload_id" binding:"required"`
}

type SelfieView struct {
	Status      string `json:"status" example:"accepted"`
	Attempts    int    `json:"attempts" example:"1"`
	MaxAttempts int    `json:"max_attempts" example:"3"`
	Reason      string `json:"reason,omitempty"`
}

type DrivingLicenseInput struct {
	Categories []string `json:"categories" binding:"required,min=1,max=13,dive,max=4" example:"B"`
	Number     string   `json:"number" binding:"required,max=32" example:"LA-123456"`
	ExpiresAt  string   `json:"expires_at" binding:"required" example:"2030-06-30"`
}

type CriminalRecordInput struct {
	CertificateNumber string `json:"certificate_number" binding:"required,max=64" example:"RC-2026-118273"`
	IssuedAt          string `json:"issued_at" binding:"required" example:"2026-10-20"`
}

type ReminderView struct {
	RemindAt time.Time `json:"remind_at"`
}

type VehicleInput struct {
	Plate      string `json:"plate" binding:"required,max=20" example:"LD-45-21-AF"`
	Brand      string `json:"brand" binding:"required,max=120" example:"Toyota"`
	Model      string `json:"model" binding:"required,max=120" example:"Corolla"`
	Year       int    `json:"year" binding:"required,min=1900,max=2200" example:"2020"`
	Color      string `json:"color" binding:"required,max=120" example:"Branco"`
	Seats      int    `json:"seats" binding:"required,min=1,max=100" example:"5"`
	Kind       string `json:"kind" binding:"omitempty,max=32" example:"car"`
	CapacityKg *int   `json:"capacity_kg" binding:"omitempty,min=0" example:"0"`
}

type VehicleRegistrationInput struct {
	OwnerName string `json:"owner_name" binding:"required,max=160" example:"Domingos Mateus Kiala"`
}

type VehicleRegistrationView struct {
	Plate                  string `json:"plate" example:"LD-45-21-AF"`
	OwnerName              string `json:"owner_name"`
	OwnerIsProvider        bool   `json:"owner_is_provider"`
	OwnerDocumentsRequired bool   `json:"owner_documents_required"`
	Status                 string `json:"status" example:"completed"`
}

type InsuranceInput struct {
	InsurerID           *uuid.UUID `json:"insurer_id" binding:"required_without=OtherInsurerName,excluded_with=OtherInsurerName"`
	OtherInsurerName    string     `json:"other_insurer_name" binding:"required_without=InsurerID,max=120"`
	InsuranceExpiresAt  string     `json:"insurance_expires_at" binding:"required" example:"2027-03-31"`
	InspectionExpiresAt string     `json:"inspection_expires_at" binding:"required" example:"2027-01-15"`
}

type BankAccountInput struct {
	BankID     uuid.UUID `json:"bank_id" binding:"required"`
	IBAN       string    `json:"iban" binding:"required,max=40" example:"AO06 0040 0000 1234 5678 1014 2"`
	HolderName string    `json:"holder_name" binding:"required,max=160" example:"Domingos Mateus Kiala"`
}

type BankView struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code" example:"0040"`
	ShortName string    `json:"short_name" example:"BAI"`
	Name      string    `json:"name" example:"Banco Angolano de Investimentos"`
}

type BankAccountView struct {
	Bank       BankView `json:"bank"`
	IBAN       string   `json:"iban" example:"AO06 •••• 1014 2"`
	HolderName string   `json:"holder_name"`
	OwnerType  string   `json:"owner_type" example:"person"`
	Status     string   `json:"status" example:"completed"`
}

type BusinessLicenseInput struct {
	Number      string `json:"number" binding:"required,max=64" example:"ALV-2026-00981"`
	ExpiresAt   string `json:"expires_at" binding:"required" example:"2027-12-31"`
	CompanyName string `json:"company_name" binding:"omitempty,max=160" example:"Auto Kiala, Lda"`
}

type CommercialCertificateInput struct {
	Code     string `json:"code" binding:"required,max=64" example:"0123-4567-8901"`
	IssuedAt string `json:"issued_at" binding:"required" example:"2026-09-01"`
}

type TaxIDInput struct {
	NIF         string `json:"nif" binding:"required,max=20" example:"5417123456"`
	CompanyName string `json:"company_name" binding:"required,max=160" example:"Auto Kiala, Lda"`
	CompanyType string `json:"company_type" binding:"required,max=40" example:"limited_company"`
}

type OpeningDayInput struct {
	Day    *int   `json:"day" binding:"required,min=0,max=6" example:"1"`
	Open   string `json:"open" binding:"omitempty,len=5" example:"08:00"`
	Close  string `json:"close" binding:"omitempty,len=5" example:"18:00"`
	Closed bool   `json:"closed"`
}

type LocationInput struct {
	Address      string            `json:"address" binding:"required,max=255" example:"Rua Rainha Ginga, 45, Luanda"`
	Reference    string            `json:"reference" binding:"max=255" example:"Ao lado da farmácia"`
	Lat          *float64          `json:"lat" binding:"required,min=-90,max=90" example:"-8.8147"`
	Lng          *float64          `json:"lng" binding:"required,min=-180,max=180" example:"13.2302"`
	OpeningHours []OpeningDayInput `json:"opening_hours" binding:"required,min=1,max=7,dive"`
}

type ServicePriceInput struct {
	ServiceID uuid.UUID `json:"service_id" binding:"required"`
	BasePrice int64     `json:"base_price" binding:"required,min=1" example:"15000"`
}

type ServicesInput struct {
	Services []ServicePriceInput `json:"services" binding:"required,min=1,max=50,dive"`
}

type WorkshopServiceView struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code" example:"oil"`
	Label       string    `json:"label" example:"Óleo e filtros"`
	Description string    `json:"description,omitempty"`
}

type PricedServiceView struct {
	ServiceID uuid.UUID `json:"service_id"`
	Label     string    `json:"label"`
	BasePrice int64     `json:"base_price" example:"15000"`
}

type OpeningDayView struct {
	Day    int    `json:"day" example:"1"`
	Open   string `json:"open,omitempty" example:"08:00"`
	Close  string `json:"close,omitempty" example:"18:00"`
	Closed bool   `json:"closed"`
}

type LocationView struct {
	Address      string           `json:"address"`
	Reference    string           `json:"reference,omitempty"`
	Lat          *float64         `json:"lat"`
	Lng          *float64         `json:"lng"`
	OpeningHours []OpeningDayView `json:"opening_hours"`
}

type StepView struct {
	Item   string `json:"item" example:"driving_license"`
	Status string `json:"status" example:"completed"`
}

type InsurerView struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name" example:"ENSA"`
}

type CatalogList[T any] struct {
	Items []T `json:"items"`
}

type OptionView struct {
	Code  string `json:"code" example:"limited_company"`
	Label string `json:"label" example:"Sociedade por quotas (Lda)"`
}

type ReviewSection struct {
	Key    string         `json:"key" example:"personal"`
	Step   string         `json:"step" example:"identity_card"`
	Status string         `json:"status" example:"completed"`
	Fields map[string]any `json:"fields"`
}

type DocumentsSummary struct {
	Sent  int        `json:"sent" example:"8"`
	Total int        `json:"total" example:"8"`
	Items []ItemView `json:"items"`
}

type ReviewView struct {
	Status      string           `json:"status" example:"in_progress"`
	Type        string           `json:"type" example:"driver"`
	Declaration string           `json:"declaration"`
	Sections    []ReviewSection  `json:"sections"`
	Documents   DocumentsSummary `json:"documents"`
	Missing     []string         `json:"missing_items"`
	SubmittedAt *time.Time       `json:"submitted_at,omitempty"`
}

type SubmitInput struct {
	DeclarationAccepted bool `json:"declaration_accepted" example:"true"`
}
