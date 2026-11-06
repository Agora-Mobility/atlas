package verification

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

const (
	declarationDriver   = "Declaro que os dados e os documentos que enviei são verdadeiros."
	declarationWorkshop = "Declaro que os dados são verdadeiros e que represento a empresa."
	maskVisibleStart    = 3
	maskVisibleEnd      = 5
)

var companyTypeLabels = map[enums.CompanyType]string{
	enums.CompanyTypeIndividualEntrepreneur: "Empresário em nome individual",
	enums.CompanyTypeSoleProprietorship:     "Sociedade unipessoal",
	enums.CompanyTypeLimitedCompany:         "Sociedade por quotas (Lda)",
	enums.CompanyTypePublicLimitedCompany:   "Sociedade anónima (SA)",
	enums.CompanyTypeCooperative:            "Cooperativa",
}

func maskIDNumber(number string) string {
	if len(number) <= maskVisibleStart+maskVisibleEnd {
		return strings.Repeat("•", len(number))
	}
	return number[:maskVisibleStart] + strings.Repeat("•", len(number)-maskVisibleStart-maskVisibleEnd) + number[len(number)-maskVisibleEnd:]
}

func (u *Usecase) overview(s *session) *dtos.VerificationView {
	snapshot := s.snapshot()
	progress := snapshot.Progress()
	view := &dtos.VerificationView{
		ID: s.verification.ID, Status: string(entities.VerificationStatusOf(s.provider, s.documents)), Type: s.verification.Kind.String(), Locked: !s.provider.IsEditable(),
		Progress:             dtos.ProgressView{Completed: progress.Completed, Total: progress.Total, Percent: progress.Percent},
		EstimatedMinutesLeft: progress.MinutesLeft, Groups: []dtos.GroupView{}, SubmittedAt: s.verification.SubmittedAt,
	}
	if progress.Next != nil {
		next := progress.Next.String()
		view.NextItem = &next
	}
	for _, group := range progress.Groups {
		groupView := dtos.GroupView{Key: group.Key.String(), Completed: group.Completed, Total: group.Total, EstimatedMinutes: group.Minutes, Items: []dtos.ItemView{}}
		for _, item := range group.Items {
			groupView.Items = append(groupView.Items, dtos.ItemView{Key: item.Key.String(), Status: string(item.Status)})
		}
		view.Groups = append(view.Groups, groupView)
	}
	return view
}

func (u *Usecase) Overview(ctx context.Context, providerID uuid.UUID) (*dtos.VerificationView, error) {
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.overview(s), nil
}

func section(key string, step enums.VerificationItem, snapshot *entities.VerificationSnapshot, fields map[string]any) dtos.ReviewSection {
	return dtos.ReviewSection{Key: key, Step: step.String(), Status: string(snapshot.ItemStatus(step)), Fields: fields}
}

func (u *Usecase) bankSection(ctx context.Context, s *session, snapshot *entities.VerificationSnapshot) (dtos.ReviewSection, error) {
	fields := map[string]any{}
	if account := s.document(enums.DocumentTypeBankAccount, enums.DocumentSubjectProvider); account != nil && account.Number != "" {
		fields["iban"], fields["holder_name"] = entities.MaskIBAN(account.Number), account.Details.HolderName
		if account.Details.BankID != nil {
			bank, err := u.Verifications.Bank(ctx, *account.Details.BankID)
			if err != nil {
				return dtos.ReviewSection{}, err
			}
			if bank != nil {
				fields["bank"] = bank.ShortName
			}
		}
	}
	return section("bank_account", enums.VerificationItemBankAccount, snapshot, fields), nil
}

