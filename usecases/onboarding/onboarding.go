package onboarding

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
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
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/onboarding/dtos"
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
)

type IncompleteError struct {
	Code   string
	Fields map[string]string
}

func (e *IncompleteError) Error() string {
	return e.Code
}

const (
	sniffBytes          = 512
	auditEntityProvider = "provider_profile"
	auditEntityDocument = "provider_document"
)

var extensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

type CodeSender interface {
	SendSilently(ctx context.Context, purpose enums.OTPPurpose, phone string) error
}

type PasswordRules func(password string) error

type Deps struct {
	Users      repository.IUserRepository
	Profiles   repository.IProviderRepository
	Onboarding repository.IOnboardingRepository
	Requests   repository.IRequestRepository
	Locator    repository.IProviderLocator
	Outbox     repository.IOutboxRepository
	Audit      repository.IAuditLogRepository
	Tx         repository.ITransactionManager
	Hasher     services.IPasswordHasher
	Codes      CodeSender
	Storage    services.IObjectStorage
	Passwords  PasswordRules
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
	if err := u.Passwords(in.Password); err != nil {
		return nil, customerrors.FieldError("password", "A password tem de ter entre 8 e 128 caracteres.")
	}
	hash, err := u.Hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}
	phoneNumber := phone.Normalize(in.Phone)
	existing, err := u.Users.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	var user *entities.User
	if existing != nil {
		if existing.IsVerified || existing.Role != enums.RoleProvider {
			return nil, ErrPhoneTaken
		}
		user = existing
		if err := user.UpdateProfile(in.Name, in.Email, phoneNumber); err != nil {
			return nil, err
		}
		user.Password = hash
	} else if user, err = entities.NewUser(in.Name, in.Email, phoneNumber, hash, enums.RoleProvider); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing != nil {
			if err := u.Users.Update(ctx, user); err != nil {
				return err
			}
		} else {
			if err := u.Users.Create(ctx, user); err != nil {
				return err
			}
			if err := u.publish(ctx, events.UserRegisteredV1{ID: user.ID, Role: user.Role, CreatedAt: user.CreatedAt}); err != nil {
				return err
			}
		}
		return u.Onboarding.CreateProfile(ctx, user.ID)
	})
	if err != nil {
		return nil, err
	}
	if err := u.Codes.SendSilently(ctx, enums.OTPPurposeVerifyPhone, user.Phone); err != nil {
		logger.FromContext(ctx).Warn("verification code not sent after provider sign-up", zap.Error(err))
	}
	return &dtos.RegisteredProvider{ID: user.ID, Name: user.Name, Phone: user.Phone, Status: string(entities.ProviderDraft), IsVerified: user.IsVerified}, nil
}

