package verification

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var (
	ErrLocked               = errors.New("the verification cannot change while it is under review")
	ErrServiceNotChosen     = errors.New("the provider has not chosen a service yet")
	ErrServiceNotOpen       = errors.New("this service does not accept providers")
	ErrProviderNotFound     = errors.New("provider not found")
	ErrUploadNotFound       = errors.New("upload not found")
	ErrFileNotUploaded      = errors.New("the file was not uploaded")
	ErrUnknownDocument      = errors.New("unknown document type")
	ErrDocumentNotInPlan    = errors.New("this document is not part of this verification")
	ErrStepNotInPlan        = errors.New("this step is not part of this verification")
	ErrTooManyExtras        = errors.New("too many extra photos")
	ErrUploadNotReady       = errors.New("the upload is not processed yet")
	ErrLivenessSession      = errors.New("the liveness session is not valid")
	ErrDeclarationRequired  = errors.New("the declaration must be accepted")
	ErrIdentityNotConfirmed = errors.New("the identity card data is not confirmed yet")
	ErrBankNotFound         = errors.New("bank not found")
	ErrInsurerNotFound      = errors.New("insurer not found")
	ErrManagedByFleet       = errors.New("the fleet manages this for its drivers")
	ErrServiceNotFound      = errors.New("workshop service not found")
	ErrWorkshopIncomplete   = errors.New("the workshop data is incomplete")
)

const (
	CodeQualityRejected      = "DOCUMENT_QUALITY_REJECTED"
	CodeIdentityExpired      = "IDENTITY_CARD_EXPIRED"
	CodeExtractionMax        = "EXTRACTION_MAX_ATTEMPTS"
	CodeLivenessFailed       = "LIVENESS_FAILED"
	CodeInvalidFileType      = "INVALID_FILE_TYPE"
	CodeFileTooLarge         = "FILE_TOO_LARGE"
	CodeInvalidPlate         = "INVALID_PLATE"
	CodeVehicleTooOld        = "VEHICLE_TOO_OLD"
	CodeCategoryRequired     = "DRIVING_CATEGORY_REQUIRED"
	CodeDocumentExpired      = "DOCUMENT_EXPIRED"
	CodeCertificateTooOld    = "CERTIFICATE_TOO_OLD"
	CodeInvalidIBAN          = "INVALID_IBAN"
	CodeIBANBankMismatch     = "IBAN_BANK_MISMATCH"
	CodeHolderNameMismatch   = "HOLDER_NAME_MISMATCH"
	CodeInvalidNIF           = "INVALID_NIF"
	CodeIncomplete           = "VERIFICATION_INCOMPLETE"
	CodeImagesRequired       = "DOCUMENT_FILES_MISSING"
	CodeManualEntryForbidden = "MANUAL_ENTRY_NOT_ALLOWED"
	CodeExtractionNotDone    = "EXTRACTION_NOT_COMPLETED"
	CodeIdentityMismatch     = "IDENTITY_DATA_INVALID"
	CodeUnderAge             = "PROVIDER_UNDER_AGE"
)

const (
	auditEntityDocument     = "provider_document"
	auditEntityVerification = "provider_verification"
	batchSize               = 100
)

type Settings struct {
	MaxFileBytes          int64
	ReadURLTTL            time.Duration
	MinProviderAge        int
	MaxAttempts           int
	CriminalRecordMaxDays int
	CertificateMaxMonths  int
	ExpiryNoticeDays      int
	PendingUploadTTL      time.Duration
	ReminderDelay         time.Duration
	HousekeepingInterval  time.Duration
	Currency              string
}

type Deps struct {
	Users         repository.IUserRepository
	Providers     repository.IProviderRepository
	Documents     repository.IProviderDocumentRepository
	Verifications repository.IVerificationRepository
	Outbox        repository.IOutboxRepository
	Audit         repository.IAuditLogRepository
	Tx            repository.ITransactionManager
	Storage       services.IObjectStorage
	Quality       services.IImageQualityChecker
	OCR           services.IDocumentOCRProvider
	Liveness      services.ILivenessProvider
	Settings      Settings
}

type Usecase struct {
	Deps
	now func() time.Time
}

func NewUsecase(deps Deps) *Usecase {
	return &Usecase{Deps: deps, now: func() time.Time { return time.Now().UTC() }}
}

func (u *Usecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Outbox.Save(ctx, envelope)
}

func (u *Usecase) audit(ctx context.Context, actor entities.Actor, action, entityType, id string, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entityType, id, nil, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Audit.Record(ctx, entry)
}

func providerActor(providerID uuid.UUID) entities.Actor {
	id := providerID
	return entities.Actor{ID: &id, Role: enums.RoleProvider}
}

