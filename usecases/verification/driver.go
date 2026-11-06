package verification

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

const (
	maxNumberLength  = 64
	maxCompanyLength = 160
)

func normalizedNumber(field, raw string) (string, error) {
	number := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
	if number == "" || len([]rune(number)) > maxNumberLength {
		return "", customerrors.FieldError(field, "Indica o número do documento.")
	}
	return number, nil
}

func (u *Usecase) SetDrivingLicense(ctx context.Context, providerID uuid.UUID, in dtos.DrivingLicenseInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemDrivingLicense)
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
	categories := []string{}
	for _, raw := range in.Categories {
		category := enums.DrivingCategory(strings.ToUpper(strings.TrimSpace(raw)))
		if !category.IsValid() {
			return nil, customerrors.FieldError("categories", "Esta categoria de carta não existe.")
		}
		if !slices.Contains(categories, category.String()) {
			categories = append(categories, category.String())
		}
	}
	if required := s.requirements.DrivingCategories; len(required) > 0 && !slices.ContainsFunc(required, func(c string) bool { return slices.Contains(categories, c) }) {
		return nil, entities.NewRuleError(CodeCategoryRequired, "categories", "O serviço escolhido exige outra categoria de carta.").With("required_any_of", required)
	}
	document, err := u.documentFor(s, enums.DocumentTypeDrivingLicense, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	document.Number, document.ExpiresAt, document.Details.Categories = number, &expiresAt, categories
	document.Edited(u.now())
	if err := u.saveDocument(ctx, document, "provider_document.update", map[string]any{"type": document.Type, "categories": categories, "expires_at": expiresAt}); err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemDrivingLicense), nil
}

func (u *Usecase) SetCriminalRecord(ctx context.Context, providerID uuid.UUID, in dtos.CriminalRecordInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemCriminalRecord)
	if err != nil {
		return nil, err
	}
	number, err := normalizedNumber("certificate_number", in.CertificateNumber)
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
	if !entities.IssuedWithinDays(issuedAt, now, u.Settings.CriminalRecordMaxDays) {
		return nil, entities.NewRuleError(CodeCertificateTooOld, "issued_at", "O registo criminal tem de ter sido emitido há menos de 90 dias.").With("max_days", u.Settings.CriminalRecordMaxDays)
	}
	document, err := u.documentFor(s, enums.DocumentTypeCriminalRecord, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	document.Number, document.IssuedAt = number, &issuedAt
	document.Edited(now)
	if err := u.saveDocument(ctx, document, "provider_document.update", map[string]any{"type": document.Type, "issued_at": issuedAt}); err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemCriminalRecord), nil
}

func (u *Usecase) RemindCriminalRecord(ctx context.Context, providerID uuid.UUID) (*dtos.ReminderView, error) {
	if _, err := u.step(ctx, providerID, enums.VerificationItemCriminalRecord); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := u.now()
	reminder := &entities.VerificationReminder{ID: id, ProviderID: providerID, Kind: entities.ReminderCriminalRecord, DueAt: now.Add(u.Settings.ReminderDelay), CreatedAt: now}
	if err := u.Verifications.ScheduleReminder(ctx, reminder); err != nil {
		return nil, err
	}
	return &dtos.ReminderView{RemindAt: reminder.DueAt}, nil
}

func (u *Usecase) SetVehicle(ctx context.Context, providerID uuid.UUID, in dtos.VehicleInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemVehicle)
	if err != nil {
		return nil, err
	}
	if s.provider.IsFleetDriver() {
		return nil, ErrManagedByFleet
	}
	plate, ok := entities.NormalizePlate(in.Plate)
	if !ok {
		return nil, entities.NewRuleError(CodeInvalidPlate, "plate", "A matrícula tem o formato LD-45-21-AF.")
	}
	now := u.now()
	if limit := s.requirements.MaxVehicleAgeYears; limit != nil && now.Year()-in.Year > *limit {
		return nil, entities.NewRuleError(CodeVehicleTooOld, "year", "A viatura é demasiado antiga para este serviço.").With("max_age_years", *limit).With("min_year", now.Year()-*limit)
	}
	vehicle := entities.Vehicle{Kind: in.Kind, CapacityKg: in.CapacityKg, Brand: in.Brand, Model: in.Model, Year: in.Year, Color: in.Color, Plate: plate, Seats: in.Seats}
	if vehicle.Kind == "" {
		vehicle.Kind = s.provider.Vehicle.Kind
	}
	if err := s.provider.SetVehicle(vehicle, now); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.Save(ctx, s.provider); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(providerID), "provider_vehicle.update", auditEntityVerification, s.verification.ID.String(), map[string]any{"plate": plate, "year": in.Year, "seats": in.Seats})
	})
	if err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemVehicle), nil
}

