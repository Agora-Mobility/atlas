package enums

import "slices"

type DocumentType string

const (
	DocumentTypeProfilePhoto            DocumentType = "profile_photo"
	DocumentTypeIDCard                  DocumentType = "id_card"
	DocumentTypeSelfie                  DocumentType = "selfie"
	DocumentTypeDrivingLicense          DocumentType = "driving_license"
	DocumentTypeCriminalRecord          DocumentType = "criminal_record"
	DocumentTypeVehiclePhoto            DocumentType = "vehicle_photo"
	DocumentTypeVehicleRegistration     DocumentType = "vehicle_registration"
	DocumentTypeVehicleOwnership        DocumentType = "vehicle_ownership"
	DocumentTypeVehicleOwnerDeclaration DocumentType = "vehicle_owner_declaration"
	DocumentTypeVehicleInsurance        DocumentType = "vehicle_insurance"
	DocumentTypeVehicleInspection       DocumentType = "vehicle_inspection"
	DocumentTypeBankAccount             DocumentType = "bank_account"
	DocumentTypeBusinessLicense         DocumentType = "business_license"
	DocumentTypeCommercialCertificate   DocumentType = "commercial_certificate"
	DocumentTypeTaxID                   DocumentType = "tax_id"
	DocumentTypeWorkshopPhoto           DocumentType = "workshop_photo"
)

var documentTypes = []DocumentType{
	DocumentTypeProfilePhoto, DocumentTypeIDCard, DocumentTypeSelfie, DocumentTypeDrivingLicense, DocumentTypeCriminalRecord,
	DocumentTypeVehiclePhoto, DocumentTypeVehicleRegistration, DocumentTypeVehicleOwnership, DocumentTypeVehicleOwnerDeclaration,
	DocumentTypeVehicleInsurance, DocumentTypeVehicleInspection, DocumentTypeBankAccount, DocumentTypeBusinessLicense,
	DocumentTypeCommercialCertificate, DocumentTypeTaxID, DocumentTypeWorkshopPhoto,
}

func DocumentTypes() []DocumentType {
	return slices.Clone(documentTypes)
}

func (t DocumentType) IsValid() bool {
	return slices.Contains(documentTypes, t)
}

func (t DocumentType) String() string {
	return string(t)
}

type DocumentSlot string

const (
	DocumentSlotMain           DocumentSlot = "main"
	DocumentSlotFront          DocumentSlot = "front"
	DocumentSlotBack           DocumentSlot = "back"
	DocumentSlotRenewalReceipt DocumentSlot = "renewal_receipt"
	DocumentSlotPage1          DocumentSlot = "page_1"
	DocumentSlotPage2          DocumentSlot = "page_2"
	DocumentSlotRear           DocumentSlot = "rear"
	DocumentSlotSide           DocumentSlot = "side"
	DocumentSlotInterior       DocumentSlot = "interior"
	DocumentSlotPlate          DocumentSlot = "plate"
	DocumentSlotFacade         DocumentSlot = "facade"
	DocumentSlotWorkArea       DocumentSlot = "work_area"
	DocumentSlotEquipment      DocumentSlot = "equipment"
	DocumentSlotReception      DocumentSlot = "reception"
	DocumentSlotExtra          DocumentSlot = "extra"
)

var documentSlots = []DocumentSlot{
	DocumentSlotMain, DocumentSlotFront, DocumentSlotBack, DocumentSlotRenewalReceipt, DocumentSlotPage1, DocumentSlotPage2,
	DocumentSlotRear, DocumentSlotSide, DocumentSlotInterior, DocumentSlotPlate, DocumentSlotFacade, DocumentSlotWorkArea,
	DocumentSlotEquipment, DocumentSlotReception, DocumentSlotExtra,
}

func (s DocumentSlot) IsValid() bool {
	return slices.Contains(documentSlots, s)
}

func (s DocumentSlot) String() string {
	return string(s)
}

type DocumentSubject string

const (
	DocumentSubjectProvider            DocumentSubject = "provider"
	DocumentSubjectLegalRepresentative DocumentSubject = "legal_representative"
	DocumentSubjectVehicleOwner        DocumentSubject = "vehicle_owner"
)

func (s DocumentSubject) IsValid() bool {
	switch s {
	case DocumentSubjectProvider, DocumentSubjectLegalRepresentative, DocumentSubjectVehicleOwner:
		return true
	}
	return false
}

func (s DocumentSubject) String() string {
	return string(s)
}

type VerificationKind string

const (
	VerificationKindDriver   VerificationKind = "driver"
	VerificationKindWorkshop VerificationKind = "workshop"
)

func (k VerificationKind) IsValid() bool {
	return k == VerificationKindDriver || k == VerificationKindWorkshop
}

func (k VerificationKind) String() string {
	return string(k)
}