func (u *Usecase) OpenServices(ctx context.Context) (*dtos.OpenServices, error) {
	open, err := u.Onboarding.OpenServices(ctx)
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

func (u *Usecase) profileOf(ctx context.Context, providerID uuid.UUID) (*entities.ProviderProfile, error) {
	profile, err := u.Profiles.FindProfile(ctx, providerID)
	if err != nil || profile != nil {
		return profile, err
	}
	if err := u.Onboarding.CreateProfile(ctx, providerID); err != nil {
		return nil, err
	}
	return u.Profiles.FindProfile(ctx, providerID)
}

func (u *Usecase) MyProfile(ctx context.Context, providerID uuid.UUID) (*dtos.ProfileView, error) {
	profile, err := u.profileOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.view(ctx, profile, false)
}

func (u *Usecase) editProfile(ctx context.Context, providerID uuid.UUID, change func(*entities.ProviderProfile) error) (*dtos.ProfileView, error) {
	profile, err := u.profileOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if err := change(profile); err != nil {
		return nil, err
	}
	if err := u.Profiles.SaveProfile(ctx, profile); err != nil {
		return nil, err
	}
	return u.view(ctx, profile, false)
}

func (u *Usecase) RequestService(ctx context.Context, providerID uuid.UUID, in dtos.RequestServiceInput) (*dtos.ProfileView, error) {
	requirements, err := u.Onboarding.Requirements(ctx, in.ServiceID)
	if err != nil {
		return nil, err
	}
	if requirements == nil {
		return nil, ErrServiceNotOpen
	}
	return u.editProfile(ctx, providerID, func(profile *entities.ProviderProfile) error {
		return profile.RequestService(in.ServiceID)
	})
}

func (u *Usecase) SetPersonalData(ctx context.Context, providerID uuid.UUID, in dtos.PersonalDataInput) (*dtos.ProfileView, error) {
	birthDate, err := time.Parse(time.DateOnly, in.BirthDate)
	if err != nil {
		return nil, customerrors.FieldError("birth_date", "A data tem de estar no formato AAAA-MM-DD.")
	}
	return u.editProfile(ctx, providerID, func(profile *entities.ProviderProfile) error {
		return profile.SetPersonalData(birthDate, in.IDNumber, in.Address, u.now())
	})
}

func (u *Usecase) SetVehicle(ctx context.Context, providerID uuid.UUID, in dtos.VehicleInput) (*dtos.ProfileView, error) {
	return u.editProfile(ctx, providerID, func(profile *entities.ProviderProfile) error {
		return profile.SetVehicle(entities.ProviderVehicle{Brand: in.Brand, Model: in.Model, Year: in.Year, Color: in.Color, Plate: in.Plate, Seats: in.Seats}, u.now())
	})
}

func (u *Usecase) editableProfile(ctx context.Context, providerID uuid.UUID) (*entities.ProviderProfile, error) {
	profile, err := u.profileOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !profile.IsEditable() {
		return nil, entities.ErrProfileLocked
	}
	return profile, nil
}

func objectPrefix(providerID uuid.UUID, documentType string) string {
	return "providers/" + providerID.String() + "/" + documentType + "/"
}

func (u *Usecase) UploadURL(ctx context.Context, providerID uuid.UUID, in dtos.UploadURLInput) (*dtos.UploadURLOutput, error) {
	if _, err := u.editableProfile(ctx, providerID); err != nil {
		return nil, err
	}
	documentType, err := u.Onboarding.DocumentType(ctx, in.DocumentType)
	if err != nil {
		return nil, err
	}
	if documentType == nil {
		return nil, ErrDocumentTypeUnknown
	}
	if !documentType.Allows(in.ContentType) {
		return nil, ErrContentTypeRejected
	}
	if in.Size > documentType.MaxSizeBytes {
		return nil, ErrFileTooLarge
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	key := objectPrefix(providerID, documentType.Code) + id.String() + extensions[in.ContentType]
	signed, err := u.Storage.PresignUpload(ctx, key, in.ContentType, in.Size)
	if err != nil {
		return nil, err
	}
	return &dtos.UploadURLOutput{UploadURL: signed.URL, ObjectKey: key, ExpiresAt: signed.ExpiresAt, Headers: signed.Headers}, nil
}

func validObjectKey(key string, providerID uuid.UUID, documentType string) bool {
	file, found := strings.CutPrefix(key, objectPrefix(providerID, documentType))
	if !found || strings.Contains(file, "/") {
		return false
	}
	extension := path.Ext(file)
	if _, err := uuid.Parse(strings.TrimSuffix(file, extension)); err != nil {
		return false
	}
	for _, allowed := range extensions {
		if allowed == extension {
			return true
		}
	}
	return false
}

func (u *Usecase) RegisterDocument(ctx context.Context, providerID uuid.UUID, in dtos.RegisterDocumentInput) (*dtos.ProfileView, error) {
	profile, err := u.editableProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	documentType, err := u.Onboarding.DocumentType(ctx, in.DocumentType)
	if err != nil {
		return nil, err
	}
	if documentType == nil {
		return nil, ErrDocumentTypeUnknown
	}
	if !validObjectKey(in.ObjectKey, providerID, documentType.Code) {
		return nil, ErrObjectKeyInvalid
	}
	object, err := u.Storage.Stat(ctx, in.ObjectKey)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, ErrObjectNotFound
	}
	if object.SizeBytes > documentType.MaxSizeBytes {
		u.discard(ctx, in.ObjectKey)
		return nil, ErrFileTooLarge
	}
	head, err := u.Storage.FirstBytes(ctx, in.ObjectKey, sniffBytes)
	if err != nil {
		return nil, err
	}
	detected := strings.TrimSpace(strings.Split(http.DetectContentType(head), ";")[0])
	if !documentType.Allows(detected) || extensions[detected] != path.Ext(in.ObjectKey) {
		u.discard(ctx, in.ObjectKey)
		return nil, ErrContentTypeRejected
	}

	previous, err := u.Onboarding.Document(ctx, providerID, documentType.Code)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := u.now()
	document := &entities.ProviderDocument{
		ID: id, ProviderID: providerID, Type: documentType.Code, ObjectKey: in.ObjectKey, ContentType: detected, SizeBytes: object.SizeBytes,
		Status: entities.DocumentUploaded, CreatedAt: now, UpdatedAt: now,
	}
	if err := u.Onboarding.SaveDocument(ctx, document); err != nil {
		return nil, err
	}
	if previous != nil && previous.ObjectKey != in.ObjectKey {
		u.discard(ctx, previous.ObjectKey)
	}
	return u.view(ctx, profile, false)
}

func (u *Usecase) discard(ctx context.Context, key string) {
	if err := u.Storage.Delete(ctx, key); err != nil {
		logger.FromContext(ctx).Warn("stored object not removed", zap.Error(err))
	}
}

func (u *Usecase) MyDocumentURL(ctx context.Context, providerID uuid.UUID, documentType string) (*dtos.DocumentURLOutput, error) {
	document, err := u.Onboarding.Document(ctx, providerID, documentType)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, ErrDocumentNotFound
	}
	signed, err := u.Storage.PresignDownload(ctx, document.ObjectKey)
	if err != nil {
		return nil, err
	}
	return &dtos.DocumentURLOutput{URL: signed.URL, ExpiresAt: signed.ExpiresAt}, nil
}

