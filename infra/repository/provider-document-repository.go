package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type ProviderDocumentRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IProviderDocumentRepository = (*ProviderDocumentRepository)(nil)

func NewProviderDocumentRepository(pool *pgxpool.Pool) *ProviderDocumentRepository {
	return &ProviderDocumentRepository{pool: pool}
}

func (r *ProviderDocumentRepository) OpenServices(ctx context.Context) ([]repository.OpenService, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListServicesOpenToProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open services: %w", err)
	}
	out := make([]repository.OpenService, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.OpenService{
			ID: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description, CategoryID: row.CategoryID, CategorySlug: row.CategorySlug,
			CategoryName: row.CategoryName, RequiresVehicle: row.RequiresVehicle, MinVehicleSeats: int(row.MinVehicleSeats),
		})
	}
	return out, nil
}

func (r *ProviderDocumentRepository) Requirements(ctx context.Context, serviceID uuid.UUID) (*entities.ProviderRequirements, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetProviderRequirements(ctx, serviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider requirements: %w", err)
	}
	documents, err := queries.ListRequiredDocuments(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("list required documents: %w", err)
	}
	requirements := &entities.ProviderRequirements{
		ServiceID: row.ServiceID, ServiceName: row.ServiceName, RequiresVehicle: row.RequiresVehicle, MinVehicleSeats: int(row.MinVehicleSeats), VehicleKinds: row.VehicleKinds, SelfSignup: row.SelfSignup,
		Documents: make([]entities.DocumentSpec, 0, len(documents)),
	}
	for _, documentType := range documents {
		if spec, known := entities.SpecOf(documentType); known {
			requirements.Documents = append(requirements.Documents, spec)
		}
	}
	return requirements, nil
}

func toProviderDocument(row *gen.ProviderDocument) (*entities.ProviderDocument, error) {
	document := &entities.ProviderDocument{
		ID: row.ID, ProviderID: row.ProviderID, Type: row.DocumentType, Subject: row.Subject, Number: row.Number, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt,
		ReviewFlags: row.ReviewFlags, Status: entities.DocumentStatus(row.Status), RejectionReason: row.RejectionReason, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Files: []entities.DocumentFile{},
	}
	if len(row.Details) > 0 {
		if err := json.Unmarshal(row.Details, &document.Details); err != nil {
			return nil, fmt.Errorf("decode document details: %w", err)
		}
	}
	return document, nil
}

func toDocumentFile(row *gen.ProviderDocumentFile) (entities.DocumentFile, error) {
	file := entities.DocumentFile{
		ID: row.ID, DocumentID: row.DocumentID, ProviderID: row.ProviderID, Slot: row.Slot, Version: int(row.Version), Status: entities.FileStatus(row.Status),
		ObjectKey: row.ObjectKey, ContentType: row.ContentType, SizeBytes: row.SizeBytes, SHA256: row.Sha256, Width: intPointer(row.Width), Height: intPointer(row.Height),
		RejectionCodes: row.RejectionCodes, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if len(row.QualityChecks) > 0 {
		if err := json.Unmarshal(row.QualityChecks, &file.QualityChecks); err != nil {
			return file, fmt.Errorf("decode quality checks: %w", err)
		}
	}
	return file, nil
}

func intPointer(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func int32Pointer(value *int) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(*value) // #nosec G115 -- image dimensions fit in int32
	return &converted
}

func (r *ProviderDocumentRepository) Documents(ctx context.Context, providerID uuid.UUID) ([]*entities.ProviderDocument, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListProviderDocuments(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list provider documents: %w", err)
	}
	files, err := queries.ListProviderDocumentFiles(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list provider document files: %w", err)
	}
	out := make([]*entities.ProviderDocument, 0, len(rows))
	byID := make(map[uuid.UUID]*entities.ProviderDocument, len(rows))
	for i := range rows {
		document, err := toProviderDocument(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, document)
		byID[document.ID] = document
	}
	for i := range files {
		file, err := toDocumentFile(&files[i])
		if err != nil {
			return nil, err
		}
		if document, found := byID[file.DocumentID]; found {
			document.Files = append(document.Files, file)
		}
	}
	return out, nil
}

func (r *ProviderDocumentRepository) withFiles(ctx context.Context, row gen.ProviderDocument, err error) (*entities.ProviderDocument, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider document: %w", err)
	}
	document, err := toProviderDocument(&row)
	if err != nil {
		return nil, err
	}
	files, err := postgres.Queries(ctx, r.pool).ListDocumentFiles(ctx, document.ID)
	if err != nil {
		return nil, fmt.Errorf("list document files: %w", err)
	}
	for i := range files {
		file, err := toDocumentFile(&files[i])
		if err != nil {
			return nil, err
		}
		document.Files = append(document.Files, file)
	}
	return document, nil
}