func (u *Usecase) SetVehicleRegistration(ctx context.Context, providerID uuid.UUID, in dtos.VehicleRegistrationInput) (*dtos.VehicleRegistrationView, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemVehicleRegistration)
	if err != nil {
		return nil, err
	}
	if s.provider.Vehicle.Plate == "" {
		return nil, entities.NewRuleError("VEHICLE_REQUIRED", "plate", "Indica primeiro os dados da viatura.")
	}
	ownerName, err := cleanText("owner_name", in.OwnerName, maxNameLength)
	if err != nil {
		return nil, err
	}
	identityName, err := s.identityName()
	if err != nil {
		return nil, err
	}
	document, err := u.documentFor(s, enums.DocumentTypeVehicleRegistration, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	ownerIsProvider := entities.SameName(ownerName, identityName)
	document.Details.OwnerName, document.Details.OwnerIsProvider = ownerName, &ownerIsProvider
	document.Unflag(entities.FlagOwnerIsNotProvider)
	if !ownerIsProvider {
		document.Flag(entities.FlagOwnerIsNotProvider)
	}
	document.Edited(u.now())
	if err := u.saveDocument(ctx, document, "provider_document.update", map[string]any{"type": document.Type, "owner_is_provider": ownerIsProvider}); err != nil {
		return nil, err
	}
	status := s.snapshot().ItemStatus(enums.VerificationItemVehicleRegistration)
	return &dtos.VehicleRegistrationView{Plate: s.provider.Vehicle.Plate, OwnerName: ownerName, OwnerIsProvider: ownerIsProvider, OwnerDocumentsRequired: !ownerIsProvider, Status: string(status)}, nil
}

func (u *Usecase) SetInsurance(ctx context.Context, providerID uuid.UUID, in dtos.InsuranceInput) (*StepResult, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemVehicleInsurance)
	if err != nil {
		return nil, err
	}
	now := u.now()
	insuranceExpires, err := futureDate("insurance_expires_at", in.InsuranceExpiresAt, now)
	if err != nil {
		return nil, err
	}
	inspectionExpires, err := futureDate("inspection_expires_at", in.InspectionExpiresAt, now)
	if err != nil {
		return nil, err
	}
	other := strings.Join(strings.Fields(in.OtherInsurerName), " ")
	if in.InsurerID != nil {
		insurer, err := u.Verifications.Insurer(ctx, *in.InsurerID)
		if err != nil {
			return nil, err
		}
		if insurer == nil {
			return nil, ErrInsurerNotFound
		}
		other = ""
	} else if other == "" {
		return nil, customerrors.FieldError("insurer_id", "Escolhe a seguradora.")
	}
	insurance, err := u.documentFor(s, enums.DocumentTypeVehicleInsurance, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	inspection, err := u.documentFor(s, enums.DocumentTypeVehicleInspection, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	insurance.ExpiresAt, insurance.Details.InsurerID, insurance.Details.OtherInsurerName = &insuranceExpires, in.InsurerID, other
	insurance.Unflag(entities.FlagOtherInsurer)
	if other != "" {
		insurance.Flag(entities.FlagOtherInsurer)
	}
	inspection.ExpiresAt = &inspectionExpires
	insurance.Edited(now)
	inspection.Edited(now)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for _, document := range []*entities.ProviderDocument{insurance, inspection} {
			if err := u.Documents.SaveDocument(ctx, document); err != nil {
				return err
			}
		}
		return u.audit(ctx, providerActor(providerID), "provider_document.update", auditEntityDocument, insurance.ID.String(), map[string]any{
			"type": insurance.Type, "insurer_id": in.InsurerID, "other_insurer": other != "", "insurance_expires_at": insuranceExpires, "inspection_expires_at": inspectionExpires,
		})
	})
	if err != nil {
		return nil, err
	}
	return u.stepView(s, enums.VerificationItemVehicleInsurance), nil
}

