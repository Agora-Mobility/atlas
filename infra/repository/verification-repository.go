package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	workshopCountryCode     = "AO"
	defaultServiceMinutes   = 60
	centsPerKwanza          = 100
	workshopOpeningHoursLen = 120
)

type VerificationRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IVerificationRepository = (*VerificationRepository)(nil)

func NewVerificationRepository(pool *pgxpool.Pool) *VerificationRepository {
	return &VerificationRepository{pool: pool}
}

func (r *VerificationRepository) Find(ctx context.Context, providerID uuid.UUID) (*entities.ProviderVerification, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderVerification(ctx, providerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider verification: %w", err)
	}
	verification := &entities.ProviderVerification{
		ID: row.ID, ProviderID: row.ProviderID, Kind: row.Kind, DeclarationAcceptedAt: row.DeclarationAcceptedAt, SubmittedAt: row.SubmittedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if len(row.Workshop) > 0 {
		if err := json.Unmarshal(row.Workshop, &verification.Workshop); err != nil {
			return nil, fmt.Errorf("decode workshop draft: %w", err)
		}
	}
	return verification, nil
}

func (r *VerificationRepository) Save(ctx context.Context, v *entities.ProviderVerification) error {
	workshop, err := json.Marshal(v.Workshop)
	if err != nil {
		return fmt.Errorf("encode workshop draft: %w", err)
	}
	err = postgres.Queries(ctx, r.pool).SaveProviderVerification(ctx, gen.SaveProviderVerificationParams{
		ID: v.ID, ProviderID: v.ProviderID, Kind: v.Kind, Workshop: workshop, DeclarationAcceptedAt: v.DeclarationAcceptedAt, SubmittedAt: v.SubmittedAt,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("save provider verification: %w", err)
	}
	return nil
}

func toBank(row *gen.Bank) entities.Bank {
	return entities.Bank{ID: row.ID, Code: row.Code, ShortName: row.ShortName, Name: row.Name}
}

func (r *VerificationRepository) Banks(ctx context.Context, search string) ([]entities.Bank, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListBanks(ctx, strings.TrimSpace(search))
	if err != nil {
		return nil, fmt.Errorf("list banks: %w", err)
	}
	out := make([]entities.Bank, 0, len(rows))
	for i := range rows {
		out = append(out, toBank(&rows[i]))
	}
	return out, nil
}

func (r *VerificationRepository) bank(row gen.Bank, err error) (*entities.Bank, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bank: %w", err)
	}
	bank := toBank(&row)
	return &bank, nil
}

func (r *VerificationRepository) Bank(ctx context.Context, id uuid.UUID) (*entities.Bank, error) {
	return r.bank(postgres.Queries(ctx, r.pool).GetBank(ctx, id))
}

func (r *VerificationRepository) BankByCode(ctx context.Context, code string) (*entities.Bank, error) {
	return r.bank(postgres.Queries(ctx, r.pool).GetBankByCode(ctx, code))
}

func (r *VerificationRepository) Insurers(ctx context.Context, search string) ([]entities.Insurer, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListInsurers(ctx, strings.TrimSpace(search))
	if err != nil {
		return nil, fmt.Errorf("list insurers: %w", err)
	}
	out := make([]entities.Insurer, 0, len(rows))
	for i := range rows {
		out = append(out, entities.Insurer{ID: rows[i].ID, Name: rows[i].Name})
	}
	return out, nil
}

func (r *VerificationRepository) Insurer(ctx context.Context, id uuid.UUID) (*entities.Insurer, error) {
	row, err := postgres.Queries(ctx, r.pool).GetInsurer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get insurer: %w", err)
	}
	return &entities.Insurer{ID: row.ID, Name: row.Name}, nil
}

func (r *VerificationRepository) WorkshopCategories(ctx context.Context) ([]entities.WorkshopCategory, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListWorkshopCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workshop categories: %w", err)
	}
	out := make([]entities.WorkshopCategory, 0, len(rows))
	for i := range rows {
		out = append(out, entities.WorkshopCategory{ID: rows[i].ID, Code: rows[i].Code, Label: rows[i].Label, Description: rows[i].Description})
	}
	return out, nil
}

