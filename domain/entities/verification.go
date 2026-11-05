package entities

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type ItemStatus string

const (
	ItemNotStarted  ItemStatus = "not_started"
	ItemInProgress  ItemStatus = "in_progress"
	ItemCompleted   ItemStatus = "completed"
	ItemNeedsReview ItemStatus = "needs_review"
)

func (s ItemStatus) Done() bool {
	return s == ItemCompleted || s == ItemNeedsReview
}

type VerificationStatus string

const (
	VerificationInProgress   VerificationStatus = "in_progress"
	VerificationSubmitted    VerificationStatus = "submitted"
	VerificationInReview     VerificationStatus = "in_review"
	VerificationNeedsChanges VerificationStatus = "needs_changes"
	VerificationApproved     VerificationStatus = "approved"
	VerificationSuspended    VerificationStatus = "suspended"
)

const (
	FlagManualEntry           = "MANUAL_ENTRY"
	FlagEditedAfterOCR        = "EDITED_AFTER_OCR"
	FlagExpiredWithReceipt    = "EXPIRED_WITH_RENEWAL_RECEIPT"
	FlagLivenessNotVerified   = "LIVENESS_NOT_VERIFIED"
	FlagOwnerIsNotProvider    = "OWNER_IS_NOT_PROVIDER"
	FlagOtherInsurer          = "OTHER_INSURER"
	FlagCompanyNameDiffers    = "COMPANY_NAME_DIFFERS"
	FlagLegalRepresentative   = "REPRESENTATIVE_IN_CERTIFICATE"
	FlagRepairActivityPending = "REPAIR_ACTIVITY_TO_CHECK"
)

const (
	SourceOCR    = "ocr"
	SourceManual = "manual"
)

const (
	ExtractionProcessing = "processing"
	ExtractionCompleted  = "completed"
	ExtractionFailed     = "failed"
)

const (
	LivenessPending     = "pending"
	LivenessAccepted    = "accepted"
	LivenessFailed      = "failed"
	LivenessNeedsReview = "needs_review"
)

const (
	OwnerTypePerson  = "person"
	OwnerTypeCompany = "company"
)

type WorkshopDay struct {
	Weekday int    `json:"weekday"`
	Open    string `json:"open,omitempty"`
	Close   string `json:"close,omitempty"`
	Closed  bool   `json:"closed"`
}

func clockMinutes(clock string) (int, bool) {
	var hour, minute int
	if len(clock) != len("08:00") {
		return 0, false
	}
	if _, err := fmt.Sscanf(clock, "%02d:%02d", &hour, &minute); err != nil || hour < 0 || hour > 24 || minute < 0 || minute > 59 || (hour == 24 && minute != 0) {
		return 0, false
	}
	return hour*60 + minute, true
}

func (d WorkshopDay) Hours() (WorkshopDayHours, bool, error) {
	if d.Weekday < 0 || d.Weekday > 6 {
		return WorkshopDayHours{}, false, validationError("opening_hours.weekday", "O dia da semana tem de estar entre 0 (domingo) e 6 (sábado).")
	}
	if d.Closed {
		return WorkshopDayHours{}, false, nil
	}
	open, okOpen := clockMinutes(d.Open)
	closing, okClose := clockMinutes(d.Close)
	if !okOpen || !okClose {
		return WorkshopDayHours{}, false, validationError("opening_hours", "As horas têm o formato HH:MM, como 08:00.")
	}
	hours := WorkshopDayHours{Weekday: d.Weekday, OpenMinute: open, CloseMinute: closing}
	if err := hours.Validate(); err != nil {
		return WorkshopDayHours{}, false, validationError("opening_hours", "A hora de abertura tem de ser antes da hora de fecho.")
	}
	return hours, true, nil
}

type WorkshopPrice struct {
	ServiceID   uuid.UUID `json:"service_id"`
	BasePriceKz int64     `json:"base_price_kz"`
}

type WorkshopDraft struct {
	Address   string          `json:"address,omitempty"`
	Reference string          `json:"reference,omitempty"`
	Lat       *float64        `json:"lat,omitempty"`
	Lng       *float64        `json:"lng,omitempty"`
	Days      []WorkshopDay   `json:"days,omitempty"`
	Services  []WorkshopPrice `json:"services,omitempty"`
	ID        *uuid.UUID      `json:"workshop_id,omitempty"`
}

