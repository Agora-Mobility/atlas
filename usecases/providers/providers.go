package providers

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/filetype"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/providers/dtos"
)

var (
	ErrRegistrationNotAllowed = errors.New("this client does not create provider accounts")
	ErrPhoneTaken             = repository.ErrPhoneAlreadyExists
	ErrEmailTaken             = repository.ErrEmailAlreadyExists
	ErrServiceNotOpen         = errors.New("this service does not accept providers")
	ErrDocumentTypeUnknown    = errors.New("unknown document type")
	ErrContentTypeRejected    = errors.New("this file type is not accepted for this document")
	ErrFileTooLarge           = errors.New("the file is too large")
	ErrObjectKeyInvalid       = errors.New("the object key does not belong to this provider and document")
	ErrObjectNotFound         = errors.New("the file was not uploaded")
	ErrDocumentNotFound       = errors.New("document not found")
	ErrProviderNotFound       = errors.New("provider not found")
	ErrProviderBusy           = errors.New("the provider has an active job")
	ErrReviewNotAllowed       = errors.New("documents are reviewed while the application is pending")
	ErrManagedByFleet         = errors.New("the fleet manages this for its drivers")
	ErrRejectionReasonUnknown = errors.New("rejection reason not found")
)

type IncompleteError struct {
	Code   string
	Fields map[string]string
}

func (e *IncompleteError) Error() string {
	return e.Code
}

const (
	auditEntityProvider = "provider"
	auditEntityDocument = "provider_document"
	auditEntityReason   = "rejection_reason"
)

type CodeSender interface {
	SendSilently(ctx context.Context, purpose enums.OTPPurpose, phone string) error
}

type Deps struct {
	Users     repository.IUserRepository
	Providers repository.IProviderRepository
	Documents repository.IProviderDocumentRepository
	Requests  repository.IRequestRepository
	Locator   repository.IProviderLocator
	Outbox    repository.IOutboxRepository
	Audit     repository.IAuditLogRepository
	Tx        repository.ITransactionManager
	Codes     CodeSender
	Storage   services.IObjectStorage
	Approvals ApprovalHook
	Settings  Settings
}

type Settings struct {
	MaxFileBytes int64
}

