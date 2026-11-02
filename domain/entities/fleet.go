package entities

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	VehicleKindCar        = "car"
	VehicleKindMotorcycle = "motorcycle"
	VehicleKindVan        = "van"
	VehicleKindTruck      = "truck"
	VehicleKindTowTruck   = "tow_truck"

	maxWorkshopTextLength = 120
	maxWorkshopHours      = 200
	maxServiceDescription = 500
	maxServiceMinutes     = 10_080

	MaxVehicleCapacityKg = 100_000
)

func IsVehicleKind(kind string) bool {
	switch kind {
	case VehicleKindCar, VehicleKindMotorcycle, VehicleKindVan, VehicleKindTruck, VehicleKindTowTruck:
		return true
	}
	return false
}

type FleetVehicle struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	ProviderID     *uuid.UUID
	ProviderName   string
	Vehicle        Vehicle
	IsActive       bool
	CreatedAt      time.Time
}

type Workshop struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	Address             Address
	Name                string
	Phone               string
	OpeningHours        string
	OffersHomeService   bool
	HomeServiceFeeCents int64
	Currency            string
	IsActive            bool
	Services            []WorkshopService
}

func (w *Workshop) Normalize() error {
	w.Name = strings.TrimSpace(w.Name)
	w.Phone = strings.TrimSpace(w.Phone)
	w.OpeningHours = strings.TrimSpace(w.OpeningHours)
	w.Currency = strings.ToUpper(strings.TrimSpace(w.Currency))
	if w.Name == "" || len([]rune(w.Name)) > maxWorkshopTextLength {
		return validationError("name", "O nome é obrigatório e tem no máximo 120 caracteres.")
	}
	if len([]rune(w.OpeningHours)) > maxWorkshopHours {
		return validationError("opening_hours", "O horário tem no máximo 200 caracteres.")
	}
	if len(w.Currency) != 3 {
		return validationError("currency", "A moeda tem de ter 3 letras.")
	}
	if w.HomeServiceFeeCents < 0 || w.HomeServiceFeeCents > MaxFeeCents {
		return validationError("home_service_fee_cents", "A taxa ao domicílio não é válida.")
	}
	address, err := w.Address.Normalized("address")
	if err != nil {
		return err
	}
	if address.Line == "" {
		return validationError("address.line", "Indica a morada da oficina.")
	}
	w.Address = address
	return nil
}

type WorkshopService struct {
	ID              uuid.UUID
	WorkshopID      uuid.UUID
	Name            string
	Description     string
	PriceCents      int64
	DurationMinutes int
	IsActive        bool
}

func (s *WorkshopService) Normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	if s.Name == "" || len([]rune(s.Name)) > maxWorkshopTextLength {
		return validationError("name", "O nome é obrigatório e tem no máximo 120 caracteres.")
	}
	if len([]rune(s.Description)) > maxServiceDescription {
		return validationError("description", "A descrição tem no máximo 500 caracteres.")
	}
	if s.PriceCents < 0 || s.PriceCents > MaxFeeCents {
		return validationError("price_cents", "O preço não é válido.")
	}
	if s.DurationMinutes < 0 || s.DurationMinutes > maxServiceMinutes {
		return validationError("duration_minutes", "A duração tem de estar entre 0 e 10 080 minutos.")
	}
	return nil
}