func (u *Usecase) driverSections(s *session, snapshot *entities.VerificationSnapshot) []dtos.ReviewSection {
	personal := map[string]any{}
	if card := s.document(enums.DocumentTypeIDCard, enums.DocumentSubjectProvider); card != nil && card.Details.Source != "" {
		personal["full_name"], personal["birth_date"], personal["id_number"] = card.Details.FullName, card.Details.BirthDate, maskIDNumber(card.Number)
		personal["id_expires_at"] = dateText(card.ExpiresAt)
	}
	personal["province"], personal["municipality"] = s.provider.Province, s.provider.Municipality
	vehicle := map[string]any{}
	if v := s.provider.Vehicle; v.Plate != "" {
		vehicle["plate"], vehicle["brand"], vehicle["model"], vehicle["year"], vehicle["color"], vehicle["seats"] = v.Plate, v.Brand, v.Model, v.Year, v.Color, v.Seats
	}
	return []dtos.ReviewSection{
		section("personal", enums.VerificationItemIdentityCard, snapshot, personal),
		{Key: "service", Step: "profile", Status: string(entities.ItemCompleted), Fields: map[string]any{"service": s.requirements.ServiceName}},
		section("vehicle", enums.VerificationItemVehicle, snapshot, vehicle),
	}
}

func (u *Usecase) workshopSections(ctx context.Context, s *session, snapshot *entities.VerificationSnapshot) ([]dtos.ReviewSection, error) {
	company := map[string]any{}
	if taxID := s.document(enums.DocumentTypeTaxID, enums.DocumentSubjectProvider); taxID != nil {
		company["company_name"], company["nif"], company["company_type"] = taxID.Details.CompanyName, taxID.Number, companyTypeLabels[enums.CompanyType(taxID.Details.CompanyType)]
	}
	if license := s.document(enums.DocumentTypeBusinessLicense, enums.DocumentSubjectProvider); license != nil {
		company["business_license_number"], company["business_license_expires_at"] = license.Number, dateText(license.ExpiresAt)
	}
	representative := map[string]any{}
	if card := s.document(enums.DocumentTypeIDCard, enums.DocumentSubjectLegalRepresentative); card != nil && card.Details.Source != "" {
		representative["full_name"], representative["id_number"] = card.Details.FullName, maskIDNumber(card.Number)
	}
	categories, err := u.categoriesByID(ctx)
	if err != nil {
		return nil, err
	}
	services := []dtos.PricedServiceView{}
	for _, price := range s.verification.Workshop.Services {
		services = append(services, dtos.PricedServiceView{ServiceID: price.ServiceID, Label: categories[price.ServiceID].Label, BasePrice: price.BasePriceKz})
	}
	location := locationView(&s.verification.Workshop)
	return []dtos.ReviewSection{
		section("company", enums.VerificationItemTaxID, snapshot, company),
		section("representative", enums.VerificationItemLegalRepresentative, snapshot, representative),
		section("workshop", enums.VerificationItemWorkshopLocation, snapshot, map[string]any{"address": location.Address, "reference": location.Reference, "opening_hours": location.OpeningHours}),
		section("services", enums.VerificationItemWorkshopServices, snapshot, map[string]any{"services": services}),
	}, nil
}