type session struct {
	provider     *entities.Provider
	verification *entities.ProviderVerification
	requirements *entities.ProviderRequirements
	documents    []*entities.ProviderDocument
}

func (s *session) snapshot() *entities.VerificationSnapshot {
	return &entities.VerificationSnapshot{Kind: s.verification.Kind, Provider: s.provider, Documents: s.documents, Workshop: s.verification.Workshop}
}

func (s *session) document(documentType enums.DocumentType, subject enums.DocumentSubject) *entities.ProviderDocument {
	for _, document := range s.documents {
		if document.Type == documentType && document.Subject == subject {
			return document
		}
	}
	return nil
}

func (u *Usecase) open(ctx context.Context, providerID uuid.UUID) (*session, error) {
	provider, err := u.Providers.Find(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		if err := u.Providers.Create(ctx, providerID); err != nil {
			return nil, err
		}
		if provider, err = u.Providers.Find(ctx, providerID); err != nil {
			return nil, err
		}
	}
	if provider.RequestedServiceID == nil {
		return nil, ErrServiceNotChosen
	}
	requirements, err := u.Documents.Requirements(ctx, *provider.RequestedServiceID)
	if err != nil {
		return nil, err
	}
	if requirements == nil {
		return nil, ErrServiceNotOpen
	}
	verification, err := u.Verifications.Find(ctx, providerID)
	if err != nil {
		return nil, err
	}
	now := u.now()
	switch {
	case verification == nil:
		if verification, err = entities.NewProviderVerification(providerID, requirements.VerificationKind, now); err != nil {
			return nil, err
		}
		if err := u.Verifications.Save(ctx, verification); err != nil {
			return nil, err
		}
	case verification.Kind != requirements.VerificationKind && provider.IsEditable():
		verification.Kind, verification.UpdatedAt = requirements.VerificationKind, now
		if err := u.Verifications.Save(ctx, verification); err != nil {
			return nil, err
		}
	}
	documents, err := u.Documents.Documents(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return &session{provider: provider, verification: verification, requirements: requirements, documents: documents}, nil
}

func (u *Usecase) editable(ctx context.Context, providerID uuid.UUID) (*session, error) {
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !s.provider.IsEditable() {
		return nil, ErrLocked
	}
	return s, nil
}

func (u *Usecase) step(ctx context.Context, providerID uuid.UUID, item enums.VerificationItem) (*session, error) {
	s, err := u.editable(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !entities.PlanHas(s.verification.Kind, item) {
		return nil, ErrStepNotInPlan
	}
	return s, nil
}

func (u *Usecase) documentFor(s *session, documentType enums.DocumentType, subject enums.DocumentSubject) (*entities.ProviderDocument, error) {
	if document := s.document(documentType, subject); document != nil {
		return document, nil
	}
	document, err := entities.NewProviderDocument(s.provider.UserID, documentType, subject, u.now())
	if err != nil {
		return nil, err
	}
	s.documents = append(s.documents, document)
	return document, nil
}

func (u *Usecase) saveDocument(ctx context.Context, document *entities.ProviderDocument, action string, after any) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(document.ProviderID), action, auditEntityDocument, document.ID.String(), after)
	})
}

func (u *Usecase) stepView(s *session, item enums.VerificationItem) *StepResult {
	return &StepResult{Item: item.String(), Status: string(s.snapshot().ItemStatus(item))}
}

type StepResult struct {
	Item   string `json:"item" example:"driving_license"`
	Status string `json:"status" example:"completed"`
}

func parseDate(field, value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, customerrors.FieldError(field, "A data tem de estar no formato AAAA-MM-DD.")
	}
	return date, nil
}

func futureDate(field, value string, now time.Time) (time.Time, error) {
	date, err := parseDate(field, value)
	if err != nil {
		return date, err
	}
	if !entities.IsFutureDate(date, now) {
		return date, entities.NewRuleError(CodeDocumentExpired, field, "A data de expiração tem de ser futura.").With("expires_at", date.Format(time.DateOnly))
	}
	return date, nil
}

func cleanText(field, value string, maxLen int) (string, error) {
	clean := strings.Join(strings.Fields(value), " ")
	if clean == "" || len([]rune(clean)) > maxLen {
		return "", customerrors.FieldError(field, "Este campo é obrigatório.")
	}
	return clean, nil
}

func subjectOr(raw string, fallback enums.DocumentSubject) enums.DocumentSubject {
	if raw == "" {
		return fallback
	}
	return enums.DocumentSubject(raw)
}

func (s *session) identityName() (string, error) {
	card := s.document(enums.DocumentTypeIDCard, entities.PersonSubject(s.verification.Kind))
	if card == nil || card.Details.FullName == "" || card.Details.Source == "" {
		return "", ErrIdentityNotConfirmed
	}
	return card.Details.FullName, nil
}