type ApprovalHook interface {
	OnApproved(ctx context.Context, providerID uuid.UUID) error
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

func (u *Usecase) Register(ctx context.Context, client *entities.Client, in dtos.RegisterProviderInput) (*dtos.RegisteredProvider, error) {
	if client.Type != enums.ClientTypeProviderApp {
		return nil, ErrRegistrationNotAllowed
	}
	phoneNumber := phone.Normalize(in.Phone)
	user, err := u.Users.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if user != nil && (user.IsVerified || user.Role != enums.RoleProvider) {
		return nil, ErrPhoneTaken
	}
	if user == nil {
		if user, err = u.createProviderAccount(ctx, phoneNumber); err != nil {
			return nil, err
		}
	} else if err := u.Providers.Create(ctx, user.ID); err != nil {
		return nil, err
	}
	if err := u.Codes.SendSilently(ctx, enums.OTPPurposeVerifyPhone, user.Phone); err != nil {
		logger.FromContext(ctx).Warn("verification code not sent after provider sign-up", zap.Error(err))
	}
	return &dtos.RegisteredProvider{ID: user.ID, Phone: user.Phone, Status: string(entities.ProviderDraft), IsVerified: user.IsVerified}, nil
}

func (u *Usecase) createProviderAccount(ctx context.Context, phoneNumber string) (*entities.User, error) {
	user, err := entities.NewSignUpUser(phoneNumber, enums.RoleProvider)
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Users.Create(ctx, user); err != nil {
			return err
		}
		if err := u.publish(ctx, events.UserRegisteredV1{ID: user.ID, Role: user.Role, CreatedAt: user.CreatedAt}); err != nil {
			return err
		}
		return u.Providers.Create(ctx, user.ID)
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (u *Usecase) OpenServices(ctx context.Context) (*dtos.OpenServices, error) {
	open, err := u.Documents.OpenServices(ctx)
	if err != nil {
		return nil, err
	}
	result := &dtos.OpenServices{Categories: []dtos.OpenCategory{}}
	for i := range open {
		service := &open[i]
		if len(result.Categories) == 0 || result.Categories[len(result.Categories)-1].ID != service.CategoryID {
			result.Categories = append(result.Categories, dtos.OpenCategory{ID: service.CategoryID, Slug: service.CategorySlug, Name: service.CategoryName})
		}
		last := &result.Categories[len(result.Categories)-1]
		last.Services = append(last.Services, dtos.OpenService{
			ID: service.ID, Slug: service.Slug, Name: service.Name, Description: service.Description,
			RequiresVehicle: service.RequiresVehicle, MinVehicleSeats: service.MinVehicleSeats,
		})
	}
	return result, nil
}

func (u *Usecase) providerOf(ctx context.Context, providerID uuid.UUID) (*entities.Provider, error) {
	provider, err := u.Providers.Find(ctx, providerID)
	if err != nil || provider != nil {
		return provider, err
	}
	if err := u.Providers.Create(ctx, providerID); err != nil {
		return nil, err
	}
	return u.Providers.Find(ctx, providerID)
}

func (u *Usecase) Me(ctx context.Context, providerID uuid.UUID) (*dtos.ProviderView, error) {
	provider, err := u.providerOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) editProvider(ctx context.Context, providerID uuid.UUID, change func(*entities.Provider) error) (*dtos.ProviderView, error) {
	provider, err := u.providerOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if err := change(provider); err != nil {
		return nil, err
	}
	if err := u.Providers.Save(ctx, provider); err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) SetProfile(ctx context.Context, providerID uuid.UUID, in dtos.ProfileInput) (*dtos.ProviderView, error) {
	birthDate, err := time.Parse(time.DateOnly, in.BirthDate)
	if err != nil {
		return nil, customerrors.FieldError("birth_date", "A data tem de estar no formato AAAA-MM-DD.")
	}
	provider, err := u.providerOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if err := u.ensureServiceCanBeChosen(ctx, provider, in.ServiceID); err != nil {
		return nil, err
	}
	user, err := u.Users.FindByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrProviderNotFound
	}
	personal := entities.PersonalData{BirthDate: birthDate, IDNumber: in.IDNumber, Province: in.Province, Municipality: in.Municipality}
	if err := provider.SetProfile(in.ServiceID, personal, u.now()); err != nil {
		return nil, err
	}
	if err := user.UpdateProfile(in.Name, in.Email, user.Phone); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Users.Update(ctx, user); err != nil {
			return err
		}
		return u.Providers.Save(ctx, provider)
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) ensureServiceCanBeChosen(ctx context.Context, provider *entities.Provider, serviceID uuid.UUID) error {
	if provider.IsFleetDriver() {
		if provider.RequestedServiceID == nil || *provider.RequestedServiceID != serviceID {
			return ErrManagedByFleet
		}
		return nil
	}
	requirements, err := u.Documents.Requirements(ctx, serviceID)
	if err != nil {
		return err
	}
	if requirements == nil || !requirements.SelfSignup {
		return ErrServiceNotOpen
	}
	return nil
}

func (u *Usecase) SetVehicle(ctx context.Context, providerID uuid.UUID, in dtos.VehicleInput) (*dtos.ProviderView, error) {
	return u.editProvider(ctx, providerID, func(provider *entities.Provider) error {
		if provider.IsFleetDriver() {
			return ErrManagedByFleet
		}
		return provider.SetVehicle(entities.Vehicle{Kind: in.Kind, CapacityKg: in.CapacityKg, Brand: in.Brand, Model: in.Model, Year: in.Year, Color: in.Color, Plate: in.Plate, Seats: in.Seats}, u.now())
	})
}

func (u *Usecase) editableProvider(ctx context.Context, providerID uuid.UUID) (*entities.Provider, error) {
	provider, err := u.providerOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !provider.IsEditable() {
		return nil, entities.ErrProfileLocked
	}
	return provider, nil
}

func objectPrefix(providerID uuid.UUID, documentType enums.DocumentType) string {
	return "providers/" + providerID.String() + "/" + documentType.String() + "/"
}

func documentSlot(spec entities.DocumentSpec, requested string) (enums.DocumentSlot, error) {
	if requested == "" {
		if len(spec.RequiredSlots) == 0 {
			return "", ErrDocumentTypeUnknown
		}
		return spec.RequiredSlots[0], nil
	}
	slot := enums.DocumentSlot(requested)
	if !spec.AcceptsSlot(slot) {
		return "", customerrors.FieldError("slot", "Este documento não tem esta parte.")
	}
	return slot, nil
}

func specOf(code string) (entities.DocumentSpec, error) {
	spec, known := entities.SpecOf(enums.DocumentType(code))
	if !known || !spec.HasFiles() {
		return spec, ErrDocumentTypeUnknown
	}
	return spec, nil
}

func (u *Usecase) UploadURL(ctx context.Context, providerID uuid.UUID, in dtos.UploadURLInput) (*dtos.UploadURLOutput, error) {
	if _, err := u.editableProvider(ctx, providerID); err != nil {
		return nil, err
	}
	spec, err := specOf(in.DocumentType)
	if err != nil {
		return nil, err
	}
	slot, err := documentSlot(spec, in.Slot)
	if err != nil {
		return nil, err
	}
	if !spec.Allows(slot, in.ContentType) {
		return nil, ErrContentTypeRejected
	}
	if in.Size > u.Settings.MaxFileBytes {
		return nil, ErrFileTooLarge
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	extension, _ := filetype.Extension(in.ContentType)
	key := objectPrefix(providerID, spec.Type) + id.String() + extension
	signed, err := u.Storage.PresignUpload(ctx, key, in.ContentType, in.Size)
	if err != nil {
		return nil, err
	}
	return &dtos.UploadURLOutput{UploadURL: signed.URL, ObjectKey: key, ExpiresAt: signed.ExpiresAt, Headers: signed.Headers}, nil
}

func validObjectKey(key string, providerID uuid.UUID, documentType enums.DocumentType) bool {
	file, found := strings.CutPrefix(key, objectPrefix(providerID, documentType))
	if !found || strings.Contains(file, "/") {
		return false
	}
	extension := path.Ext(file)
	if _, err := uuid.Parse(strings.TrimSuffix(file, extension)); err != nil {
		return false
	}
	return filetype.IsKnownExtension(extension)
}

func (u *Usecase) RegisterDocument(ctx context.Context, providerID uuid.UUID, in dtos.RegisterDocumentInput) (*dtos.ProviderView, error) {
	provider, err := u.editableProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	spec, err := specOf(in.DocumentType)
	if err != nil {
		return nil, err
	}
	slot, err := documentSlot(spec, in.Slot)
	if err != nil {
		return nil, err
	}
	if !validObjectKey(in.ObjectKey, providerID, spec.Type) {
		return nil, ErrObjectKeyInvalid
	}
	object, err := u.Storage.Stat(ctx, in.ObjectKey)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, ErrObjectNotFound
	}
	if object.SizeBytes > u.Settings.MaxFileBytes {
		u.discard(ctx, in.ObjectKey)
		return nil, ErrFileTooLarge
	}
	head, err := u.Storage.FirstBytes(ctx, in.ObjectKey, filetype.SniffBytes)
	if err != nil {
		return nil, err
	}
	detected := filetype.Detect(head)
	extension, _ := filetype.Extension(detected)
	if !spec.Allows(slot, detected) || extension != path.Ext(in.ObjectKey) {
		u.discard(ctx, in.ObjectKey)
		return nil, ErrContentTypeRejected
	}
	now := u.now()
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		document, err := u.Documents.Document(ctx, providerID, spec.Type, enums.DocumentSubjectProvider)
		if err != nil {
			return err
		}
		if document == nil {
			if document, err = entities.NewProviderDocument(providerID, spec.Type, enums.DocumentSubjectProvider, now); err != nil {
				return err
			}
		}
		document.Edited(now)
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		version, err := u.Documents.NextFileVersion(ctx, document.ID, slot)
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		file := &entities.DocumentFile{
			ID: id, DocumentID: document.ID, ProviderID: providerID, Slot: slot, Version: version, Status: entities.FileAccepted, ObjectKey: in.ObjectKey,
			ContentType: detected, SizeBytes: object.SizeBytes, CreatedAt: now, UpdatedAt: now,
		}
		if err := u.Documents.SupersedeFiles(ctx, document.ID, slot, file.ID, now); err != nil {
			return err
		}
		return u.Documents.SaveFile(ctx, file)
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) discard(ctx context.Context, key string) {
	if err := u.Storage.Delete(ctx, key); err != nil {
		logger.FromContext(ctx).Warn("stored object not removed", zap.Error(err))
	}
}

func (u *Usecase) MyDocumentURL(ctx context.Context, providerID uuid.UUID, documentType string) (*dtos.DocumentURLOutput, error) {
	spec, err := specOf(documentType)
	if err != nil {
		return nil, ErrDocumentNotFound
	}
	document, err := u.Documents.Document(ctx, providerID, spec.Type, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, ErrDocumentNotFound
	}
	for _, slot := range append(slices.Clone(spec.RequiredSlots), spec.OptionalSlots...) {
		if file := document.Current(slot); file != nil {
			signed, err := u.Storage.PresignDownload(ctx, file.ObjectKey)
			if err != nil {
				return nil, err
			}
			return &dtos.DocumentURLOutput{URL: signed.URL, ExpiresAt: signed.ExpiresAt}, nil
		}
	}
	return nil, ErrDocumentNotFound
}

func (u *Usecase) Submit(ctx context.Context, providerID uuid.UUID) (*dtos.ProviderView, error) {
	provider, err := u.editableProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	missing, err := u.missingFor(ctx, provider)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, &IncompleteError{Code: "PROFILE_INCOMPLETE", Fields: missing}
	}
	now := u.now()
	if err := provider.Submit(now); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.Save(ctx, provider); err != nil {
			return err
		}
		return u.publish(ctx, events.ProviderReviewV1{Type: events.ProviderApplicationSubmitted, ProviderID: providerID, ServiceID: provider.RequestedServiceID, OccurredAt: now})
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) missingFor(ctx context.Context, provider *entities.Provider) (map[string]string, error) {
	user, err := u.Users.FindByID(ctx, provider.UserID)
	if err != nil {
		return nil, err
	}
	if provider.RequestedServiceID == nil {
		return map[string]string{"service": "Escolhe o serviço que queres prestar."}, nil
	}
	requirements, err := u.Documents.Requirements(ctx, *provider.RequestedServiceID)
	if err != nil {
		return nil, err
	}
	if requirements == nil {
		return map[string]string{"service": "Este serviço já não aceita prestadores. Escolhe outro."}, nil
	}
	documents, err := u.Documents.Documents(ctx, provider.UserID)
	if err != nil {
		return nil, err
	}
	name := ""
	if user != nil {
		name = user.Name
	}
	return requirements.MissingFor(provider, name, documents), nil
}

