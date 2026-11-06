package entities

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type ProviderStatus string

const (
	ProviderDraft     ProviderStatus = "draft"
	ProviderPending   ProviderStatus = "pending"
	ProviderApproved  ProviderStatus = "approved"
	ProviderRejected  ProviderStatus = "rejected"
	ProviderSuspended ProviderStatus = "suspended"
)

const (
	minProviderAgeYears = 18
	minBirthYear        = 1900
	minVehicleYear      = 1950
	maxVehicleSeats     = 60
	maxProfileTextLen   = 120
	maxReasonTextLen    = 2000
)

var idCardNumber = regexp.MustCompile(`^\d{9}[A-Z]{2}\d{3}$`)

var (
	ErrProfileLocked             = errors.New("the profile cannot change in this state")
	ErrInvalidProviderTransition = errors.New("this change of provider status is not possible")
	ErrReviewReasonRequired      = errors.New("a reason is required")
)

type Vehicle struct {
	Kind       string
	CapacityKg *int
	Brand      string
	Model      string
	Year       int
	Color      string
	Plate      string
	Seats      int
}

func (v Vehicle) Normalized(now time.Time) (Vehicle, error) {
	if v.Kind == "" {
		v.Kind = VehicleKindCar
	}
	if !IsVehicleKind(v.Kind) {
		return v, validationError("kind", "O tipo de viatura não é válido.")
	}
	if v.CapacityKg != nil && (*v.CapacityKg < 0 || *v.CapacityKg > MaxVehicleCapacityKg) {
		return v, validationError("capacity_kg", "A capacidade tem de estar entre 0 e 100 000 kg.")
	}
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
	OrganizationID     *uuid.UUID
	ServiceID          *uuid.UUID
	RequestedServiceID *uuid.UUID
	BirthDate          *time.Time
	IDNumber           string
	Province           string
	Municipality       string
	Vehicle            Vehicle
	StatusReason       string
	SubmittedAt        *time.Time
	ReviewedAt         *time.Time
	Base               *GeoPoint
	BaseAddressID      *uuid.UUID
	ServiceRadiusKm    *float64
	LastJobCompletedAt *time.Time
	CreatedAt          time.Time
	RatingSum          int64
	RatingCount        int
	CompletedRequests  int
}

func (p *Provider) IsEditable() bool {
	return p.Status == ProviderDraft || p.Status == ProviderRejected
}

func (p *Provider) IsFleetDriver() bool {
	return p.OrganizationID != nil
}

func (p *Provider) RequestService(serviceID uuid.UUID) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	p.RequestedServiceID = &serviceID
	return nil
}

type PersonalData struct {
	BirthDate    time.Time
	IDNumber     string
	Province     string
	Municipality string
}

func (d PersonalData) normalized(now time.Time) (PersonalData, error) {
	d.IDNumber = strings.ToUpper(strings.Join(strings.Fields(d.IDNumber), ""))
	d.Province, d.Municipality = strings.TrimSpace(d.Province), strings.TrimSpace(d.Municipality)
	if d.BirthDate.Year() < minBirthYear || d.BirthDate.AddDate(minProviderAgeYears, 0, 0).After(now) {
		return d, validationError("birth_date", "É preciso ter pelo menos 18 anos.")
	}
	if !idCardNumber.MatchString(d.IDNumber) {
		return d, validationError("id_number", "O número do BI tem 9 algarismos, 2 letras e 3 algarismos, como 005487123LA041.")
	}
	if d.Province == "" || len([]rune(d.Province)) > maxProfileTextLen {
		return d, validationError("province", "Indica a província.")
	}
	if d.Municipality == "" || len([]rune(d.Municipality)) > maxProfileTextLen {
		return d, validationError("municipality", "Indica o município.")
	}
	return d, nil
}

func (p *Provider) SetProfile(serviceID uuid.UUID, data PersonalData, now time.Time) error {
	if !p.IsEditable() {
		return ErrProfileLocked
	}
	normalized, err := data.normalized(now)
	if err != nil {
		return err
	}
	p.RequestedServiceID = &serviceID
	p.BirthDate, p.IDNumber = &normalized.BirthDate, normalized.IDNumber
	p.Province, p.Municipality = normalized.Province, normalized.Municipality
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
		return "", validationError("reason", "O motivo tem no máximo 2000 caracteres.")
	}
	return reason, nil
}

type ProviderRequirements struct {
	ServiceID          uuid.UUID
	ServiceName        string
	RequiresVehicle    bool
	MinVehicleSeats    int
	VehicleKinds       []string
	SelfSignup         bool
	VerificationKind   enums.VerificationKind
	DrivingCategories  []string
	MaxVehicleAgeYears *int
	Documents          []DocumentSpec
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
	if profile.Province == "" {
		missing["personal.province"] = "Falta a província."
	}
	if profile.Municipality == "" {
		missing["personal.municipality"] = "Falta o município."
	}
	if r.RequiresVehicle {
		r.addVehicleGaps(profile.Vehicle, missing)
	}
	byType := providerDocumentsByType(documents)
	for i := range r.Documents {
		required := &r.Documents[i]
		document, sent := byType[required.Type]
		key := "documents." + required.Type.String()
		switch {
		case !sent || (required.HasFiles() && !document.AnyFile()):
			missing[key] = "Falta: " + required.Name + "."
		case document.Status == DocumentRejected:
			missing[key] = "Rejeitado, envia de novo: " + document.RejectionReason
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
	if !r.AcceptsKind(vehicle.Kind) {
		missing["vehicle.kind"] = "Este serviço não aceita este tipo de viatura."
	}
}

func (r *ProviderRequirements) AcceptsKind(kind string) bool {
	if len(r.VehicleKinds) == 0 {
		return true
	}
	for _, accepted := range r.VehicleKinds {
		if accepted == kind {
			return true
		}
	}
	return false
}

func (r *ProviderRequirements) VehicleGaps(vehicle Vehicle) map[string]string {
	gaps := map[string]string{}
	if r.RequiresVehicle {
		r.addVehicleGaps(vehicle, gaps)
	}
	return gaps
}

func (r *ProviderRequirements) NotApproved(documents []*ProviderDocument) map[string]string {
	byType := providerDocumentsByType(documents)
	pending := map[string]string{}
	for i := range r.Documents {
		required := &r.Documents[i]
		if document, sent := byType[required.Type]; !sent || document.Status != DocumentApproved {
			pending["documents."+required.Type.String()] = "Ainda não aprovado: " + required.Name + "."
		}
	}
	return pending
}

func providerDocumentsByType(documents []*ProviderDocument) map[enums.DocumentType]*ProviderDocument {
	byType := make(map[enums.DocumentType]*ProviderDocument, len(documents))
	for _, document := range documents {
		if document.Subject == enums.DocumentSubjectProvider {
			byType[document.Type] = document
		}
	}
	return byType
}