func (r *ProviderDocumentRepository) Document(ctx context.Context, providerID uuid.UUID, documentType enums.DocumentType, subject enums.DocumentSubject) (*entities.ProviderDocument, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderDocument(ctx, gen.GetProviderDocumentParams{ProviderID: providerID, DocumentType: documentType, Subject: subject})
	return r.withFiles(ctx, row, err)
}

func (r *ProviderDocumentRepository) DocumentByID(ctx context.Context, id uuid.UUID) (*entities.ProviderDocument, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderDocumentByID(ctx, id)
	return r.withFiles(ctx, row, err)
}

func (r *ProviderDocumentRepository) SaveDocument(ctx context.Context, d *entities.ProviderDocument) error {
	details, err := json.Marshal(d.Details)
	if err != nil {
		return fmt.Errorf("encode document details: %w", err)
	}
	flags := d.ReviewFlags
	if flags == nil {
		flags = []string{}
	}
	err = postgres.Queries(ctx, r.pool).SaveProviderDocument(ctx, gen.SaveProviderDocumentParams{
		ID: d.ID, ProviderID: d.ProviderID, DocumentType: d.Type, Subject: d.Subject, Number: d.Number, IssuedAt: d.IssuedAt, ExpiresAt: d.ExpiresAt, Details: details,
		ReviewFlags: flags, Status: string(d.Status), RejectionReason: d.RejectionReason, ReviewedBy: d.ReviewedBy, ReviewedAt: d.ReviewedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("save provider document: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) file(row gen.ProviderDocumentFile, err error) (*entities.DocumentFile, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get document file: %w", err)
	}
	file, err := toDocumentFile(&row)
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *ProviderDocumentRepository) File(ctx context.Context, providerID, fileID uuid.UUID) (*entities.DocumentFile, error) {
	return r.file(postgres.Queries(ctx, r.pool).GetProviderDocumentFile(ctx, gen.GetProviderDocumentFileParams{ID: fileID, ProviderID: providerID}))
}

func (r *ProviderDocumentRepository) FileByID(ctx context.Context, fileID uuid.UUID) (*entities.DocumentFile, error) {
	return r.file(postgres.Queries(ctx, r.pool).GetDocumentFile(ctx, fileID))
}

func (r *ProviderDocumentRepository) NextFileVersion(ctx context.Context, documentID uuid.UUID, slot enums.DocumentSlot) (int, error) {
	version, err := postgres.Queries(ctx, r.pool).NextDocumentFileVersion(ctx, gen.NextDocumentFileVersionParams{DocumentID: documentID, Slot: slot})
	if err != nil {
		return 0, fmt.Errorf("next file version: %w", err)
	}
	return int(version), nil
}

func (r *ProviderDocumentRepository) SaveFile(ctx context.Context, f *entities.DocumentFile) error {
	checks, err := json.Marshal(f.QualityChecks)
	if err != nil {
		return fmt.Errorf("encode quality checks: %w", err)
	}
	codes := f.RejectionCodes
	if codes == nil {
		codes = []string{}
	}
	err = postgres.Queries(ctx, r.pool).SaveDocumentFile(ctx, gen.SaveDocumentFileParams{
		ID: f.ID, DocumentID: f.DocumentID, ProviderID: f.ProviderID, Slot: f.Slot, Version: int32(f.Version), Status: string(f.Status), ObjectKey: f.ObjectKey,
		ContentType: f.ContentType, SizeBytes: f.SizeBytes, Sha256: f.SHA256, Width: int32Pointer(f.Width), Height: int32Pointer(f.Height), QualityChecks: checks,
		RejectionCodes: codes, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}) // #nosec G115 -- versions are small counters
	if err != nil {
		return fmt.Errorf("save document file: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) SupersedeFiles(ctx context.Context, documentID uuid.UUID, slot enums.DocumentSlot, keepID uuid.UUID, now time.Time) error {
	err := postgres.Queries(ctx, r.pool).SupersedeDocumentFiles(ctx, gen.SupersedeDocumentFilesParams{Now: now, DocumentID: documentID, Slot: slot, KeepID: keepID})
	if err != nil {
		return fmt.Errorf("supersede document files: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) StalePendingFiles(ctx context.Context, before time.Time, limit int) ([]entities.DocumentFile, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListStalePendingFiles(ctx, gen.ListStalePendingFilesParams{Before: before, MaxRows: int32(limit)}) // #nosec G115 -- bounded batch size
	if err != nil {
		return nil, fmt.Errorf("list stale uploads: %w", err)
	}
	out := make([]entities.DocumentFile, 0, len(rows))
	for i := range rows {
		file, err := toDocumentFile(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, nil
}

func (r *ProviderDocumentRepository) DeletePendingFile(ctx context.Context, fileID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).DeleteDocumentFile(ctx, fileID); err != nil {
		return fmt.Errorf("delete pending upload: %w", err)
	}
	return nil
}

func toRejectionReason(row *gen.RejectionReason) entities.RejectionReason {
	return entities.RejectionReason{
		ID: row.ID, Scope: entities.RejectionScope(row.Scope), Code: row.Code, Label: row.Label, RequiresNote: row.RequiresNote, IsActive: row.IsActive, CategoryID: row.CategoryID, DisplayOrder: int(row.DisplayOrder),
	}
}

func toRejectionReasons(rows []gen.RejectionReason) []entities.RejectionReason {
	out := make([]entities.RejectionReason, 0, len(rows))
	for i := range rows {
		out = append(out, toRejectionReason(&rows[i]))
	}
	return out
}

func (r *ProviderDocumentRepository) ListReasons(ctx context.Context, scope string, includeInactive bool) ([]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRejectionReasons(ctx, gen.ListRejectionReasonsParams{Scope: scope, IncludeInactive: includeInactive})
	if err != nil {
		return nil, fmt.Errorf("list rejection reasons: %w", err)
	}
	return toRejectionReasons(rows), nil
}

func (r *ProviderDocumentRepository) ActiveReasonsByIDs(ctx context.Context, scope entities.RejectionScope, ids []uuid.UUID) ([]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListActiveRejectionReasonsByIDs(ctx, gen.ListActiveRejectionReasonsByIDsParams{Scope: string(scope), Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("list rejection reasons by id: %w", err)
	}
	return toRejectionReasons(rows), nil
}

func (r *ProviderDocumentRepository) FindReason(ctx context.Context, id uuid.UUID) (*entities.RejectionReason, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRejectionReason(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rejection reason: %w", err)
	}
	reason := toRejectionReason(&row)
	return &reason, nil
}

func (r *ProviderDocumentRepository) CreateReason(ctx context.Context, reason *entities.RejectionReason) error {
	err := postgres.Queries(ctx, r.pool).CreateRejectionReason(ctx, gen.CreateRejectionReasonParams{
		ID: reason.ID, Scope: string(reason.Scope), Code: reason.Code, Label: reason.Label, RequiresNote: reason.RequiresNote, IsActive: reason.IsActive, CategoryID: reason.CategoryID, DisplayOrder: int32(reason.DisplayOrder), // #nosec G115 -- small value
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return repository.ErrRejectionReasonCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create rejection reason: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) UpdateReason(ctx context.Context, reason *entities.RejectionReason) error {
	err := postgres.Queries(ctx, r.pool).UpdateRejectionReason(ctx, gen.UpdateRejectionReasonParams{
		ID: reason.ID, Label: reason.Label, RequiresNote: reason.RequiresNote, IsActive: reason.IsActive, DisplayOrder: int32(reason.DisplayOrder), // #nosec G115 -- small value
	})
	if err != nil {
		return fmt.Errorf("update rejection reason: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) ReplaceProviderReasons(ctx context.Context, providerID uuid.UUID, reasonIDs []uuid.UUID) error {
	queries := postgres.Queries(ctx, r.pool)
	if err := queries.ClearProviderRejectionReasons(ctx, providerID); err != nil {
		return fmt.Errorf("clear provider reasons: %w", err)
	}
	if len(reasonIDs) == 0 {
		return nil
	}
	if err := queries.AddProviderRejectionReasons(ctx, gen.AddProviderRejectionReasonsParams{ProviderID: providerID, ReasonIds: reasonIDs}); err != nil {
		return fmt.Errorf("add provider reasons: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) ProviderReasons(ctx context.Context, providerID uuid.UUID) ([]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListProviderRejectionReasons(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list provider reasons: %w", err)
	}
	return toRejectionReasons(rows), nil
}

func (r *ProviderDocumentRepository) ReplaceDocumentReasons(ctx context.Context, document *entities.ProviderDocument, reasonIDs []uuid.UUID) error {
	queries := postgres.Queries(ctx, r.pool)
	if err := queries.ClearDocumentRejectionReasons(ctx, gen.ClearDocumentRejectionReasonsParams{ProviderID: document.ProviderID, DocumentType: document.Type, Subject: document.Subject}); err != nil {
		return fmt.Errorf("clear document reasons: %w", err)
	}
	if len(reasonIDs) == 0 {
		return nil
	}
	err := queries.AddDocumentRejectionReasons(ctx, gen.AddDocumentRejectionReasonsParams{ProviderID: document.ProviderID, DocumentType: document.Type, Subject: document.Subject, ReasonIds: reasonIDs})
	if err != nil {
		return fmt.Errorf("add document reasons: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) DocumentReasons(ctx context.Context, providerID uuid.UUID) (map[uuid.UUID][]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDocumentRejectionReasons(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list document reasons: %w", err)
	}
	out := map[uuid.UUID][]entities.RejectionReason{}
	for i := range rows {
		row := &rows[i]
		out[row.DocumentID] = append(out[row.DocumentID], entities.RejectionReason{
			ID: row.ID, Scope: entities.RejectionScope(row.Scope), Code: row.Code, Label: row.Label, RequiresNote: row.RequiresNote, IsActive: row.IsActive, DisplayOrder: int(row.DisplayOrder),
		})
	}
	return out, nil
}
