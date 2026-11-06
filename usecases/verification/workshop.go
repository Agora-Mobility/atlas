package verification

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

const maxAddressLength = 255

func (u *Usecase) SetBusinessLicense(ctx context.Context, providerID uuid.UUID, in dtos.BusinessLicenseInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemBusinessLicense)
	if err != nil {
		return nil, err
	}
	number, err := normalizedNumber("number", in.Number)
	if err != nil {
		return nil, err
	}
	expiresAt, err := futureDate("expires_at", in.ExpiresAt, u.now())
	if err != nil {
		return nil, err
	}
	document, err := u.documentFor(s, enums.DocumentTypeBusinessLicense, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	document.Number, document.ExpiresAt = number, &expiresAt
	document.Details.CompanyName = strings.Join(strings.Fields(in.CompanyName), " ")
	document.Flag(entities.FlagRepairActivityPending)
	document.Edited(u.now())
	taxID := s.document(enums.DocumentTypeTaxID, enums.DocumentSubjectProvider)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if taxID != nil {
			flagCompanyName(taxID, document)
			if err := u.Documents.SaveDocument(ctx, taxID); err != nil {
				return err
			}
		}
		if err := u.Documents.SaveDocument(ctx, document); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(providerID), "provider_document.update", auditEntityDocument, document.ID.String(), map[string]any{"type": document.Type, "expires_at": expiresAt})
	})
	if err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemBusinessLicense), nil
}

func flagCompanyName(taxID, license *entities.ProviderDocument) {
	taxID.Unflag(entities.FlagCompanyNameDiffers)
	if license != nil && license.Details.CompanyName != "" && taxID.Details.CompanyName != "" && !entities.SameName(license.Details.CompanyName, taxID.Details.CompanyName) {
		taxID.Flag(entities.FlagCompanyNameDiffers)
	}
}

func (u *Usecase) SetCommercialCertificate(ctx context.Context, providerID uuid.UUID, in dtos.CommercialCertificateInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemCommercialCertificate)
	if err != nil {
		return nil, err
	}
	code, err := normalizedNumber("code", in.Code)
	if err != nil {
		return nil, err
	}
	issuedAt, err := parseDate("issued_at", in.IssuedAt)
	if err != nil {
		return nil, err
	}
	now := u.now()
	if entities.IsFutureDate(issuedAt, now) {
		return nil, customerrors.FieldError("issued_at", "A data de emissão não pode ser futura.")
	}
	if !entities.IssuedWithinMonths(issuedAt, now, u.Settings.CertificateMaxMonths) {
		return nil, entities.NewRuleError(CodeCertificateTooOld, "issued_at", "A certidão comercial tem de ter sido emitida há menos de 6 meses.").With("max_months", u.Settings.CertificateMaxMonths)
	}
	document, err := u.documentFor(s, enums.DocumentTypeCommercialCertificate, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	document.Number, document.IssuedAt = code, &issuedAt
	document.Flag(entities.FlagLegalRepresentative)
	document.Edited(now)
	if err := u.saveDocument(ctx, document, "provider_document.update", map[string]any{"type": document.Type, "issued_at": issuedAt}); err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemCommercialCertificate), nil
}

func (u *Usecase) SetTaxID(ctx context.Context, providerID uuid.UUID, in dtos.TaxIDInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemTaxID)
	if err != nil {
		return nil, err
	}
	nif := strings.Join(strings.Fields(in.NIF), "")
	if !entities.ValidCompanyTaxID(nif) {
		return nil, entities.NewRuleError(CodeInvalidNIF, "nif", "O NIF da empresa tem 10 algarismos.")
	}
	companyName, err := cleanText("company_name", in.CompanyName, maxCompanyLength)
	if err != nil {
		return nil, err
	}
	companyType := enums.CompanyType(in.CompanyType)
	if !companyType.IsValid() {
		return nil, customerrors.FieldError("company_type", "Escolhe o tipo de empresa.")
	}
	document, err := u.documentFor(s, enums.DocumentTypeTaxID, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	document.Number, document.Details.CompanyName, document.Details.CompanyType = nif, companyName, companyType.String()
	flagCompanyName(document, s.document(enums.DocumentTypeBusinessLicense, enums.DocumentSubjectProvider))
	document.Edited(u.now())
	if err := u.saveDocument(ctx, document, "provider_document.update", map[string]any{"type": document.Type, "company_type": companyType, "review_flags": document.ReviewFlags}); err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemTaxID), nil
}

func (u *Usecase) saveDraft(ctx context.Context, s *session, action string, after any) error {
	s.verification.UpdatedAt = u.now()
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Verifications.Save(ctx, s.verification); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(s.provider.UserID), action, auditEntityVerification, s.verification.ID.String(), after)
	})
}