func (u *Usecase) view(ctx context.Context, provider *entities.Provider, withLinks bool) (*dtos.ProviderView, error) {
	view := &dtos.ProviderView{
		UserID: provider.UserID, Status: string(provider.Status), StatusReason: provider.StatusReason, OrganizationID: provider.OrganizationID, RequestedServiceID: provider.RequestedServiceID, ServiceID: provider.ServiceID,
		IDNumber: provider.IDNumber, Province: provider.Province, Municipality: provider.Municipality, SubmittedAt: provider.SubmittedAt, ReviewedAt: provider.ReviewedAt,
		RequiredDocuments: []dtos.RequiredDocumentView{}, Documents: []dtos.DocumentView{}, Missing: map[string]string{},
	}
	if provider.BirthDate != nil {
		view.BirthDate = provider.BirthDate.Format(time.DateOnly)
	}
	if provider.Vehicle.Plate != "" {
		v := provider.Vehicle
		view.Vehicle = &dtos.VehicleView{Kind: v.Kind, CapacityKg: v.CapacityKg, Brand: v.Brand, Model: v.Model, Year: v.Year, Color: v.Color, Plate: v.Plate, Seats: v.Seats}
	}
	user, err := u.Users.FindByID(ctx, provider.UserID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		view.Name, view.Phone, view.Email = user.Name, user.Phone, user.Email
	}
	if provider.RequestedServiceID != nil {
		requirements, err := u.Documents.Requirements(ctx, *provider.RequestedServiceID)
		if err != nil {
			return nil, err
		}
		if requirements != nil {
			for i := range requirements.Documents {
				view.RequiredDocuments = append(view.RequiredDocuments, requiredDocumentView(&requirements.Documents[i], u.Settings.MaxFileBytes))
			}
		}
	}
	if provider.IsEditable() {
		if view.Missing, err = u.missingFor(ctx, provider); err != nil {
			return nil, err
		}
	}
	if provider.Status == entities.ProviderRejected || provider.Status == entities.ProviderSuspended {
		reasons, err := u.Documents.ProviderReasons(ctx, provider.UserID)
		if err != nil {
			return nil, err
		}
		view.StatusReasons = toReasonViews(reasons)
	}
	documentReasons, err := u.Documents.DocumentReasons(ctx, provider.UserID)
	if err != nil {
		return nil, err
	}
	documents, err := u.Documents.Documents(ctx, provider.UserID)
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		item, err := u.documentView(ctx, document, documentReasons[document.ID], withLinks)
		if err != nil {
			return nil, err
		}
		view.Documents = append(view.Documents, item)
	}
	return view, nil
}

