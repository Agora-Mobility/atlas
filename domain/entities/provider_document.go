package entities

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type DocumentStatus string

const (
	DocumentUploaded DocumentStatus = "uploaded"
	DocumentApproved DocumentStatus = "approved"
	DocumentRejected DocumentStatus = "rejected"
)

type FileStatus string

const (
	FilePendingUpload FileStatus = "pending_upload"
	FileProcessing    FileStatus = "processing"
	FileAccepted      FileStatus = "accepted"
	FileQualityFailed FileStatus = "quality_failed"
	FileSuperseded    FileStatus = "superseded"
)

const (
	ContentTypeJPEG = "image/jpeg"
	ContentTypePNG  = "image/png"
	ContentTypeWebP = "image/webp"
	ContentTypeHEIC = "image/heic"
	ContentTypePDF  = "application/pdf"
)

var imageContentTypes = []string{ContentTypeJPEG, ContentTypePNG, ContentTypeWebP, ContentTypeHEIC}

const (
	DocumentScopePersonal = "personal"
	DocumentScopeVehicle  = "vehicle"
	DocumentScopeCompany  = "company"
	DocumentScopePayment  = "payment"
)

type DocumentSpec struct {
	Type          enums.DocumentType
	Name          string
	Scope         string
	RequiredSlots []enums.DocumentSlot
	OptionalSlots []enums.DocumentSlot
	PDFSlots      []enums.DocumentSlot
	MaxExtras     int
	Subjects      []enums.DocumentSubject
}

var (
	onlyProvider     = []enums.DocumentSubject{enums.DocumentSubjectProvider}
	personSubjects   = []enums.DocumentSubject{enums.DocumentSubjectProvider, enums.DocumentSubjectLegalRepresentative}
	identitySubjects = []enums.DocumentSubject{enums.DocumentSubjectProvider, enums.DocumentSubjectLegalRepresentative, enums.DocumentSubjectVehicleOwner}
	mainSlot         = []enums.DocumentSlot{enums.DocumentSlotMain}
	twoSides         = []enums.DocumentSlot{enums.DocumentSlotFront, enums.DocumentSlotBack}
)

const MaxWorkshopExtraPhotos = 6

