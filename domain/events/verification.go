package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	ProviderDocumentUploaded       = "providers.document-uploaded"
	ProviderIdentityReadRequested  = "providers.identity-read-requested"
	ProviderVerificationReminder   = "providers.verification-reminder"
	providerVerificationAggregates = ProviderAggregate
)

type ProviderDocumentUploadedV1 struct {
	ProviderID uuid.UUID `json:"provider_id"`
	DocumentID uuid.UUID `json:"document_id"`
	FileID     uuid.UUID `json:"file_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ProviderDocumentUploadedV1) EventType() string     { return ProviderDocumentUploaded }
func (ProviderDocumentUploadedV1) EventVersion() int     { return 1 }
func (ProviderDocumentUploadedV1) AggregateType() string { return providerVerificationAggregates }
func (e ProviderDocumentUploadedV1) AggregateID() string { return e.ProviderID.String() }

type ProviderIdentityReadRequestedV1 struct {
	ProviderID uuid.UUID `json:"provider_id"`
	DocumentID uuid.UUID `json:"document_id"`
	Attempt    int       `json:"attempt"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (ProviderIdentityReadRequestedV1) EventType() string     { return ProviderIdentityReadRequested }
func (ProviderIdentityReadRequestedV1) EventVersion() int     { return 1 }
func (ProviderIdentityReadRequestedV1) AggregateType() string { return providerVerificationAggregates }
func (e ProviderIdentityReadRequestedV1) AggregateID() string { return e.ProviderID.String() }

type ProviderVerificationReminderV1 struct {
	ReminderID   uuid.UUID  `json:"reminder_id"`
	ProviderID   uuid.UUID  `json:"provider_id"`
	Kind         string     `json:"kind"`
	DocumentType string     `json:"document_type,omitempty"`
	ExpiresOn    *time.Time `json:"expires_on,omitempty"`
	OccurredAt   time.Time  `json:"occurred_at"`
}

func (ProviderVerificationReminderV1) EventType() string     { return ProviderVerificationReminder }
func (ProviderVerificationReminderV1) EventVersion() int     { return 1 }
func (ProviderVerificationReminderV1) AggregateType() string { return providerVerificationAggregates }
func (e ProviderVerificationReminderV1) AggregateID() string { return e.ProviderID.String() }