func requiredDocumentView(spec *entities.DocumentSpec, maxBytes int64) dtos.RequiredDocumentView {
	view := dtos.RequiredDocumentView{
		Type: spec.Type.String(), Name: spec.Name, Scope: spec.Scope, MaxSizeBytes: maxBytes, Slots: []dtos.SlotView{}, AllowedContentTypes: []string{},
	}
	for _, slot := range spec.RequiredSlots {
		view.Slots = append(view.Slots, dtos.SlotView{Slot: slot.String(), Required: true, AllowedContentTypes: spec.ContentTypes(slot)})
	}
	for _, slot := range spec.OptionalSlots {
		view.Slots = append(view.Slots, dtos.SlotView{Slot: slot.String(), Required: false, AllowedContentTypes: spec.ContentTypes(slot)})
	}
	if len(spec.RequiredSlots) > 0 {
		view.AllowedContentTypes = spec.ContentTypes(spec.RequiredSlots[0])
	}
	return view
}

func (u *Usecase) documentView(ctx context.Context, document *entities.ProviderDocument, reasons []entities.RejectionReason, withLinks bool) (dtos.DocumentView, error) {
	item := dtos.DocumentView{
		ID: document.ID, Type: document.Type.String(), Subject: document.Subject.String(), Name: document.Name(), Status: string(document.Status), RejectionReason: document.RejectionReason,
		ReviewFlags: document.ReviewFlags, ReviewedAt: document.ReviewedAt, UpdatedAt: document.UpdatedAt, Files: []dtos.DocumentFileView{},
	}
	if document.Status == entities.DocumentRejected {
		item.RejectionReasons = toReasonViews(reasons)
	}
	for i := range document.Files {
		file := &document.Files[i]
		if file.Status == entities.FilePendingUpload {
			continue
		}
		fileView := dtos.DocumentFileView{
			ID: file.ID, Slot: file.Slot.String(), Version: file.Version, Status: string(file.Status), ContentType: file.ContentType, SizeBytes: file.SizeBytes, RejectionCodes: file.RejectionCodes,
		}
		if withLinks {
			signed, err := u.Storage.PresignDownload(ctx, file.ObjectKey)
			if err != nil {
				return item, err
			}
			fileView.URL, fileView.URLExpiresAt = signed.URL, &signed.ExpiresAt
		}
		if item.ContentType == "" && file.Status == entities.FileAccepted {
			item.ContentType, item.SizeBytes, item.URL, item.URLExpiresAt = file.ContentType, file.SizeBytes, fileView.URL, fileView.URLExpiresAt
		}
		item.Files = append(item.Files, fileView)
	}
	return item, nil
}

