package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type LiveRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ILiveRepository = (*LiveRepository)(nil)

func NewLiveRepository(pool *pgxpool.Pool) *LiveRepository {
	return &LiveRepository{pool: pool}
}

func (r *LiveRepository) AddMessage(ctx context.Context, m *entities.RequestMessage) error {
	err := postgres.Queries(ctx, r.pool).InsertRequestMessage(ctx, gen.InsertRequestMessageParams{ID: m.ID, RequestID: m.RequestID, SenderID: m.SenderID, SenderType: m.SenderType, Body: m.Body, QuickReplyID: m.QuickReplyID, CreatedAt: m.CreatedAt})
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}

func (r *LiveRepository) Messages(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]entities.RequestMessage, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRequestMessages(ctx, gen.ListRequestMessagesParams{RequestID: requestID, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	out := make([]entities.RequestMessage, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, entities.RequestMessage{ID: row.ID, RequestID: row.RequestID, SenderID: row.SenderID, SenderType: row.SenderType, Body: row.Body, QuickReplyID: row.QuickReplyID, ReadAt: row.ReadAt, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (r *LiveRepository) MarkRead(ctx context.Context, requestID, readerID uuid.UUID, at time.Time) error {
	if err := postgres.Queries(ctx, r.pool).MarkRequestMessagesRead(ctx, gen.MarkRequestMessagesReadParams{ReadAt: &at, RequestID: requestID, ReaderID: readerID}); err != nil {
		return fmt.Errorf("mark messages read: %w", err)
	}
	return nil
}

func (r *LiveRepository) CreateShare(ctx context.Context, id, requestID uuid.UUID, tokenHash string, expiresAt, now time.Time) error {
	err := postgres.Queries(ctx, r.pool).InsertRequestShare(ctx, gen.InsertRequestShareParams{ID: id, RequestID: requestID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: now})
	if err != nil {
		return fmt.Errorf("create share: %w", err)
	}
	return nil
}

func (r *LiveRepository) ShareByHash(ctx context.Context, tokenHash string) (*repository.RequestShare, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRequestShareByHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get share: %w", err)
	}
	return &repository.RequestShare{ID: row.ID, RequestID: row.RequestID, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt}, nil
}

func (r *LiveRepository) RevokeShares(ctx context.Context, requestID uuid.UUID, at time.Time) error {
	if err := postgres.Queries(ctx, r.pool).RevokeRequestShares(ctx, gen.RevokeRequestSharesParams{RequestID: requestID, RevokedAt: &at}); err != nil {
		return fmt.Errorf("revoke shares: %w", err)
	}
	return nil
}

func toSOS(row *gen.SosAlert) entities.SOSAlert {
	alert := entities.SOSAlert{ID: row.ID, RequestID: row.RequestID, UserID: row.UserID, Note: row.Note, Status: row.Status, ResolvedAt: row.ResolvedAt, CreatedAt: row.CreatedAt}
	if row.Lat != nil && row.Lng != nil {
		alert.Point = &entities.GeoPoint{Lat: *row.Lat, Lng: *row.Lng}
	}
	return alert
}

func (r *LiveRepository) CreateSOS(ctx context.Context, a *entities.SOSAlert) (bool, error) {
	params := gen.InsertSOSAlertParams{ID: a.ID, RequestID: a.RequestID, UserID: a.UserID, Note: a.Note, CreatedAt: a.CreatedAt}
	if a.Point != nil {
		params.Lat, params.Lng = &a.Point.Lat, &a.Point.Lng
	}
	rows, err := postgres.Queries(ctx, r.pool).InsertSOSAlert(ctx, params)
	if err != nil {
		return false, fmt.Errorf("create sos: %w", err)
	}
	return rows > 0, nil
}

func (r *LiveRepository) OpenSOS(ctx context.Context, requestID, userID uuid.UUID) (*entities.SOSAlert, error) {
	row, err := postgres.Queries(ctx, r.pool).GetOpenSOSAlert(ctx, gen.GetOpenSOSAlertParams{RequestID: requestID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sos: %w", err)
	}
	alert := toSOS(&row)
	return &alert, nil
}

func (r *LiveRepository) ListSOS(ctx context.Context, status string, limit, offset int) ([]entities.SOSAlert, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListSOSAlerts(ctx, gen.ListSOSAlertsParams{Status: status, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list sos: %w", err)
	}
	total, err := queries.CountSOSAlerts(ctx, status)
	if err != nil {
		return nil, 0, fmt.Errorf("count sos: %w", err)
	}
	out := make([]entities.SOSAlert, 0, len(rows))
	for i := range rows {
		out = append(out, toSOS(&rows[i]))
	}
	return out, total, nil
}

func (r *LiveRepository) ResolveSOS(ctx context.Context, id uuid.UUID, by *uuid.UUID, at time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).ResolveSOSAlert(ctx, gen.ResolveSOSAlertParams{ID: id, ResolvedBy: by, ResolvedAt: &at})
	if err != nil {
		return false, fmt.Errorf("resolve sos: %w", err)
	}
	return rows > 0, nil
}