func (u *Usecase) missing(ctx context.Context, s *session) ([]string, error) {
	snapshot := s.snapshot()
	missing := []string{}
	for _, item := range snapshot.Missing() {
		missing = append(missing, item.String())
	}
	if s.verification.Kind == enums.VerificationKindDriver && len(s.requirements.VehicleGaps(s.provider.Vehicle)) > 0 && snapshot.ItemStatus(enums.VerificationItemVehicle).Done() {
		missing = append(missing, enums.VerificationItemVehicle.String())
	}
	user, err := u.Users.FindByID(ctx, s.provider.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || strings.TrimSpace(user.Name) == "" || s.provider.Province == "" || s.provider.Municipality == "" {
		missing = append([]string{"profile"}, missing...)
	}
	return missing, nil
}

func (u *Usecase) review(ctx context.Context, s *session) (*dtos.ReviewView, error) {
	snapshot := s.snapshot()
	progress := snapshot.Progress()
	view := &dtos.ReviewView{
		Status: string(entities.VerificationStatusOf(s.provider, s.documents)), Type: s.verification.Kind.String(), Declaration: declarationDriver,
		Documents: dtos.DocumentsSummary{Sent: progress.Completed, Total: progress.Total, Items: []dtos.ItemView{}}, SubmittedAt: s.verification.SubmittedAt,
	}
	for _, group := range progress.Groups {
		for _, item := range group.Items {
			view.Documents.Items = append(view.Documents.Items, dtos.ItemView{Key: item.Key.String(), Status: string(item.Status)})
		}
	}
	if s.verification.Kind == enums.VerificationKindWorkshop {
		view.Declaration = declarationWorkshop
		sections, err := u.workshopSections(ctx, s, snapshot)
		if err != nil {
			return nil, err
		}
		view.Sections = sections
	} else {
		view.Sections = u.driverSections(s, snapshot)
	}
	bank, err := u.bankSection(ctx, s, snapshot)
	if err != nil {
		return nil, err
	}
	view.Sections = append(view.Sections, bank)
	if view.Missing, err = u.missing(ctx, s); err != nil {
		return nil, err
	}
	return view, nil
}

func (u *Usecase) Review(ctx context.Context, providerID uuid.UUID) (*dtos.ReviewView, error) {
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return u.review(ctx, s)
}

func (u *Usecase) Submit(ctx context.Context, providerID uuid.UUID, in dtos.SubmitInput) (*dtos.ReviewView, error) {
	if !in.DeclarationAccepted {
		return nil, ErrDeclarationRequired
	}
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if s.provider.Status == entities.ProviderPending && s.verification.SubmittedAt != nil {
		return u.review(ctx, s)
	}
	if !s.provider.IsEditable() {
		return nil, ErrLocked
	}
	missing, err := u.missing(ctx, s)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, entities.NewRuleError(CodeIncomplete, "", "Ainda faltam passos por concluir.").With("missing_items", missing)
	}
	now := u.now()
	if err := s.provider.Submit(now); err != nil {
		return nil, err
	}
	s.verification.Submit(now)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Providers.Save(ctx, s.provider); err != nil {
			return err
		}
		if err := u.Verifications.Save(ctx, s.verification); err != nil {
			return err
		}
		if err := u.publish(ctx, events.ProviderReviewV1{Type: events.ProviderApplicationSubmitted, ProviderID: providerID, ServiceID: s.provider.RequestedServiceID, OccurredAt: now}); err != nil {
			return err
		}
		return u.audit(ctx, providerActor(providerID), "provider_verification.submit", auditEntityVerification, s.verification.ID.String(), map[string]any{
			"kind": s.verification.Kind, "declaration_accepted_at": now,
		})
	})
	if err != nil {
		return nil, err
	}
	return u.review(ctx, s)
}

func (u *Usecase) Banks(ctx context.Context, search string) (*dtos.CatalogList[dtos.BankView], error) {
	banks, err := u.Verifications.Banks(ctx, search)
	if err != nil {
		return nil, err
	}
	list := &dtos.CatalogList[dtos.BankView]{Items: make([]dtos.BankView, 0, len(banks))}
	for i := range banks {
		list.Items = append(list.Items, dtos.BankView{ID: banks[i].ID, Code: banks[i].Code, ShortName: banks[i].ShortName, Name: banks[i].Name})
	}
	return list, nil
}

func (u *Usecase) Insurers(ctx context.Context, search string) (*dtos.CatalogList[dtos.InsurerView], error) {
	insurers, err := u.Verifications.Insurers(ctx, search)
	if err != nil {
		return nil, err
	}
	list := &dtos.CatalogList[dtos.InsurerView]{Items: make([]dtos.InsurerView, 0, len(insurers))}
	for i := range insurers {
		list.Items = append(list.Items, dtos.InsurerView{ID: insurers[i].ID, Name: insurers[i].Name})
	}
	return list, nil
}

func (u *Usecase) WorkshopServices(ctx context.Context) (*dtos.CatalogList[dtos.WorkshopServiceView], error) {
	categories, err := u.Verifications.WorkshopCategories(ctx)
	if err != nil {
		return nil, err
	}
	list := &dtos.CatalogList[dtos.WorkshopServiceView]{Items: make([]dtos.WorkshopServiceView, 0, len(categories))}
	for i := range categories {
		list.Items = append(list.Items, dtos.WorkshopServiceView{ID: categories[i].ID, Code: categories[i].Code, Label: categories[i].Label, Description: categories[i].Description})
	}
	return list, nil
}