func (u *Usecase) Submit(ctx context.Context, providerID uuid.UUID) (*dtos.ProfileView, error) {
	profile, err := u.editableProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	missing, err := u.missingFor(ctx, profile)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, &IncompleteError{Code: "PROFILE_INCOMPLETE", Fields: missing}
	}
	now := u.now()
	if err := profile.Submit(now); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Profiles.SaveProfile(ctx, profile); err != nil {
			return err
		}
		return u.publish(ctx, events.ProviderReviewV1{Type: events.ProviderApplicationSubmitted, ProviderID: providerID, ServiceID: profile.RequestedServiceID, OccurredAt: now})
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, profile, false)
}

func (u *Usecase) missingFor(ctx context.Context, profile *entities.ProviderProfile) (map[string]string, error) {
	user, err := u.Users.FindByID(ctx, profile.UserID)
	if err != nil {
		return nil, err
	}
	if profile.RequestedServiceID == nil {
		return map[string]string{"service": "Escolhe o serviço que queres prestar."}, nil
	}
	requirements, err := u.Onboarding.Requirements(ctx, *profile.RequestedServiceID)
	if err != nil {
		return nil, err
	}
	if requirements == nil {
		return map[string]string{"service": "Este serviço já não aceita prestadores. Escolhe outro."}, nil
	}
	documents, err := u.Onboarding.Documents(ctx, profile.UserID)
	if err != nil {
		return nil, err
	}
	name := ""
	if user != nil {
		name = user.Name
	}
	return requirements.MissingFor(profile, name, documents), nil
}