func (u *Usecase) ListApplications(ctx context.Context, filter dtos.ApplicationFilter, page pagination.Params) (*dtos.ApplicationList, error) {
	page = page.Normalize()
	applications, total, err := u.Providers.Applications(ctx, repository.ProviderApplicationFilter{
		Status: filter.Status, OrganizationID: filter.OrganizationID, ServiceID: filter.ServiceID, Category: filter.Category, Search: filter.Search,
		Limit: int(page.Limit()), Offset: int(page.Offset()),
	})
	if err != nil {
		return nil, err
	}
	list := &dtos.ApplicationList{Items: make([]dtos.ApplicationView, 0, len(applications)), Total: total, Page: page.Page, Size: page.Size}
	for i := range applications {
		application := &applications[i]
		list.Items = append(list.Items, dtos.ApplicationView{
			UserID: application.UserID, Name: application.Name, Phone: application.Phone, Status: string(application.Status), RequestedServiceID: application.RequestedServiceID,
			RequestedServiceName: application.RequestedServiceName, ServiceID: application.ServiceID, VehiclePlate: application.VehiclePlate,
			OrganizationID: application.OrganizationID, OrganizationName: application.OrganizationName, SubmittedAt: application.SubmittedAt,
		})
	}
	return list, nil
}

func (u *Usecase) existingProvider(ctx context.Context, providerID uuid.UUID) (*entities.Provider, error) {
	provider, err := u.Providers.Find(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, ErrProviderNotFound
	}
	return provider, nil
}