func (u *Usecase) DrivingCategories() *dtos.CatalogList[dtos.OptionView] {
	list := &dtos.CatalogList[dtos.OptionView]{Items: []dtos.OptionView{}}
	for _, category := range enums.DrivingCategories() {
		list.Items = append(list.Items, dtos.OptionView{Code: category.String(), Label: category.String()})
	}
	return list
}

func (u *Usecase) CompanyTypes() *dtos.CatalogList[dtos.OptionView] {
	list := &dtos.CatalogList[dtos.OptionView]{Items: []dtos.OptionView{}}
	for _, companyType := range enums.CompanyTypes() {
		list.Items = append(list.Items, dtos.OptionView{Code: companyType.String(), Label: companyTypeLabels[companyType]})
	}
	return list
}

type AdminFileView struct {
	ID            uuid.UUID `json:"id"`
	Slot          string    `json:"slot"`
	Version       int       `json:"version"`
	Status        string    `json:"status"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	Reasons       []string  `json:"reasons"`
	URL           string    `json:"url"`
	URLExpiresAt  time.Time `json:"url_expires_at"`
	UploadedAt    time.Time `json:"uploaded_at"`
	MetadataStrip bool      `json:"metadata_removed"`
}

type AdminDocumentView struct {
	ID              uuid.UUID                `json:"id"`
	Type            string                   `json:"type"`
	Subject         string                   `json:"subject"`
	Name            string                   `json:"name"`
	Status          string                   `json:"status"`
	Number          string                   `json:"number,omitempty"`
	IssuedAt        string                   `json:"issued_at,omitempty"`
	ExpiresAt       string                   `json:"expires_at,omitempty"`
	Details         entities.DocumentDetails `json:"details"`
	ReviewFlags     []string                 `json:"review_flags"`
	RejectionReason string                   `json:"rejection_reason,omitempty"`
	Files           []AdminFileView          `json:"files"`
}

type AdminVerificationView struct {
	Verification *dtos.VerificationView `json:"verification"`
	Review       *dtos.ReviewView       `json:"review"`
	Documents    []AdminDocumentView    `json:"documents"`
}

func (u *Usecase) AdminView(ctx context.Context, actor entities.Actor, providerID uuid.UUID) (*AdminVerificationView, error) {
	s, err := u.open(ctx, providerID)
	if err != nil {
		return nil, err
	}
	review, err := u.review(ctx, s)
	if err != nil {
		return nil, err
	}
	view := &AdminVerificationView{Verification: u.overview(s), Review: review, Documents: []AdminDocumentView{}}
	viewed := []string{}
	for _, document := range s.documents {
		item := AdminDocumentView{
			ID: document.ID, Type: document.Type.String(), Subject: document.Subject.String(), Name: document.Name(), Status: string(document.Status), Number: document.Number,
			IssuedAt: dateText(document.IssuedAt), ExpiresAt: dateText(document.ExpiresAt), Details: document.Details, ReviewFlags: document.ReviewFlags,
			RejectionReason: document.RejectionReason, Files: []AdminFileView{},
		}
		for i := range document.Files {
			file := &document.Files[i]
			if file.Status == entities.FilePendingUpload {
				continue
			}
			signed, err := u.Storage.PresignDownloadFor(ctx, file.URL, u.Settings.ReadURLTTL)
			if err != nil {
				return nil, err
			}
			item.Files = append(item.Files, AdminFileView{
				ID: file.ID, Slot: file.Slot.String(), Version: file.Version, Status: string(file.Status), ContentType: file.ContentType, SizeBytes: file.SizeBytes, SHA256: file.SHA256,
				Reasons: file.RejectionCodes, URL: signed.URL, URLExpiresAt: signed.ExpiresAt, UploadedAt: file.CreatedAt, MetadataStrip: file.QualityChecks.MetadataRemoved,
			})
			viewed = append(viewed, file.ID.String())
		}
		view.Documents = append(view.Documents, item)
	}
	if err := u.audit(ctx, actor, "provider_document.view", auditEntityVerification, s.verification.ID.String(), map[string]any{"provider_id": providerID, "files": viewed}); err != nil {
		return nil, err
	}
	return view, nil
}