func (r *VerificationRepository) ScheduleReminder(ctx context.Context, reminder *entities.VerificationReminder) error {
	err := postgres.Queries(ctx, r.pool).ScheduleVerificationReminder(ctx, gen.ScheduleVerificationReminderParams{
		ID: reminder.ID, ProviderID: reminder.ProviderID, Kind: reminder.Kind, DocumentID: reminder.DocumentID, ExpiresOn: reminder.ExpiresOn,
		DueAt: reminder.DueAt, CreatedAt: reminder.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("schedule reminder: %w", err)
	}
	return nil
}

func (r *VerificationRepository) DueReminders(ctx context.Context, now time.Time, limit int) ([]entities.VerificationReminder, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDueVerificationReminders(ctx, gen.ListDueVerificationRemindersParams{Now: now, MaxRows: int32(limit)}) // #nosec G115 -- bounded batch size
	if err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	out := make([]entities.VerificationReminder, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		reminder := entities.VerificationReminder{ID: row.ID, ProviderID: row.ProviderID, Kind: row.Kind, DocumentID: row.DocumentID, ExpiresOn: row.ExpiresOn, DueAt: row.DueAt}
		if row.DocumentType != nil {
			reminder.DocumentType = *row.DocumentType
		}
		out = append(out, reminder)
	}
	return out, nil
}

func (r *VerificationRepository) MarkReminderSent(ctx context.Context, id uuid.UUID, now time.Time) error {
	if err := postgres.Queries(ctx, r.pool).MarkVerificationReminderSent(ctx, gen.MarkVerificationReminderSentParams{ID: id, SentAt: &now}); err != nil {
		return fmt.Errorf("mark reminder sent: %w", err)
	}
	return nil
}

func (r *VerificationRepository) ExpiringDocuments(ctx context.Context, from, until time.Time, limit int) ([]entities.ExpiringDocument, error) {
	types := make([]string, 0, len(entities.ExpiringDocumentTypes))
	for _, documentType := range entities.ExpiringDocumentTypes {
		types = append(types, documentType.String())
	}
	rows, err := postgres.Queries(ctx, r.pool).ListExpiringDocuments(ctx, gen.ListExpiringDocumentsParams{
		FromDay: from, UntilDay: until, DocumentTypes: types, MaxRows: int32(limit), // #nosec G115 -- bounded batch size
	})
	if err != nil {
		return nil, fmt.Errorf("list expiring documents: %w", err)
	}
	out := make([]entities.ExpiringDocument, 0, len(rows))
	for i := range rows {
		out = append(out, entities.ExpiringDocument{DocumentID: rows[i].ID, ProviderID: rows[i].ProviderID, DocumentType: rows[i].DocumentType, ExpiresOn: rows[i].ExpiresOn})
	}
	return out, nil
}

func describeHours(hours []entities.WorkshopDayHours) string {
	days := []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
	parts := make([]string, 0, len(hours))
	for _, day := range hours {
		parts = append(parts, fmt.Sprintf("%s %02d:%02d-%02d:%02d", days[day.Weekday], day.OpenMinute/60, day.OpenMinute%60, day.CloseMinute/60, day.CloseMinute%60))
	}
	text := strings.Join(parts, ", ")
	if len([]rune(text)) > workshopOpeningHoursLen {
		text = string([]rune(text)[:workshopOpeningHoursLen])
	}
	return text
}

func (r *VerificationRepository) CreateWorkshop(ctx context.Context, w entities.NewWorkshop, now time.Time) (uuid.UUID, error) {
	queries := postgres.Queries(ctx, r.pool)
	taken, err := queries.CountOrganizationsWithNIF(ctx, &w.NIF)
	if err != nil {
		return uuid.Nil, fmt.Errorf("count organizations with nif: %w", err)
	}
	if taken > 0 {
		return uuid.Nil, repository.ErrNIFTaken
	}
	organizationID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	if err := queries.CreateOrganization(ctx, gen.CreateOrganizationParams{
		ID: organizationID, Type: enums.OrganizationTypePartner, Name: w.CompanyName, Nif: &w.NIF, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("create workshop organization: %w", err)
	}
	address := entities.Address{Line: w.Draft.Address, Reference: w.Draft.Reference, CountryCode: workshopCountryCode}
	if address.ID, err = uuid.NewV7(); err != nil {
		return uuid.Nil, err
	}
	if w.Draft.Lat != nil && w.Draft.Lng != nil {
		address.Point = entities.GeoPoint{Lat: *w.Draft.Lat, Lng: *w.Draft.Lng}
	}
	if err := saveAddress(ctx, queries, &address); err != nil {
		return uuid.Nil, err
	}
	workshopID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	hours := w.Draft.OpenHours()
	if err := queries.SaveWorkshop(ctx, gen.SaveWorkshopParams{
		ID: workshopID, OrganizationID: organizationID, AddressID: address.ID, Name: w.CompanyName, Phone: w.Phone, OpeningHours: describeHours(hours),
		Currency: w.Currency, IsActive: true,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("save workshop: %w", err)
	}
	for _, day := range hours {
		if err := queries.InsertWorkshopHours(ctx, gen.InsertWorkshopHoursParams{
			WorkshopID: workshopID, Weekday: int16(day.Weekday), OpenMinute: int32(day.OpenMinute), CloseMinute: int32(day.CloseMinute), // #nosec G115 -- validated by the entity
		}); err != nil {
			return uuid.Nil, fmt.Errorf("save workshop hours: %w", err)
		}
	}
	for _, price := range w.Draft.Services {
		category := w.Categories[price.ServiceID]
		serviceID, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, err
		}
		categoryID := price.ServiceID
		if err := queries.SaveWorkshopService(ctx, gen.SaveWorkshopServiceParams{
			ID: serviceID, WorkshopID: workshopID, Name: category.Label, Description: category.Description, PriceCents: price.BasePriceKz * centsPerKwanza,
			DurationMinutes: defaultServiceMinutes, IsActive: true, CategoryID: &categoryID,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("save workshop service: %w", err)
		}
	}
	return workshopID, nil
}