func (u *Usecase) view(ctx context.Context, profile *entities.ProviderProfile, withLinks bool) (*dtos.ProfileView, error) {
	view := &dtos.ProfileView{
		UserID: profile.UserID, Status: string(profile.Status), StatusReason: profile.StatusReason, RequestedServiceID: profile.RequestedServiceID, ServiceID: profile.ServiceID,
		IDNumber: profile.IDNumber, Address: profile.Address, SubmittedAt: profile.SubmittedAt, ReviewedAt: profile.ReviewedAt,
		RequiredDocuments: []dtos.RequiredDocumentView{}, Documents: []dtos.DocumentView{}, Missing: map[string]string{},
	}
	if profile.BirthDate != nil {
		view.BirthDate = profile.BirthDate.Format(time.DateOnly)
	}
	if profile.Vehicle.Plate != "" {
		v := profile.Vehicle
		view.Vehicle = &dtos.VehicleView{Brand: v.Brand, Model: v.Model, Year: v.Year, Color: v.Color, Plate: v.Plate, Seats: v.Seats}
	}
	user, err := u.Users.FindByID(ctx, profile.UserID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		view.Name, view.Phone, view.Email = user.Name, user.Phone, user.Email
	}
	if profile.RequestedServiceID != nil {
		requirements, err := u.Onboarding.Requirements(ctx, *profile.RequestedServiceID)
		if err != nil {
			return nil, err
		}
		if requirements != nil {
			for _, required := range requirements.Documents {
				view.RequiredDocuments = append(view.RequiredDocuments, dtos.RequiredDocumentView{
					Type: required.Code, Name: required.Name, Scope: required.Scope, AllowedContentTypes: required.AllowedContentTypes, MaxSizeBytes: required.MaxSizeBytes,
				})
			}
		}
	}
	if profile.IsEditable() {
		if view.Missing, err = u.missingFor(ctx, profile); err != nil {
			return nil, err
		}
	}
	documents, err := u.Onboarding.Documents(ctx, profile.UserID)
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		item := dtos.DocumentView{
			Type: document.Type, Name: document.TypeName, Status: string(document.Status), RejectionReason: document.RejectionReason, ContentType: document.ContentType,
			SizeBytes: document.SizeBytes, ReviewedAt: document.ReviewedAt, UpdatedAt: document.UpdatedAt,
		}
		if withLinks {
			signed, err := u.Storage.PresignDownload(ctx, document.ObjectKey)
			if err != nil {
				return nil, err
			}
			item.URL, item.URLExpiresAt = signed.URL, &signed.ExpiresAt
		}
		view.Documents = append(view.Documents, item)
	}
	return view, nil
}

func (u *Usecase) ListApplications(ctx context.Context, status string, page pagination.Params) (*dtos.ApplicationList, error) {
	page = page.Normalize()
	applications, total, err := u.Onboarding.Applications(ctx, status, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.ApplicationList{Items: make([]dtos.ApplicationView, 0, len(applications)), Total: total, Page: page.Page, Size: page.Size}
	for i := range applications {
		application := &applications[i]
		list.Items = append(list.Items, dtos.ApplicationView{
			UserID: application.UserID, Name: application.Name, Phone: application.Phone, Status: string(application.Status), RequestedServiceID: application.RequestedServiceID,
			RequestedServiceName: application.RequestedServiceName, ServiceID: application.ServiceID, VehiclePlate: application.VehiclePlate, SubmittedAt: application.SubmittedAt,
		})
	}
	return list, nil
}

func (u *Usecase) existingProfile(ctx context.Context, providerID uuid.UUID) (*entities.ProviderProfile, error) {
	profile, err := u.Profiles.FindProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrProviderNotFound
	}
	return profile, nil
}

func (u *Usecase) GetApplication(ctx context.Context, providerID uuid.UUID) (*dtos.ProfileView, error) {
	profile, err := u.existingProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.view(ctx, profile, true)
}

