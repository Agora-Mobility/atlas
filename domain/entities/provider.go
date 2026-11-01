package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProviderStatus string

const (
	ProviderDraft     ProviderStatus = "draft"
	ProviderPending   ProviderStatus = "pending"
	ProviderApproved  ProviderStatus = "approved"
	ProviderRejected  ProviderStatus = "rejected"
	ProviderSuspended ProviderStatus = "suspended"
)

type DocumentStatus string

const (
	DocumentUploaded DocumentStatus = "uploaded"
	DocumentApproved DocumentStatus = "approved"
	DocumentRejected DocumentStatus = "rejected"
)

const (
	minProviderAgeYears = 18
	minBirthYear        = 1900
	minVehicleYear      = 1950
	maxVehicleSeats     = 60
	maxProfileTextLen   = 120
	maxAddressTextLen   = 200
	maxReasonTextLen    = 500
)

var (
	ErrProfileLocked             = errors.New("the profile cannot change in this state")
	ErrInvalidProviderTransition = errors.New("this change of provider status is not possible")
	ErrReviewReasonRequired      = errors.New("a reason is required")
)

type Vehicle struct {
	Brand string
	Model string
	Year  int
	Color string
	Plate string
	Seats int
}

func (v Vehicle) Normalized(now time.Time) (Vehicle, error) {
	v.Brand, v.Model = strings.TrimSpace(v.Brand), strings.TrimSpace(v.Model)
	v.Color = strings.TrimSpace(v.Color)
	v.Plate = strings.ToUpper(strings.TrimSpace(v.Plate))
	for _, field := range []string{v.Brand, v.Model, v.Color, v.Plate} {
		if field == "" || len([]rune(field)) > maxProfileTextLen {
			return v, validationError("vehicle", "Indica a marca, o modelo, a cor e a matrícula.")
		}
	}
	if v.Year < minVehicleYear || v.Year > now.Year()+1 {
		return v, validationError("year", "O ano da viatura não é válido.")
	}
	if v.Seats < 1 || v.Seats > maxVehicleSeats {
		return v, validationError("seats", "O número de lugares não é válido.")
	}
	return v, nil
}

func (v Vehicle) Description() string {
	return strings.TrimSpace(v.Brand + " " + v.Model + " " + v.Color)
}

type Provider struct {
	UserID             uuid.UUID
	Status             ProviderStatus
	ServiceID          *uuid.UUID
	RequestedServiceID *uuid.UUID
	BirthDate          *time.Time
	IDNumber           string
	Address            string
	Vehicle            Vehicle
	StatusReason       string
	SubmittedAt        *time.Time
	ReviewedAt         *time.Time
	Base               *GeoPoint
	ServiceRadiusKm    *float64
	LastJobCompletedAt *time.Time
	CreatedAt          time.Time
}

func (p *Provider) IsEditable() bool {
	return p.Status == ProviderDraft || p.Status == ProviderRejected
}

func (p *Provider) RequestService(serviceID uuid.UUID) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	p.RequestedServiceID = &serviceID
	return nil
}

func (p *Provider) SetPersonalData(birthDate time.Time, idNumber, address string, now time.Time) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	idNumber, address = strings.TrimSpace(idNumber), strings.TrimSpace(address)
	if birthDate.Year() < minBirthYear || birthDate.AddDate(minProviderAgeYears, 0, 0).After(now) {
		return validationError("birth_date", "É preciso ter pelo menos 18 anos.")
	}
	if idNumber == "" || len([]rune(idNumber)) > maxProfileTextLen {
		return validationError("id_number", "Indica o número do documento de identificação.")
	}
	if address == "" || len([]rune(address)) > maxAddressTextLen {
		return validationError("address", "Indica a morada.")
	}
	p.BirthDate, p.IDNumber, p.Address = &birthDate, idNumber, address
	return nil
}

func (p *Provider) SetVehicle(vehicle Vehicle, now time.Time) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	normalized, err := vehicle.Normalized(now)
	if err != nil {
		return err
	}
	p.Vehicle = normalized
	return nil
}

func (p *Provider) Submit(now time.Time) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	p.Status = ProviderPending
	p.StatusReason = ""
	p.SubmittedAt = &now
	return nil
}

func (p *Provider) Approve(serviceID uuid.UUID, now time.Time) error {
	if p.Status != ProviderPending && p.Status != ProviderSuspended {
		return ErrInvalidProviderTransition
	}
	p.Status = ProviderApproved
	p.ServiceID = &serviceID
	p.StatusReason = ""
	p.ReviewedAt = &now
	return nil
}

