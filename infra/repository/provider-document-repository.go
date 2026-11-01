package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		ServiceID: row.ServiceID, ServiceName: row.ServiceName, RequiresVehicle: row.RequiresVehicle, MinVehicleSeats: int(row.MinVehicleSeats),
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