func (w WorkshopDraft) OpenHours() []WorkshopDayHours {
	out := []WorkshopDayHours{}
	for _, day := range w.Days {
		if hours, open, err := day.Hours(); err == nil && open {
			out = append(out, hours)
		}
	}
	return out
}

func (w WorkshopDraft) LocationComplete() bool {
	return strings.TrimSpace(w.Address) != "" && w.Lat != nil && w.Lng != nil && len(w.OpenHours()) > 0
}

type ProviderVerification struct {
	ID                    uuid.UUID
	ProviderID            uuid.UUID
	Kind                  enums.VerificationKind
	Workshop              WorkshopDraft
	DeclarationAcceptedAt *time.Time
	SubmittedAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func NewProviderVerification(providerID uuid.UUID, kind enums.VerificationKind, now time.Time) (*ProviderVerification, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &ProviderVerification{ID: id, ProviderID: providerID, Kind: kind, CreatedAt: now, UpdatedAt: now}, nil
}

func (v *ProviderVerification) Submit(now time.Time) {
	v.DeclarationAcceptedAt, v.SubmittedAt, v.UpdatedAt = &now, &now, now
}

type PlanItem struct {
	Key     enums.VerificationItem
	Minutes int
}

type PlanGroup struct {
	Key   enums.VerificationGroup
	Items []PlanItem
}

var driverPlan = []PlanGroup{
	{Key: enums.VerificationGroupAboutYou, Items: []PlanItem{
		{enums.VerificationItemIdentityCard, 1}, {enums.VerificationItemSelfie, 1}, {enums.VerificationItemDrivingLicense, 1}, {enums.VerificationItemCriminalRecord, 1},
	}},
	{Key: enums.VerificationGroupYourVehicle, Items: []PlanItem{
		{enums.VerificationItemVehicle, 3}, {enums.VerificationItemVehicleRegistration, 1}, {enums.VerificationItemVehicleInsurance, 2},
	}},
	{Key: enums.VerificationGroupPayments, Items: []PlanItem{{enums.VerificationItemBankAccount, 1}}},
}

var workshopPlan = []PlanGroup{
	{Key: enums.VerificationGroupAboutCompany, Items: []PlanItem{
		{enums.VerificationItemBusinessLicense, 1}, {enums.VerificationItemCommercialCertificate, 1}, {enums.VerificationItemTaxID, 1},
	}},
	{Key: enums.VerificationGroupRepresentative, Items: []PlanItem{{enums.VerificationItemLegalRepresentative, 2}}},
	{Key: enums.VerificationGroupWorkshop, Items: []PlanItem{
		{enums.VerificationItemWorkshopPhotos, 2}, {enums.VerificationItemWorkshopLocation, 2}, {enums.VerificationItemWorkshopServices, 2},
	}},
	{Key: enums.VerificationGroupPayments, Items: []PlanItem{{enums.VerificationItemBankAccount, 1}}},
}

func PlanOf(kind enums.VerificationKind) []PlanGroup {
	if kind == enums.VerificationKindWorkshop {
		return workshopPlan
	}
	return driverPlan
}

func PlanHas(kind enums.VerificationKind, item enums.VerificationItem) bool {
	for _, group := range PlanOf(kind) {
		for _, planned := range group.Items {
			if planned.Key == item {
				return true
			}
		}
	}
	return false
}

type VerificationSnapshot struct {
	Kind      enums.VerificationKind
	Provider  *Provider
	Documents []*ProviderDocument
	Workshop  WorkshopDraft
}

func (s *VerificationSnapshot) Document(documentType enums.DocumentType, subject enums.DocumentSubject) *ProviderDocument {
	for _, document := range s.Documents {
		if document.Type == documentType && document.Subject == subject {
			return document
		}
	}
	return nil
}

func documentStatus(document *ProviderDocument, dataComplete, dataStarted bool) ItemStatus {
	switch {
	case document == nil && !dataStarted:
		return ItemNotStarted
	case document == nil:
		return ItemInProgress
	case document.Status == DocumentRejected:
		return ItemInProgress
	case document.FilesComplete() && dataComplete && document.NeedsReview():
		return ItemNeedsReview
	case document.FilesComplete() && dataComplete:
		return ItemCompleted
	case document.AnyFile() || dataStarted:
		return ItemInProgress
	}
	return ItemNotStarted
}

func combine(statuses ...ItemStatus) ItemStatus {
	done, review, started := true, false, false
	for _, status := range statuses {
		done = done && status.Done()
		review = review || status == ItemNeedsReview
		started = started || status != ItemNotStarted
	}
	switch {
	case done && review:
		return ItemNeedsReview
	case done:
		return ItemCompleted
	case started:
		return ItemInProgress
	}
	return ItemNotStarted
}

func (s *VerificationSnapshot) identity(subject enums.DocumentSubject) ItemStatus {
	document := s.Document(enums.DocumentTypeIDCard, subject)
	if document == nil {
		return ItemNotStarted
	}
	return documentStatus(document, document.Number != "" && document.Details.Source != "", document.Number != "")
}

func (s *VerificationSnapshot) selfie(subject enums.DocumentSubject) ItemStatus {
	document := s.Document(enums.DocumentTypeSelfie, subject)
	if document == nil {
		return ItemNotStarted
	}
	liveness := document.Details.Liveness
	verified := liveness != nil && (liveness.Status == LivenessAccepted || liveness.Status == LivenessNeedsReview)
	return documentStatus(document, verified, liveness != nil)
}

func (s *VerificationSnapshot) simple(documentType enums.DocumentType, complete func(*ProviderDocument) bool) ItemStatus {
	document := s.Document(documentType, enums.DocumentSubjectProvider)
	if document == nil {
		return ItemNotStarted
	}
	return documentStatus(document, complete(document), document.Number != "" || document.IssuedAt != nil || document.ExpiresAt != nil)
}

func (s *VerificationSnapshot) vehicle() ItemStatus {
	hasVehicle := s.Provider != nil && s.Provider.Vehicle.Plate != ""
	return documentStatus(s.Document(enums.DocumentTypeVehiclePhoto, enums.DocumentSubjectProvider), hasVehicle, hasVehicle)
}

func (s *VerificationSnapshot) registration() ItemStatus {
	document := s.Document(enums.DocumentTypeVehicleRegistration, enums.DocumentSubjectProvider)
	if document == nil {
		return ItemNotStarted
	}
	status := documentStatus(document, document.Details.OwnerName != "", document.Details.OwnerName != "")
	if document.Details.OwnerIsProvider == nil || *document.Details.OwnerIsProvider {
		return status
	}
	declaration := documentStatus(s.Document(enums.DocumentTypeVehicleOwnerDeclaration, enums.DocumentSubjectProvider), true, false)
	ownerCard := documentStatus(s.Document(enums.DocumentTypeIDCard, enums.DocumentSubjectVehicleOwner), true, false)
	if status == ItemCompleted {
		status = ItemNeedsReview
	}
	return combine(status, declaration, ownerCard)
}

func (s *VerificationSnapshot) insurance() ItemStatus {
	insurance := s.simple(enums.DocumentTypeVehicleInsurance, func(d *ProviderDocument) bool {
		return d.ExpiresAt != nil && (d.Details.InsurerID != nil || d.Details.OtherInsurerName != "")
	})
	inspection := s.simple(enums.DocumentTypeVehicleInspection, func(d *ProviderDocument) bool { return d.ExpiresAt != nil })
	return combine(insurance, inspection)
}

func (s *VerificationSnapshot) workshopLocation() ItemStatus {
	switch {
	case s.Workshop.LocationComplete():
		return ItemCompleted
	case s.Workshop.Address != "" || len(s.Workshop.Days) > 0:
		return ItemInProgress
	}
	return ItemNotStarted
}

func (s *VerificationSnapshot) ItemStatus(item enums.VerificationItem) ItemStatus {
	withNumberAnd := func(check func(*ProviderDocument) bool) func(*ProviderDocument) bool {
		return func(d *ProviderDocument) bool { return d.Number != "" && check(d) }
	}
	switch item {
	case enums.VerificationItemIdentityCard:
		return s.identity(enums.DocumentSubjectProvider)
	case enums.VerificationItemSelfie:
		return s.selfie(enums.DocumentSubjectProvider)
	case enums.VerificationItemDrivingLicense:
		return s.simple(enums.DocumentTypeDrivingLicense, withNumberAnd(func(d *ProviderDocument) bool { return d.ExpiresAt != nil && len(d.Details.Categories) > 0 }))
	case enums.VerificationItemCriminalRecord:
		return s.simple(enums.DocumentTypeCriminalRecord, withNumberAnd(func(d *ProviderDocument) bool { return d.IssuedAt != nil }))
	case enums.VerificationItemVehicle:
		return s.vehicle()
	case enums.VerificationItemVehicleRegistration:
		return s.registration()
	case enums.VerificationItemVehicleInsurance:
		return s.insurance()
	case enums.VerificationItemBankAccount:
		return s.simple(enums.DocumentTypeBankAccount, withNumberAnd(func(d *ProviderDocument) bool { return d.Details.BankID != nil && d.Details.HolderName != "" }))
	case enums.VerificationItemBusinessLicense:
		return s.simple(enums.DocumentTypeBusinessLicense, withNumberAnd(func(d *ProviderDocument) bool { return d.ExpiresAt != nil }))
	case enums.VerificationItemCommercialCertificate:
		return s.simple(enums.DocumentTypeCommercialCertificate, withNumberAnd(func(d *ProviderDocument) bool { return d.IssuedAt != nil }))
	case enums.VerificationItemTaxID:
		return s.simple(enums.DocumentTypeTaxID, withNumberAnd(func(d *ProviderDocument) bool { return d.Details.CompanyName != "" && d.Details.CompanyType != "" }))
	case enums.VerificationItemLegalRepresentative:
		return combine(s.identity(enums.DocumentSubjectLegalRepresentative), s.selfie(enums.DocumentSubjectLegalRepresentative))
	case enums.VerificationItemWorkshopPhotos:
		return documentStatus(s.Document(enums.DocumentTypeWorkshopPhoto, enums.DocumentSubjectProvider), true, false)
	case enums.VerificationItemWorkshopLocation:
		return s.workshopLocation()
	case enums.VerificationItemWorkshopServices:
		if len(s.Workshop.Services) > 0 {
			return ItemCompleted
		}
		return ItemNotStarted
	}
	return ItemNotStarted
}

type ItemProgress struct {
	Key    enums.VerificationItem
	Status ItemStatus
}

type GroupProgress struct {
	Key       enums.VerificationGroup
	Completed int
	Total     int
	Minutes   int
	Items     []ItemProgress
}

type Progress struct {
	Completed   int
	Total       int
	Percent     int
	MinutesLeft int
	Next        *enums.VerificationItem
	Groups      []GroupProgress
}

func (s *VerificationSnapshot) Progress() Progress {
	progress := Progress{Groups: []GroupProgress{}}
	for _, group := range PlanOf(s.Kind) {
		current := GroupProgress{Key: group.Key, Items: []ItemProgress{}}
		for _, planned := range group.Items {
			status := s.ItemStatus(planned.Key)
			current.Items = append(current.Items, ItemProgress{Key: planned.Key, Status: status})
			current.Total++
			current.Minutes += planned.Minutes
			if status.Done() {
				current.Completed++
				continue
			}
			progress.MinutesLeft += planned.Minutes
			if progress.Next == nil {
				key := planned.Key
				progress.Next = &key
			}
		}
		progress.Completed += current.Completed
		progress.Total += current.Total
		progress.Groups = append(progress.Groups, current)
	}
	if progress.Total > 0 {
		progress.Percent = progress.Completed * 100 / progress.Total
	}
	return progress
}

func (s *VerificationSnapshot) Missing() []enums.VerificationItem {
	missing := []enums.VerificationItem{}
	for _, group := range PlanOf(s.Kind) {
		for _, planned := range group.Items {
			if !s.ItemStatus(planned.Key).Done() {
				missing = append(missing, planned.Key)
			}
		}
	}
	return missing
}

func VerificationStatusOf(provider *Provider, documents []*ProviderDocument) VerificationStatus {
	switch provider.Status {
	case ProviderPending:
		for _, document := range documents {
			if document.ReviewedAt != nil && provider.SubmittedAt != nil && document.ReviewedAt.After(*provider.SubmittedAt) {
				return VerificationInReview
			}
		}
		return VerificationSubmitted
	case ProviderRejected:
		return VerificationNeedsChanges
	case ProviderApproved:
		return VerificationApproved
	case ProviderSuspended:
		return VerificationSuspended
	}
	return VerificationInProgress
}

type RuleError struct {
	Code   string
	Field  string
	Detail string
	Data   map[string]any
}

func (e *RuleError) Error() string {
	return e.Code
}

func NewRuleError(code, field, detail string) *RuleError {
	return &RuleError{Code: code, Field: field, Detail: detail}
}

func (e *RuleError) With(key string, value any) *RuleError {
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	e.Data[key] = value
	return e
}