func (p *Provider) Reject(reason string, now time.Time) error {
	if p.Status != ProviderPending {
		return ErrInvalidProviderTransition
	}
	return p.closeWith(ProviderRejected, reason, now)
}

func (p *Provider) Suspend(reason string, now time.Time) error {
	if p.Status != ProviderApproved {
		return ErrInvalidProviderTransition
	}
	return p.closeWith(ProviderSuspended, reason, now)
}

func (p *Provider) closeWith(status ProviderStatus, reason string, now time.Time) error {
	reason, err := normalizedReason(reason)
	if err != nil {
		return err
	}
	p.Status = status
	p.StatusReason = reason
	p.ReviewedAt = &now
	return nil
}

func normalizedReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", ErrReviewReasonRequired
	}
	if len([]rune(reason)) > maxReasonTextLen {
		return "", validationError("reason", "O motivo tem no máximo 500 caracteres.")
	}
	return reason, nil
}

type DocumentType struct {
	Code                string
	Name                string
	Scope               string
	AllowedContentTypes []string
	MaxSizeBytes        int64
}

func (t DocumentType) Allows(contentType string) bool {
	for _, allowed := range t.AllowedContentTypes {
		if allowed == contentType {
			return true
		}
	}
	return false
}

type ProviderDocument struct {
	ID              uuid.UUID
	ProviderID      uuid.UUID
	Type            string
	TypeName        string
	ObjectKey       string
	ContentType     string
	SizeBytes       int64
	Status          DocumentStatus
	RejectionReason string
	ReviewedBy      *uuid.UUID
	ReviewedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (d *ProviderDocument) Review(approved bool, reason string, reviewer *uuid.UUID, now time.Time) error {
	if approved {
		d.Status = DocumentApproved
		d.RejectionReason = ""
	} else {
		normalized, err := normalizedReason(reason)
		if err != nil {
			return err
		}
		d.Status = DocumentRejected
		d.RejectionReason = normalized
	}
	d.ReviewedBy = reviewer
	d.ReviewedAt = &now
	d.UpdatedAt = now
	return nil
}

type ProviderRequirements struct {
	ServiceID       uuid.UUID
	ServiceName     string
	RequiresVehicle bool
	MinVehicleSeats int
	Documents       []DocumentType
}

func (r *ProviderRequirements) MissingFor(profile *Provider, name string, documents []*ProviderDocument) map[string]string {
	missing := map[string]string{}
	if strings.TrimSpace(name) == "" {
		missing["personal.name"] = "Falta o nome."
	}
	if profile.BirthDate == nil {
		missing["personal.birth_date"] = "Falta a data de nascimento."
	}
	if profile.IDNumber == "" {
		missing["personal.id_number"] = "Falta o número do documento de identificação."
	}
	if profile.Address == "" {
		missing["personal.address"] = "Falta a morada."
	}
	if r.RequiresVehicle {
		r.addVehicleGaps(profile.Vehicle, missing)
	}
	byType := make(map[string]*ProviderDocument, len(documents))
	for _, document := range documents {
		byType[document.Type] = document
	}
	for _, required := range r.Documents {
		document, sent := byType[required.Code]
		switch {
		case !sent:
			missing["documents."+required.Code] = "Falta: " + required.Name + "."
		case document.Status == DocumentRejected:
			missing["documents."+required.Code] = "Rejeitado, envia de novo: " + document.RejectionReason
		}
	}
	return missing
}

func (r *ProviderRequirements) addVehicleGaps(vehicle Vehicle, missing map[string]string) {
	if vehicle.Plate == "" {
		missing["vehicle"] = "Faltam os dados da viatura."
		return
	}
	if vehicle.Seats < r.MinVehicleSeats {
		missing["vehicle.seats"] = "Este serviço exige uma viatura com mais lugares."
	}
}

func (r *ProviderRequirements) NotApproved(documents []*ProviderDocument) map[string]string {
	approved := make(map[string]bool, len(documents))
	for _, document := range documents {
		approved[document.Type] = document.Status == DocumentApproved
	}
	pending := map[string]string{}
	for _, required := range r.Documents {
		if !approved[required.Code] {
			pending["documents."+required.Code] = "Ainda não aprovado: " + required.Name + "."
		}
	}
	return pending
}