var documentSpecs = map[enums.DocumentType]DocumentSpec{
	enums.DocumentTypeProfilePhoto: {Name: "Foto de perfil", Scope: DocumentScopePersonal, RequiredSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeIDCard: {
		Name: "Bilhete de identidade", Scope: DocumentScopePersonal, RequiredSlots: twoSides,
		OptionalSlots: []enums.DocumentSlot{enums.DocumentSlotRenewalReceipt}, PDFSlots: []enums.DocumentSlot{enums.DocumentSlotRenewalReceipt}, Subjects: identitySubjects,
	},
	enums.DocumentTypeSelfie:         {Name: "Selfie de verificação", Scope: DocumentScopePersonal, RequiredSlots: mainSlot, Subjects: personSubjects},
	enums.DocumentTypeDrivingLicense: {Name: "Carta de condução", Scope: DocumentScopePersonal, RequiredSlots: twoSides, Subjects: onlyProvider},
	enums.DocumentTypeCriminalRecord: {Name: "Registo criminal", Scope: DocumentScopePersonal, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeVehiclePhoto: {
		Name: "Fotografias da viatura", Scope: DocumentScopeVehicle, Subjects: onlyProvider,
		RequiredSlots: []enums.DocumentSlot{enums.DocumentSlotFront, enums.DocumentSlotRear, enums.DocumentSlotSide, enums.DocumentSlotInterior, enums.DocumentSlotPlate},
	},
	enums.DocumentTypeVehicleRegistration: {
		Name: "Livrete", Scope: DocumentScopeVehicle, Subjects: onlyProvider,
		RequiredSlots: []enums.DocumentSlot{enums.DocumentSlotPage1, enums.DocumentSlotPage2},
	},
	enums.DocumentTypeVehicleOwnership:        {Name: "Título de propriedade", Scope: DocumentScopeVehicle, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeVehicleOwnerDeclaration: {Name: "Declaração do proprietário", Scope: DocumentScopeVehicle, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeVehicleInsurance:        {Name: "Seguro", Scope: DocumentScopeVehicle, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeVehicleInspection:       {Name: "Inspeção", Scope: DocumentScopeVehicle, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeBankAccount:             {Name: "Comprovativo de IBAN", Scope: DocumentScopePayment, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeBusinessLicense:         {Name: "Alvará comercial", Scope: DocumentScopeCompany, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeCommercialCertificate:   {Name: "Certidão comercial", Scope: DocumentScopeCompany, RequiredSlots: mainSlot, PDFSlots: mainSlot, Subjects: onlyProvider},
	enums.DocumentTypeTaxID:                   {Name: "NIF da empresa", Scope: DocumentScopeCompany, Subjects: onlyProvider},
	enums.DocumentTypeWorkshopPhoto: {
		Name: "Fotografias da oficina", Scope: DocumentScopeCompany, Subjects: onlyProvider, MaxExtras: MaxWorkshopExtraPhotos,
		RequiredSlots: []enums.DocumentSlot{enums.DocumentSlotFacade, enums.DocumentSlotWorkArea, enums.DocumentSlotEquipment, enums.DocumentSlotReception},
		OptionalSlots: []enums.DocumentSlot{enums.DocumentSlotExtra},
	},
}

func SpecOf(documentType enums.DocumentType) (DocumentSpec, bool) {
	spec, found := documentSpecs[documentType]
	spec.Type = documentType
	return spec, found
}

func DocumentName(documentType enums.DocumentType) string {
	spec, _ := SpecOf(documentType)
	return spec.Name
}

func (s DocumentSpec) AcceptsSlot(slot enums.DocumentSlot) bool {
	return slices.Contains(s.RequiredSlots, slot) || slices.Contains(s.OptionalSlots, slot)
}

func (s DocumentSpec) AcceptsSubject(subject enums.DocumentSubject) bool {
	return slices.Contains(s.Subjects, subject)
}

func (s DocumentSpec) ContentTypes(slot enums.DocumentSlot) []string {
	types := slices.Clone(imageContentTypes)
	if slices.Contains(s.PDFSlots, slot) {
		types = append(types, ContentTypePDF)
	}
	return types
}

func (s DocumentSpec) Allows(slot enums.DocumentSlot, contentType string) bool {
	return slices.Contains(s.ContentTypes(slot), contentType)
}

func (s DocumentSpec) HasFiles() bool {
	return len(s.RequiredSlots) > 0
}

type Extraction struct {
	Status      string             `json:"status"`
	Attempts    int                `json:"attempts"`
	Fields      map[string]string  `json:"fields,omitempty"`
	Confidence  map[string]float64 `json:"confidence,omitempty"`
	Failure     string             `json:"failure,omitempty"`
	RequestedAt *time.Time         `json:"requested_at,omitempty"`
	FinishedAt  *time.Time         `json:"finished_at,omitempty"`
}

type Liveness struct {
	SessionID     string     `json:"session_id,omitempty"`
	Challenge     string     `json:"challenge,omitempty"`
	Status        string     `json:"status,omitempty"`
	Attempts      int        `json:"attempts"`
	FailureReason string     `json:"failure_reason,omitempty"`
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
}

type DocumentDetails struct {
	FullName             string      `json:"full_name,omitempty"`
	BirthDate            string      `json:"birth_date,omitempty"`
	Source               string      `json:"source,omitempty"`
	ManuallyEditedFields []string    `json:"manually_edited_fields,omitempty"`
	Extraction           *Extraction `json:"extraction,omitempty"`
	Liveness             *Liveness   `json:"liveness,omitempty"`
	Categories           []string    `json:"categories,omitempty"`
	OwnerName            string      `json:"owner_name,omitempty"`
	OwnerIsProvider      *bool       `json:"owner_is_provider,omitempty"`
	InsurerID            *uuid.UUID  `json:"insurer_id,omitempty"`
	OtherInsurerName     string      `json:"other_insurer_name,omitempty"`
	BankID               *uuid.UUID  `json:"bank_id,omitempty"`
	HolderName           string      `json:"holder_name,omitempty"`
	OwnerType            string      `json:"owner_type,omitempty"`
	CompanyName          string      `json:"company_name,omitempty"`
	CompanyType          string      `json:"company_type,omitempty"`
}

type ProviderDocument struct {
	ID              uuid.UUID
	ProviderID      uuid.UUID
	Type            enums.DocumentType
	Subject         enums.DocumentSubject
	Number          string
	IssuedAt        *time.Time
	ExpiresAt       *time.Time
	Details         DocumentDetails
	ReviewFlags     []string
	Status          DocumentStatus
	RejectionReason string
	ReviewedBy      *uuid.UUID
	ReviewedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Files           []DocumentFile
}

func NewProviderDocument(providerID uuid.UUID, documentType enums.DocumentType, subject enums.DocumentSubject, now time.Time) (*ProviderDocument, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &ProviderDocument{
		ID: id, ProviderID: providerID, Type: documentType, Subject: subject, Status: DocumentUploaded, ReviewFlags: []string{}, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (d *ProviderDocument) Name() string {
	return DocumentName(d.Type)
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

func (d *ProviderDocument) Edited(now time.Time) {
	d.Status = DocumentUploaded
	d.RejectionReason = ""
	d.ReviewedBy, d.ReviewedAt = nil, nil
	d.UpdatedAt = now
}

func (d *ProviderDocument) Flag(flag string) {
	if !slices.Contains(d.ReviewFlags, flag) {
		d.ReviewFlags = append(d.ReviewFlags, flag)
	}
}

func (d *ProviderDocument) Unflag(flag string) {
	d.ReviewFlags = slices.DeleteFunc(d.ReviewFlags, func(existing string) bool { return existing == flag })
}

func (d *ProviderDocument) NeedsReview() bool {
	return len(d.ReviewFlags) > 0
}

func (d *ProviderDocument) Current(slot enums.DocumentSlot) *DocumentFile {
	var latest *DocumentFile
	for i := range d.Files {
		file := &d.Files[i]
		if file.Slot == slot && file.Status != FileSuperseded && file.Status != FilePendingUpload && (latest == nil || file.Version > latest.Version) {
			latest = file
		}
	}
	return latest
}

func (d *ProviderDocument) Accepted(slot enums.DocumentSlot) []DocumentFile {
	out := []DocumentFile{}
	for i := range d.Files {
		if d.Files[i].Slot == slot && d.Files[i].Status == FileAccepted {
			out = append(out, d.Files[i])
		}
	}
	return out
}

func (d *ProviderDocument) HasAccepted(slot enums.DocumentSlot) bool {
	return len(d.Accepted(slot)) > 0
}

func (d *ProviderDocument) FilesComplete() bool {
	spec, _ := SpecOf(d.Type)
	for _, slot := range spec.RequiredSlots {
		if !d.HasAccepted(slot) {
			return false
		}
	}
	return true
}

func (d *ProviderDocument) AnyFile() bool {
	for i := range d.Files {
		if status := d.Files[i].Status; status != FilePendingUpload && status != FileSuperseded {
			return true
		}
	}
	return false
}

type QualityChecks struct {
	TextLegible     *bool `json:"text_legible,omitempty"`
	NoShadowOnData  *bool `json:"no_shadow_on_data,omitempty"`
	CornersVisible  *bool `json:"corners_visible,omitempty"`
	NoGlare         *bool `json:"no_glare,omitempty"`
	Sharp           *bool `json:"sharp,omitempty"`
	WellLit         *bool `json:"well_lit,omitempty"`
	SingleFace      *bool `json:"single_face,omitempty"`
	MetadataRemoved bool  `json:"metadata_removed"`
}

type DocumentFile struct {
	ID             uuid.UUID
	DocumentID     uuid.UUID
	ProviderID     uuid.UUID
	Slot           enums.DocumentSlot
	Version        int
	Status         FileStatus
	ObjectKey      string
	ContentType    string
	SizeBytes      int64
	SHA256         string
	Width          *int
	Height         *int
	QualityChecks  QualityChecks
	RejectionCodes []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (f *DocumentFile) IsImage() bool {
	return slices.Contains(imageContentTypes, f.ContentType)
}