func (u *Usecase) SetBankAccount(ctx context.Context, providerID uuid.UUID, in dtos.BankAccountInput, kind enums.VerificationKind) (*dtos.BankAccountView, error) {
	s, err := u.step(ctx, providerID, enums.VerificationItemBankAccount)
	if err != nil {
		return nil, err
	}
	if s.verification.Kind != kind {
		return nil, ErrStepNotInPlan
	}
	iban := entities.NormalizeIBAN(in.IBAN)
	if !entities.ValidAngolanIBAN(iban) {
		return nil, entities.NewRuleError(CodeInvalidIBAN, "iban", "O IBAN angolano começa por AO06 e tem 25 caracteres.")
	}
	bank, err := u.Verifications.Bank(ctx, in.BankID)
	if err != nil {
		return nil, err
	}
	if bank == nil {
		return nil, ErrBankNotFound
	}
	if bank.Code != entities.BankCodeOf(iban) {
		mismatch := entities.NewRuleError(CodeIBANBankMismatch, "bank_id", "O IBAN não pertence ao banco escolhido.").With("iban_bank_code", entities.BankCodeOf(iban))
		if detected, err := u.Verifications.BankByCode(ctx, entities.BankCodeOf(iban)); err == nil && detected != nil {
			mismatch = mismatch.With("detected_bank", dtos.BankView{ID: detected.ID, Code: detected.Code, ShortName: detected.ShortName, Name: detected.Name})
		}
		return nil, mismatch
	}
	holder, err := cleanText("holder_name", in.HolderName, maxNameLength)
	if err != nil {
		return nil, err
	}
	expected, ownerType, detail := "", entities.OwnerTypePerson, "O titular da conta tem de ser a pessoa do BI."
	if kind == enums.VerificationKindWorkshop {
		ownerType, detail = entities.OwnerTypeCompany, "O titular tem de ser a empresa. Uma conta pessoal não é aceite."
		taxID := s.document(enums.DocumentTypeTaxID, enums.DocumentSubjectProvider)
		if taxID == nil || taxID.Details.CompanyName == "" {
			return nil, entities.NewRuleError("COMPANY_NOT_CONFIRMED", "holder_name", "Indica primeiro o NIF e o nome da empresa.")
		}
		expected = taxID.Details.CompanyName
	} else if expected, err = s.identityName(); err != nil {
		return nil, err
	}
	if !entities.SameName(holder, expected) {
		return nil, entities.NewRuleError(CodeHolderNameMismatch, "holder_name", detail)
	}
	document, err := u.documentFor(s, enums.DocumentTypeBankAccount, enums.DocumentSubjectProvider)
	if err != nil {
		return nil, err
	}
	bankID := bank.ID
	document.Number, document.Details.BankID, document.Details.HolderName, document.Details.OwnerType = iban, &bankID, holder, ownerType
	document.Edited(u.now())
	if err := u.saveDocument(ctx, document, "provider_bank_account.update", map[string]any{"bank_id": bank.ID, "iban": entities.MaskIBAN(iban), "owner_type": ownerType}); err != nil {
		return nil, err
	}
	return &dtos.BankAccountView{
		Bank: dtos.BankView{ID: bank.ID, Code: bank.Code, ShortName: bank.ShortName, Name: bank.Name}, IBAN: entities.MaskIBAN(iban), HolderName: holder, OwnerType: ownerType,
		Status: string(s.snapshot().ItemStatus(enums.VerificationItemBankAccount)),
	}, nil
}

func dateText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.DateOnly)
}