func (u *Usecase) Profile(ctx context.Context, providerID uuid.UUID) (*dtos.ProviderView, error) {
	provider, err := u.existingProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, false)
}

func (u *Usecase) GetApplication(ctx context.Context, actor entities.Actor, providerID uuid.UUID) (*dtos.ProviderView, error) {
	provider, err := u.existingProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	view, err := u.view(ctx, provider, true)
	if err != nil {
		return nil, err
	}
	if err := u.audit(ctx, actor, "provider_document.view", auditEntityProvider, providerID.String(), map[string]int{"documents": len(view.Documents)}); err != nil {
		return nil, err
	}
	return view, nil
}

func (u *Usecase) ReviewDocument(ctx context.Context, actor entities.Actor, providerID uuid.UUID, documentType string, in dtos.ReviewDocumentInput) (*dtos.ProviderView, error) {
	provider, err := u.existingProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider.Status != entities.ProviderPending {
		return nil, ErrReviewNotAllowed
	}
	subject := enums.DocumentSubject(in.Subject)
	if subject == "" {
		subject = enums.DocumentSubjectProvider
	}
	if !enums.DocumentType(documentType).IsValid() || !subject.IsValid() {
		return nil, ErrDocumentNotFound
	}
	document, err := u.Documents.Document(ctx, providerID, enums.DocumentType(documentType), subject)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, ErrDocumentNotFound
	}
	approved := in.Status == string(entities.DocumentApproved)
	reasonIDs, reasonText := []uuid.UUID{}, ""
	if !approved {
		if reasonIDs, reasonText, err = u.resolveReasons(ctx, entities.RejectionScopeDocument, in.ReasonIDs, in.Note); err != nil {
			return nil, err
		}
	}
	if err := document.Review(approved, reasonText, actor.ID, u.now()); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		if err := u.Documents.ReplaceDocumentReasons(ctx, document, reasonIDs); err != nil {
			return err
		}
		return u.audit(ctx, actor, "provider_document.review", auditEntityDocument, document.ID.String(), map[string]string{"type": document.Type.String(), "subject": document.Subject.String(), "status": string(document.Status), "reason": document.RejectionReason})
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, provider, true)
}

