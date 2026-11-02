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

type SupportRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ISupportRepository = (*SupportRepository)(nil)

func NewSupportRepository(pool *pgxpool.Pool) *SupportRepository {
	return &SupportRepository{pool: pool}
}

func toTicket(row *gen.SupportTicket) entities.SupportTicket {
	return entities.SupportTicket{ID: row.ID, Code: row.Code, UserID: row.UserID, RequestID: row.RequestID, TopicID: row.TopicID, Subject: row.Subject, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (r *SupportRepository) NextCode(ctx context.Context) (string, error) {
	code, err := postgres.Queries(ctx, r.pool).NextTicketCode(ctx)
	if err != nil {
		return "", fmt.Errorf("next ticket code: %w", err)
	}
	return code, nil
}

func (r *SupportRepository) Create(ctx context.Context, t *entities.SupportTicket) error {
	err := postgres.Queries(ctx, r.pool).InsertTicket(ctx, gen.InsertTicketParams{ID: t.ID, Code: t.Code, UserID: t.UserID, RequestID: t.RequestID, TopicID: t.TopicID, Subject: t.Subject, Status: t.Status, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt})
	if err != nil {
		return fmt.Errorf("create ticket: %w", err)
	}
	return nil
}

func (r *SupportRepository) Find(ctx context.Context, id uuid.UUID) (*entities.SupportTicket, error) {
	row, err := postgres.Queries(ctx, r.pool).GetTicket(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ticket: %w", err)
	}
	ticket := toTicket(&row)
	return &ticket, nil
}

func (r *SupportRepository) OfUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]entities.SupportTicket, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListTicketsOfUser(ctx, gen.ListTicketsOfUserParams{UserID: userID, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list tickets: %w", err)
	}
	total, err := queries.CountTicketsOfUser(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count tickets: %w", err)
	}
	out := make([]entities.SupportTicket, 0, len(rows))
	for i := range rows {
		out = append(out, toTicket(&rows[i]))
	}
	return out, total, nil
}

func (r *SupportRepository) List(ctx context.Context, status string, limit, offset int) ([]entities.SupportTicket, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListTickets(ctx, gen.ListTicketsParams{Status: status, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list tickets: %w", err)
	}
	total, err := queries.CountTickets(ctx, status)
	if err != nil {
		return nil, 0, fmt.Errorf("count tickets: %w", err)
	}
	out := make([]entities.SupportTicket, 0, len(rows))
	for i := range rows {
		out = append(out, toTicket(&rows[i]))
	}
	return out, total, nil
}

func (r *SupportRepository) SetStatus(ctx context.Context, id uuid.UUID, status string) error {
	if _, err := postgres.Queries(ctx, r.pool).SetTicketStatus(ctx, gen.SetTicketStatusParams{ID: id, Status: status}); err != nil {
		return fmt.Errorf("set ticket status: %w", err)
	}
	return nil
}

func (r *SupportRepository) AddMessage(ctx context.Context, m *entities.SupportMessage) error {
	err := postgres.Queries(ctx, r.pool).InsertTicketMessage(ctx, gen.InsertTicketMessageParams{TicketID: m.TicketID, SenderType: m.SenderType, SenderID: m.SenderID, Body: m.Body, CreatedAt: m.CreatedAt})
	if err != nil {
		return fmt.Errorf("add ticket message: %w", err)
	}
	return nil
}

func (r *SupportRepository) Messages(ctx context.Context, ticketID uuid.UUID) ([]entities.SupportMessage, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListTicketMessages(ctx, ticketID)
	if err != nil {
		return nil, fmt.Errorf("list ticket messages: %w", err)
	}
	out := make([]entities.SupportMessage, 0, len(rows))
	for i := range rows {
		out = append(out, entities.SupportMessage{TicketID: rows[i].TicketID, SenderType: rows[i].SenderType, SenderID: rows[i].SenderID, Body: rows[i].Body, CreatedAt: rows[i].CreatedAt})
	}
	return out, nil
}