func (u *Usecase) SetLocation(ctx context.Context, providerID uuid.UUID, in dtos.LocationInput) (*dtos.LocationView, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemWorkshopLocation)
	if err != nil {
		return nil, err
	}
	address, err := cleanText("address", in.Address, maxAddressLength)
	if err != nil {
		return nil, err
	}
	days, seen, open := []entities.WorkshopDay{}, map[int]bool{}, 0
	for _, input := range in.OpeningHours {
		day := entities.WorkshopDay{Weekday: *input.Day, Open: input.Open, Close: input.Close, Closed: input.Closed}
		if seen[day.Weekday] {
			return nil, customerrors.FieldError("opening_hours", "Indica cada dia uma só vez.")
		}
		seen[day.Weekday] = true
		_, isOpen, err := day.Hours()
		if err != nil {
			return nil, err
		}
		if !isOpen {
			day.Open, day.Close = "", ""
		} else {
			open++
		}
		days = append(days, day)
	}
	if open == 0 {
		return nil, customerrors.FieldError("opening_hours", "A oficina tem de abrir pelo menos um dia.")
	}
	draft := &s.verification.Workshop
	draft.Address, draft.Reference, draft.Lat, draft.Lng, draft.Days = address, strings.Join(strings.Fields(in.Reference), " "), in.Lat, in.Lng, days
	if err := u.saveDraft(ctx, s, "provider_workshop.location", map[string]any{"open_days": open}); err != nil {
		return nil, err
	}
	return locationView(draft), nil
}

func locationView(draft *entities.WorkshopDraft) *dtos.LocationView {
	view := &dtos.LocationView{Address: draft.Address, Reference: draft.Reference, Lat: draft.Lat, Lng: draft.Lng, OpeningHours: []dtos.OpeningDayView{}}
	for _, day := range draft.Days {
		view.OpeningHours = append(view.OpeningHours, dtos.OpeningDayView{Day: day.Weekday, Open: day.Open, Close: day.Close, Closed: day.Closed})
	}
	return view
}

func (u *Usecase) categoriesByID(ctx context.Context) (map[uuid.UUID]entities.WorkshopCategory, error) {
	categories, err := u.Verifications.WorkshopCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]entities.WorkshopCategory, len(categories))
	for i := range categories {
		out[categories[i].ID] = categories[i]
	}
	return out, nil
}

func (u *Usecase) SetServices(ctx context.Context, providerID uuid.UUID, in dtos.ServicesInput) ([]dtos.PricedServiceView, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemWorkshopServices)
	if err != nil {
		return nil, err
	}
	categories, err := u.categoriesByID(ctx)
	if err != nil {
		return nil, err
	}
	prices, seen := []entities.WorkshopPrice{}, map[uuid.UUID]bool{}
	views := []dtos.PricedServiceView{}
	for i, input := range in.Services {
		category, known := categories[input.ServiceID]
		if !known {
			return nil, customerrors.FieldError(fmt.Sprintf("services[%d].service_id", i), "Este serviço não existe.")
		}
		if seen[input.ServiceID] {
			return nil, customerrors.FieldError(fmt.Sprintf("services[%d].service_id", i), "Este serviço aparece duas vezes.")
		}
		if input.BasePrice <= 0 {
			return nil, customerrors.FieldError(fmt.Sprintf("services[%d].base_price", i), "O preço base é um valor inteiro em Kz, sem IVA, maior que zero.")
		}
		seen[input.ServiceID] = true
		prices = append(prices, entities.WorkshopPrice{ServiceID: input.ServiceID, BasePriceKz: input.BasePrice})
		views = append(views, dtos.PricedServiceView{ServiceID: input.ServiceID, Label: category.Label, BasePrice: input.BasePrice})
	}
	s.verification.Workshop.Services = prices
	if err := u.saveDraft(ctx, s, "provider_workshop.services", map[string]any{"services": len(prices)}); err != nil {
		return nil, err
	}
	return views, nil
}

func (u *Usecase) OnApproved(ctx context.Context, providerID uuid.UUID) error {
	verification, err := u.Verifications.Find(ctx, providerID)
	if err != nil || verification == nil || verification.Kind != enums.VerificationKindWorkshop || verification.Workshop.ID != nil {
		return err
	}
	taxID, err := u.Documents.Document(ctx, providerID, enums.DocumentTypeTaxID, enums.DocumentSubjectProvider)
	if err != nil {
		return err
	}
	if taxID == nil || !verification.Workshop.LocationComplete() {
		return ErrWorkshopIncomplete
	}
	user, err := u.Users.FindByID(ctx, providerID)
	if err != nil {
		return err
	}
	phone := ""
	if user != nil {
		phone = user.Phone
	}
	categories, err := u.categoriesByID(ctx)
	if err != nil {
		return err
	}
	now := u.now()
	workshopID, err := u.Verifications.CreateWorkshop(ctx, entities.NewWorkshop{
		ProviderID: providerID, CompanyName: taxID.Details.CompanyName, NIF: taxID.Number, Phone: phone, Currency: u.Settings.Currency,
		Draft: verification.Workshop, Categories: categories,
	}, now)
	if err != nil {
		return err
	}
	verification.Workshop.ID, verification.UpdatedAt = &workshopID, now
	return u.Verifications.Save(ctx, verification)
}