func (u *Usecase) ReviewProvider(ctx context.Context, actor entities.Actor, providerID uuid.UUID, in dtos.ReviewProviderInput) (*dtos.ProviderView, error) {
	if in.Status == string(entities.ProviderApproved) && in.ServiceID == nil {
		return nil, customerrors.FieldError("service_id", "Um prestador aprovado presta exatamente um serviço.")
	}
	provider, err := u.existingProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	now := u.now()
	eventType, serviceChanged := "", false
	reasonIDs, reasonText := []uuid.UUID{}, ""
	if scope, closing := closingScopes[entities.ProviderStatus(in.Status)]; closing {
		if reasonIDs, reasonText, err = u.resolveReasons(ctx, scope, in.ReasonIDs, in.Note); err != nil {
			return nil, err
		}
	}
	switch entities.ProviderStatus(in.Status) {
	case entities.ProviderApproved:
		serviceChanged = provider.ServiceID != nil && *provider.ServiceID != *in.ServiceID
		if err := u.checkApproval(ctx, provider, *in.ServiceID, serviceChanged); err != nil {
			return nil, err
		}
		err, eventType = provider.Approve(*in.ServiceID, now), events.ProviderApproved
	case entities.ProviderRejected:
		err, eventType = provider.Reject(reasonText, now), events.ProviderRejected
	default:
		err, eventType = provider.Suspend(reasonText, now), events.ProviderSuspended
	}
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.Save(ctx, provider); err != nil {
			return err
		}
		if err := u.Documents.ReplaceProviderReasons(ctx, providerID, reasonIDs); err != nil {
			return err
		}
		if serviceChanged {
			if err := u.Providers.ClearCoverage(ctx, providerID); err != nil {
				return err
			}
		}
		if provider.Status == entities.ProviderApproved && u.Approvals != nil {
			if err := u.Approvals.OnApproved(ctx, providerID); err != nil {
				return err
			}
		}
		if err := u.publish(ctx, events.ProviderReviewV1{Type: eventType, ProviderID: providerID, ServiceID: provider.ServiceID, Reason: provider.StatusReason, OccurredAt: now}); err != nil {
			return err
		}
		return u.audit(ctx, actor, "provider.review", auditEntityProvider, providerID.String(), map[string]any{"status": in.Status, "service_id": provider.ServiceID, "reason": provider.StatusReason})
	})
	if err != nil {
		return nil, err
	}
	if provider.Status != entities.ProviderApproved || serviceChanged {
		if err := u.Locator.SetOffline(ctx, providerID); err != nil {
			return nil, err
		}
	}
	return u.view(ctx, provider, true)
}

func (u *Usecase) checkApproval(ctx context.Context, provider *entities.Provider, serviceID uuid.UUID, serviceChanged bool) error {
	if provider.Status != entities.ProviderPending && provider.Status != entities.ProviderSuspended {
		return entities.ErrInvalidProviderTransition
	}
	if serviceChanged {
		active, err := u.Requests.ActiveOfProvider(ctx, provider.UserID)
		if err != nil {
			return err
		}
		if active != nil {
			return ErrProviderBusy
		}
	}
	requirements, err := u.Documents.Requirements(ctx, serviceID)
	if err != nil {
		return err
	}
	if requirements == nil {
		return ErrServiceNotOpen
	}
	documents, err := u.Documents.Documents(ctx, provider.UserID)
	if err != nil {
		return err
	}
	gaps := requirements.NotApproved(documents)
	for _, document := range documents {
		if document.Subject != enums.DocumentSubjectProvider && document.AnyFile() && document.Status != entities.DocumentApproved {
			gaps["documents."+document.Type.String()+"."+document.Subject.String()] = "Ainda não aprovado: " + document.Name() + "."
		}
	}
	for field, message := range requirements.VehicleGaps(provider.Vehicle) {
		gaps[field] = message
	}
	if _, short := gaps["vehicle.seats"]; short {
		gaps["vehicle.seats"] = fmt.Sprintf("O serviço %s exige pelo menos %d lugares.", requirements.ServiceName, requirements.MinVehicleSeats)
	}
	if len(gaps) > 0 {
		return &IncompleteError{Code: "APPROVAL_REQUIREMENTS_NOT_MET", Fields: gaps}
	}
	return nil
}

