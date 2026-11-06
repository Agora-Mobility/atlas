package entities

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Bank struct {
	ID        uuid.UUID
	Code      string
	ShortName string
	Name      string
}

type Insurer struct {
	ID   uuid.UUID
	Name string
}

type WorkshopCategory struct {
	ID          uuid.UUID
	Code        string
	Label       string
	Description string
}

const (
	ReminderCriminalRecord = "criminal_record"
	ReminderDocumentExpiry = "document_expiry"
)

type VerificationReminder struct {
	ID           uuid.UUID
	ProviderID   uuid.UUID
	Kind         string
	DocumentID   *uuid.UUID
	DocumentType enums.DocumentType
	ExpiresOn    *time.Time
	DueAt        time.Time
	CreatedAt    time.Time
}

var ExpiringDocumentTypes = []enums.DocumentType{
	enums.DocumentTypeVehicleInsurance, enums.DocumentTypeVehicleInspection, enums.DocumentTypeDrivingLicense, enums.DocumentTypeBusinessLicense,
}

type ExpiringDocument struct {
	DocumentID   uuid.UUID
	ProviderID   uuid.UUID
	DocumentType enums.DocumentType
	ExpiresOn    time.Time
}

type NewWorkshop struct {
	ProviderID  uuid.UUID
	CompanyName string
	NIF         string
	Phone       string
	Currency    string
	Draft       WorkshopDraft
	Categories  map[uuid.UUID]WorkshopCategory
}
