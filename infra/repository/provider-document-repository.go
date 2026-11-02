package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
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
		Documents: make([]entities.DocumentType, 0, len(documents)),
	}
	for _, document := range documents {
		requirements.Documents = append(requirements.Documents, entities.DocumentType{
			Code: document.Code, Name: document.Name, Scope: document.Scope, AllowedContentTypes: document.AllowedContentTypes, MaxSizeBytes: document.MaxSizeBytes,
		})
	}
	return requirements, nil
}

func (r *ProviderDocumentRepository) DocumentType(ctx context.Context, code string) (*entities.DocumentType, error) {
	row, err := postgres.Queries(ctx, r.pool).GetDocumentType(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get document type: %w", err)
	}
	return &entities.DocumentType{Code: row.Code, Name: row.Name, Scope: row.Scope, AllowedContentTypes: row.AllowedContentTypes, MaxSizeBytes: row.MaxSizeBytes}, nil
}

func (r *ProviderDocumentRepository) Documents(ctx context.Context, providerID uuid.UUID) ([]*entities.ProviderDocument, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListProviderDocuments(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list provider documents: %w", err)
	}
	out := make([]*entities.ProviderDocument, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, &entities.ProviderDocument{
			ID: row.ID, ProviderID: row.ProviderID, Type: row.DocumentType, TypeName: row.TypeName, ObjectKey: row.ObjectKey, ContentType: row.ContentType, SizeBytes: row.SizeBytes,
			Status: entities.DocumentStatus(row.Status), RejectionReason: row.RejectionReason, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

func (r *ProviderDocumentRepository) Document(ctx context.Context, providerID uuid.UUID, documentType string) (*entities.ProviderDocument, error) {
	row, err := postgres.Queries(ctx, r.pool).GetProviderDocument(ctx, gen.GetProviderDocumentParams{ProviderID: providerID, DocumentType: documentType})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider document: %w", err)
	}
	return &entities.ProviderDocument{
		ID: row.ID, ProviderID: row.ProviderID, Type: row.DocumentType, TypeName: row.TypeName, ObjectKey: row.ObjectKey, ContentType: row.ContentType, SizeBytes: row.SizeBytes,
		Status: entities.DocumentStatus(row.Status), RejectionReason: row.RejectionReason, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *ProviderDocumentRepository) SaveDocument(ctx context.Context, d *entities.ProviderDocument) error {
	err := postgres.Queries(ctx, r.pool).SaveProviderDocument(ctx, gen.SaveProviderDocumentParams{
		ID: d.ID, ProviderID: d.ProviderID, DocumentType: d.Type, ObjectKey: d.ObjectKey, ContentType: d.ContentType, SizeBytes: d.SizeBytes, Status: string(d.Status),
		RejectionReason: d.RejectionReason, ReviewedBy: d.ReviewedBy, ReviewedAt: d.ReviewedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("save provider document: %w", err)
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

func (r *ProviderDocumentRepository) ReplaceDocumentReasons(ctx context.Context, providerID uuid.UUID, documentType string, reasonIDs []uuid.UUID) error {
	queries := postgres.Queries(ctx, r.pool)
	if err := queries.ClearDocumentRejectionReasons(ctx, gen.ClearDocumentRejectionReasonsParams{ProviderID: providerID, DocumentType: documentType}); err != nil {
		return fmt.Errorf("clear document reasons: %w", err)
	}
	if len(reasonIDs) == 0 {
		return nil
	}
	err := queries.AddDocumentRejectionReasons(ctx, gen.AddDocumentRejectionReasonsParams{ProviderID: providerID, DocumentType: documentType, ReasonIds: reasonIDs})
	if err != nil {
		return fmt.Errorf("add document reasons: %w", err)
	}
	return nil
}

func (r *ProviderDocumentRepository) DocumentReasons(ctx context.Context, providerID uuid.UUID) (map[string][]entities.RejectionReason, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDocumentRejectionReasons(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list document reasons: %w", err)
	}
	out := map[string][]entities.RejectionReason{}
	for i := range rows {
		row := &rows[i]
		out[row.DocumentType] = append(out[row.DocumentType], entities.RejectionReason{
			ID: row.ID, Scope: entities.RejectionScope(row.Scope), Code: row.Code, Label: row.Label, RequiresNote: row.RequiresNote, IsActive: row.IsActive, DisplayOrder: int(row.DisplayOrder),
		})
	}
	return out, nil
}
