package dtos

import (
	"github.com/google/uuid"

	addressdtos "github.com/Agora-Mobility/atlas/usecases/addresses/dtos"
)

type RegisterDriverInput struct {
	Name      string     `json:"name" binding:"required,max=120" example:"Pedro Cunga"`
	Phone     string     `json:"phone" binding:"required,max=32" example:"+244923000000"`
	Email     string     `json:"email" binding:"omitempty,max=254" example:"pedro@exemplo.ao"`
	ServiceID uuid.UUID  `json:"service_id" binding:"required"`
	VehicleID *uuid.UUID `json:"vehicle_id"`
}

type DriverView struct {
	UserID             uuid.UUID  `json:"user_id"`
	Name               string     `json:"name"`
	Phone              string     `json:"phone,omitempty"`
	IsVerified         bool       `json:"is_verified"`
	Status             string     `json:"status"`
	RequestedServiceID *uuid.UUID `json:"requested_service_id,omitempty"`
	ServiceID          *uuid.UUID `json:"service_id,omitempty"`
	ServiceName        string     `json:"service_name,omitempty"`
	VehicleID          *uuid.UUID `json:"vehicle_id,omitempty"`
	VehiclePlate       string     `json:"vehicle_plate,omitempty"`
}

type DriverList struct {
	Items []DriverView `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

type AssignVehicleInput struct {
	VehicleID uuid.UUID `json:"vehicle_id" binding:"required"`
}

type FleetVehicleInput struct {
	Kind       string     `json:"kind" binding:"required,oneof=car motorcycle van truck tow_truck" example:"tow_truck"`
	Brand      string     `json:"brand" binding:"required,max=120" example:"Isuzu"`
	Model      string     `json:"model" binding:"required,max=120" example:"NPR"`
	Year       int        `json:"year" binding:"required,min=1900,max=2200" example:"2020"`
	Color      string     `json:"color" binding:"required,max=120" example:"Branco"`
	Plate      string     `json:"plate" binding:"required,max=20" example:"LD-45-21-AF"`
	Seats      int        `json:"seats" binding:"required,min=1,max=100" example:"3"`
	CapacityKg *int       `json:"capacity_kg" binding:"omitempty,min=0,max=100000" example:"3500"`
	DriverID   *uuid.UUID `json:"driver_id"`
}

type UpdateVehicleInput struct {
	Kind       string `json:"kind" binding:"required,oneof=car motorcycle van truck tow_truck"`
	Brand      string `json:"brand" binding:"required,max=120"`
	Model      string `json:"model" binding:"required,max=120"`
	Year       int    `json:"year" binding:"required,min=1900,max=2200"`
	Color      string `json:"color" binding:"required,max=120"`
	Plate      string `json:"plate" binding:"required,max=20"`
	Seats      int    `json:"seats" binding:"required,min=1,max=100"`
	CapacityKg *int   `json:"capacity_kg" binding:"omitempty,min=0,max=100000"`
	IsActive   bool   `json:"is_active"`
}

type FleetVehicleView struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	DriverID       *uuid.UUID `json:"driver_id,omitempty"`
	DriverName     string     `json:"driver_name,omitempty"`
	Kind           string     `json:"kind"`
	Brand          string     `json:"brand"`
	Model          string     `json:"model"`
	Year           int        `json:"year"`
	Color          string     `json:"color"`
	Plate          string     `json:"plate"`
	Seats          int        `json:"seats"`
	CapacityKg     *int       `json:"capacity_kg,omitempty"`
	IsActive       bool       `json:"is_active"`
}

type VehicleList struct {
	Items []FleetVehicleView `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

type WorkshopInput struct {
	Name                string                   `json:"name" binding:"required,max=120" example:"Oficina Central"`
	Phone               string                   `json:"phone" binding:"max=32" example:"+244923000000"`
	OpeningHours        string                   `json:"opening_hours" binding:"max=200" example:"Seg-Sáb 08:00-18:00"`
	OffersHomeService   bool                     `json:"offers_home_service"`
	HomeServiceFeeCents int64                    `json:"home_service_fee_cents" binding:"min=0,max=10000000000"`
	Currency            string                   `json:"currency" binding:"required,len=3" example:"AOA"`
	IsActive            *bool                    `json:"is_active"`
	Address             addressdtos.AddressInput `json:"address" binding:"required"`
}

type WorkshopServiceInput struct {
	Name            string     `json:"name" binding:"required,max=120" example:"Mudança de óleo"`
	Description     string     `json:"description" binding:"max=500"`
	PriceCents      int64      `json:"price_cents" binding:"min=0,max=10000000000" example:"2500000"`
	DurationMinutes int        `json:"duration_minutes" binding:"min=0,max=10080" example:"45"`
	IsActive        *bool      `json:"is_active"`
	CategoryID      *uuid.UUID `json:"category_id"`
}

type WorkshopServiceView struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description,omitempty"`
	PriceCents      int64      `json:"price_cents"`
	DurationMinutes int        `json:"duration_minutes"`
	IsActive        bool       `json:"is_active"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty"`
}

type WorkshopView struct {
	ID                  uuid.UUID               `json:"id"`
	OrganizationID      uuid.UUID               `json:"organization_id"`
	Name                string                  `json:"name"`
	Phone               string                  `json:"phone,omitempty"`
	OpeningHours        string                  `json:"opening_hours,omitempty"`
	OffersHomeService   bool                    `json:"offers_home_service"`
	HomeServiceFeeCents int64                   `json:"home_service_fee_cents"`
	Currency            string                  `json:"currency"`
	IsActive            bool                    `json:"is_active"`
	Address             addressdtos.AddressView `json:"address"`
	Services            []WorkshopServiceView   `json:"services"`
}

type WorkshopList struct {
	Items []WorkshopView `json:"items"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}
