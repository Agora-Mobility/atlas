package dtos

import (
	"time"

	"github.com/google/uuid"
)

type RegisterProviderInput struct {
	Name     string `json:"name" binding:"required,max=120" example:"Pedro Cunga"`
	Phone    string `json:"phone" binding:"required,max=32" example:"+244923000000"`
	Email    string `json:"email" binding:"omitempty,max=254" example:"pedro@exemplo.ao"`
	Password string `json:"password" binding:"required,max=128" example:"S3gredo!forte"`
}

type RegisteredProvider struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
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

type RequestServiceInput struct {
	ServiceID uuid.UUID `json:"service_id" binding:"required"`
}

type PersonalDataInput struct {
	BirthDate string `json:"birth_date" binding:"required" example:"1990-05-20"`
	IDNumber  string `json:"id_number" binding:"required,max=120" example:"000000000LA000"`
	Address   string `json:"address" binding:"required,max=200" example:"Rua Rainha Ginga, 29, Luanda"`
}

type VehicleInput struct {
	Brand string `json:"brand" binding:"required,max=120" example:"Toyota"`
	Model string `json:"model" binding:"required,max=120" example:"Corolla"`
	Year  int    `json:"year" binding:"required" example:"2020"`
	Color string `json:"color" binding:"required,max=120" example:"Branco"`
	Plate string `json:"plate" binding:"required,max=20" example:"LD-45-21-AF"`
	Seats int    `json:"seats" binding:"required" example:"5"`
}

type UploadURLInput struct {
	DocumentType string `json:"document_type" binding:"required,max=64" example:"id_card"`
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
	ObjectKey    string `json:"object_key" binding:"required,max=255"`
}

type DocumentView struct {
	Type            string     `json:"type"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
	ContentType     string     `json:"content_type"`
	SizeBytes       int64      `json:"size_bytes"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	URL             string     `json:"url,omitempty"`
	URLExpiresAt    *time.Time `json:"url_expires_at,omitempty"`
}

type DocumentURLOutput struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type VehicleView struct {
	Brand string `json:"brand"`
	Model string `json:"model"`
	Year  int    `json:"year"`
	Color string `json:"color"`
	Plate string `json:"plate"`
	Seats int    `json:"seats"`
}

type RequiredDocumentView struct {
	Type                string   `json:"type"`
	Name                string   `json:"name"`
	Scope               string   `json:"scope"`
	AllowedContentTypes []string `json:"allowed_content_types"`
	MaxSizeBytes        int64    `json:"max_size_bytes"`
}

type ProfileView struct {
	UserID             uuid.UUID              `json:"user_id"`
	Name               string                 `json:"name"`
	Phone              string                 `json:"phone,omitempty"`
	Email              string                 `json:"email,omitempty"`
	Status             string                 `json:"status"`
	StatusReason       string                 `json:"status_reason,omitempty"`
	RequestedServiceID *uuid.UUID             `json:"requested_service_id,omitempty"`
	ServiceID          *uuid.UUID             `json:"service_id,omitempty"`
	BirthDate          string                 `json:"birth_date,omitempty"`
	IDNumber           string                 `json:"id_number,omitempty"`
	Address            string                 `json:"address,omitempty"`
	Vehicle            *VehicleView           `json:"vehicle,omitempty"`
	SubmittedAt        *time.Time             `json:"submitted_at,omitempty"`
	ReviewedAt         *time.Time             `json:"reviewed_at,omitempty"`
	RequiredDocuments  []RequiredDocumentView `json:"required_documents"`
	Documents          []DocumentView         `json:"documents"`
	Missing            map[string]string      `json:"missing"`
}

type ReviewDocumentInput struct {
	Status string `json:"status" binding:"required,oneof=approved rejected"`
	Reason string `json:"reason" binding:"max=500"`
}

type ReviewProviderInput struct {
	Status    string     `json:"status" binding:"required,oneof=approved rejected suspended"`
	ServiceID *uuid.UUID `json:"service_id"`
	Reason    string     `json:"reason" binding:"max=500"`
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
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
}

type ApplicationList struct {
	Items []ApplicationView `json:"items"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}