var closingScopes = map[entities.ProviderStatus]entities.RejectionScope{
	entities.ProviderRejected:  entities.RejectionScopeProvider,
	entities.ProviderSuspended: entities.RejectionScopeSuspension,
}

func distinctIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, repeated := seen[id]; !repeated {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func (u *Usecase) resolveReasons(ctx context.Context, scope entities.RejectionScope, ids []uuid.UUID, note string) ([]uuid.UUID, string, error) {
	distinct := distinctIDs(ids)
	reasons, err := u.Documents.ActiveReasonsByIDs(ctx, scope, distinct)
	if err != nil {
		return nil, "", err
	}
	if len(reasons) != len(distinct) {
		return nil, "", entities.ErrUnknownRejectionReason
	}
	text, err := entities.ComposeRejection(reasons, note)
	if err != nil {
		return nil, "", err
	}
	return distinct, text, nil
}

func toReasonView(reason *entities.RejectionReason) dtos.RejectionReasonView {
	return dtos.RejectionReasonView{
		ID: reason.ID, Scope: string(reason.Scope), Code: reason.Code, Label: reason.Label, RequiresNote: reason.RequiresNote, IsActive: reason.IsActive, DisplayOrder: reason.DisplayOrder, CategoryID: reason.CategoryID,
	}
}

func toReasonViews(reasons []entities.RejectionReason) []dtos.RejectionReasonView {
	out := make([]dtos.RejectionReasonView, 0, len(reasons))
	for i := range reasons {
		out = append(out, toReasonView(&reasons[i]))
	}
	return out
}

func (u *Usecase) ListRejectionReasons(ctx context.Context, scope string, includeInactive bool) (*dtos.RejectionReasonList, error) {
	reasons, err := u.Documents.ListReasons(ctx, scope, includeInactive)
	if err != nil {
		return nil, err
	}
	return &dtos.RejectionReasonList{Items: toReasonViews(reasons)}, nil
}

func (u *Usecase) CreateRejectionReason(ctx context.Context, actor entities.Actor, in dtos.CreateRejectionReasonInput) (*dtos.RejectionReasonView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	reason, err := entities.RejectionReason{
		ID: id, Scope: entities.RejectionScope(in.Scope), Code: in.Code, Label: in.Label, RequiresNote: in.RequiresNote, IsActive: true, DisplayOrder: in.DisplayOrder, CategoryID: in.CategoryID,
	}.Validated()
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.CreateReason(ctx, &reason); err != nil {
			return err
		}
		return u.audit(ctx, actor, "rejection_reason.create", auditEntityReason, reason.ID.String(), reason)
	})
	if err != nil {
		return nil, err
	}
	view := toReasonView(&reason)
	return &view, nil
}

func (u *Usecase) UpdateRejectionReason(ctx context.Context, actor entities.Actor, id uuid.UUID, in dtos.UpdateRejectionReasonInput) (*dtos.RejectionReasonView, error) {
	reason, err := u.Documents.FindReason(ctx, id)
	if err != nil {
		return nil, err
	}
	if reason == nil {
		return nil, ErrRejectionReasonUnknown
	}
	reason.Label, reason.RequiresNote, reason.IsActive, reason.DisplayOrder = in.Label, in.RequiresNote, in.IsActive, in.DisplayOrder
	updated, err := reason.Validated()
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Documents.UpdateReason(ctx, &updated); err != nil {
			return err
		}
		return u.audit(ctx, actor, "rejection_reason.update", auditEntityReason, updated.ID.String(), updated)
	})
	if err != nil {
		return nil, err
	}
	view := toReasonView(&updated)
	return &view, nil
}