func (u *Usecase) ReviewDocument(ctx context.Context, actor entities.Actor, providerID uuid.UUID, documentType string, in dtos.ReviewDocumentInput) (*dtos.ProfileView, error) {
	profile, err := u.existingProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile.Status != entities.ProviderPending {
		return nil, ErrReviewNotAllowed
	}
	document, err := u.Onboarding.Document(ctx, providerID, documentType)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, ErrDocumentNotFound
	}
	if err := document.Review(in.Status == string(entities.DocumentApproved), in.Reason, actor.ID, u.now()); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Onboarding.SaveDocument(ctx, document); err != nil {
			return err
		}
		return u.audit(ctx, actor, "provider_document.review", auditEntityDocument, document.ID.String(), map[string]string{"type": document.Type, "status": string(document.Status), "reason": document.RejectionReason})
	})
	if err != nil {
		return nil, err
	}
	return u.view(ctx, profile, true)
}

func (u *Usecase) ReviewProvider(ctx context.Context, actor entities.Actor, providerID uuid.UUID, in dtos.ReviewProviderInput) (*dtos.ProfileView, error) {
	if in.Status == string(entities.ProviderApproved) && in.ServiceID == nil {
		return nil, customerrors.FieldError("service_id", "Um prestador aprovado presta exatamente um serviço.")
	}
	profile, err := u.existingProfile(ctx, providerID)
	if err != nil {
		return nil, err
	}
	now := u.now()
	eventType, serviceChanged := "", false
	switch entities.ProviderStatus(in.Status) {
	case entities.ProviderApproved:
		serviceChanged = profile.ServiceID != nil && *profile.ServiceID != *in.ServiceID
		if err := u.checkApproval(ctx, profile, *in.ServiceID, serviceChanged); err != nil {
			return nil, err
		}
		err, eventType = profile.Approve(*in.ServiceID, now), events.ProviderApproved
	case entities.ProviderRejected:
		err, eventType = profile.Reject(in.Reason, now), events.ProviderRejected
	default:
		err, eventType = profile.Suspend(in.Reason, now), events.ProviderSuspended
	}
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Profiles.SaveProfile(ctx, profile); err != nil {
			return err
		}
		if serviceChanged {
			if err := u.Profiles.ClearCoverage(ctx, providerID); err != nil {
				return err
			}
		}
		if err := u.publish(ctx, events.ProviderReviewV1{Type: eventType, ProviderID: providerID, ServiceID: profile.ServiceID, Reason: profile.StatusReason, OccurredAt: now}); err != nil {
			return err
		}
		return u.audit(ctx, actor, "provider_profile.review", auditEntityProvider, providerID.String(), map[string]any{"status": in.Status, "service_id": profile.ServiceID, "reason": profile.StatusReason})
	})
	if err != nil {
		return nil, err
	}
	if profile.Status != entities.ProviderApproved || serviceChanged {
		if err := u.Locator.SetOffline(ctx, providerID); err != nil {
			return nil, err
		}
	}
	return u.view(ctx, profile, true)
}

func (u *Usecase) checkApproval(ctx context.Context, profile *entities.ProviderProfile, serviceID uuid.UUID, serviceChanged bool) error {
	if profile.Status != entities.ProviderPending && profile.Status != entities.ProviderSuspended {
		return entities.ErrInvalidProviderTransition
	}
	if serviceChanged {
		active, err := u.Requests.ActiveOfProvider(ctx, profile.UserID)
		if err != nil {
			return err
		}
		if active != nil {
			return ErrProviderBusy
		}
	}
	requirements, err := u.Onboarding.Requirements(ctx, serviceID)
	if err != nil {
		return err
	}
	if requirements == nil {
		return ErrServiceNotOpen
	}
	documents, err := u.Onboarding.Documents(ctx, profile.UserID)
	if err != nil {
		return err
	}
	gaps := requirements.NotApproved(documents)
	if requirements.RequiresVehicle && profile.Vehicle.Seats < requirements.MinVehicleSeats {
		gaps["vehicle.seats"] = fmt.Sprintf("O serviço %s exige pelo menos %d lugares.", requirements.ServiceName, requirements.MinVehicleSeats)
	}
	if len(gaps) > 0 {
		return &IncompleteError{Code: "APPROVAL_REQUIREMENTS_NOT_MET", Fields: gaps}
	}
	return nil
}
