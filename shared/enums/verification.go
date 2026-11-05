package enums

type VerificationItem string

const (
	VerificationItemIdentityCard          VerificationItem = "identity_card"
	VerificationItemSelfie                VerificationItem = "selfie"
	VerificationItemDrivingLicense        VerificationItem = "driving_license"
	VerificationItemCriminalRecord        VerificationItem = "criminal_record"
	VerificationItemVehicle               VerificationItem = "vehicle"
	VerificationItemVehicleRegistration   VerificationItem = "vehicle_registration"
	VerificationItemVehicleInsurance      VerificationItem = "vehicle_insurance"
	VerificationItemBankAccount           VerificationItem = "bank_account"
	VerificationItemBusinessLicense       VerificationItem = "business_license"
	VerificationItemCommercialCertificate VerificationItem = "commercial_certificate"
	VerificationItemTaxID                 VerificationItem = "tax_id"
	VerificationItemLegalRepresentative   VerificationItem = "legal_representative"
	VerificationItemWorkshopPhotos        VerificationItem = "workshop_photos"
	VerificationItemWorkshopLocation      VerificationItem = "workshop_location"
	VerificationItemWorkshopServices      VerificationItem = "workshop_services"
)

func (i VerificationItem) String() string {
	return string(i)
}

type VerificationGroup string

const (
	VerificationGroupAboutYou       VerificationGroup = "about_you"
	VerificationGroupYourVehicle    VerificationGroup = "your_vehicle"
	VerificationGroupPayments       VerificationGroup = "payments"
	VerificationGroupAboutCompany   VerificationGroup = "about_company"
	VerificationGroupRepresentative VerificationGroup = "representative"
	VerificationGroupWorkshop       VerificationGroup = "workshop"
)

func (g VerificationGroup) String() string {
	return string(g)
}

type DrivingCategory string

var drivingCategories = []DrivingCategory{"A1", "A", "B1", "B", "BE", "C1", "C1E", "C", "CE", "D1", "D1E", "D", "DE"}

func DrivingCategories() []DrivingCategory {
	return append([]DrivingCategory(nil), drivingCategories...)
}

func (c DrivingCategory) IsValid() bool {
	for _, known := range drivingCategories {
		if known == c {
			return true
		}
	}
	return false
}

func (c DrivingCategory) String() string {
	return string(c)
}

type CompanyType string

const (
	CompanyTypeIndividualEntrepreneur CompanyType = "individual_entrepreneur"
	CompanyTypeSoleProprietorship     CompanyType = "sole_proprietorship"
	CompanyTypeLimitedCompany         CompanyType = "limited_company"
	CompanyTypePublicLimitedCompany   CompanyType = "public_limited_company"
	CompanyTypeCooperative            CompanyType = "cooperative"
)

var companyTypes = []CompanyType{
	CompanyTypeIndividualEntrepreneur, CompanyTypeSoleProprietorship, CompanyTypeLimitedCompany, CompanyTypePublicLimitedCompany, CompanyTypeCooperative,
}

func CompanyTypes() []CompanyType {
	return append([]CompanyType(nil), companyTypes...)
}

func (t CompanyType) IsValid() bool {
	for _, known := range companyTypes {
		if known == t {
			return true
		}
	}
	return false
}

func (t CompanyType) String() string {
	return string(t)
}
