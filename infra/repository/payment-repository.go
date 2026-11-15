package repository

import (
	"context"
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
)

const pendingPaymentIndex = "payments_one_pending_key"

type PaymentRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IPaymentRepository = (*PaymentRepository)(nil)

func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

func toPayment(row *gen.Payment) *entities.Payment {
	return &entities.Payment{
		ID: row.ID, RequestID: row.RequestID, CustomerID: row.CustomerID, Kind: entities.PaymentKind(row.Kind), GatewayFeeCents: row.GatewayFeeCents, Method: entities.PaymentMethod(row.Method), Status: entities.PaymentStatus(row.Status),
		AmountCents: row.AmountCents, RefundedCents: row.RefundedCents, Currency: row.Currency, MerchantID: row.MerchantID, ProviderChargeID: row.ProviderChargeID,
		PayerPhoneMasked: row.PayerPhoneMasked, ReferenceNumber: row.ReferenceNumber, ReferenceEntity: row.ReferenceEntity, FailureMessage: row.FailureMessage,
		ExpiresAt: row.ExpiresAt, NextCheckAt: row.NextCheckAt, PaidAt: row.PaidAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Duplicate: row.Duplicate,
	}
}

func paymentOrNil(row *gen.Payment, err error, action string) (*entities.Payment, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return toPayment(row), nil
}

func (r *PaymentRepository) ListMethods(ctx context.Context, category string, onlyEnabled bool) ([]repository.CategoryPaymentMethod, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListCategoryPaymentMethods(ctx, gen.ListCategoryPaymentMethodsParams{Category: category, OnlyEnabled: onlyEnabled})
	if err != nil {
		return nil, fmt.Errorf("list payment methods: %w", err)
	}
	out := make([]repository.CategoryPaymentMethod, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.CategoryPaymentMethod{CategoryID: row.CategoryID, CategorySlug: row.CategorySlug, CategoryName: row.CategoryName, Method: entities.PaymentMethod(row.Method), IsEnabled: row.IsEnabled})
	}
	return out, nil
}

func (r *PaymentRepository) IsMethodEnabled(ctx context.Context, categoryID uuid.UUID, method entities.PaymentMethod) (bool, error) {
	enabled, err := postgres.Queries(ctx, r.pool).IsPaymentMethodEnabled(ctx, gen.IsPaymentMethodEnabledParams{CategoryID: categoryID, Method: string(method)})
	if err != nil {
		return false, fmt.Errorf("check payment method: %w", err)
	}
	return enabled, nil
}

func (r *PaymentRepository) SetMethod(ctx context.Context, categoryID uuid.UUID, method entities.PaymentMethod, enabled bool) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).SetCategoryPaymentMethod(ctx, gen.SetCategoryPaymentMethodParams{CategoryID: categoryID, Method: string(method), IsEnabled: enabled})
	if err != nil {
		return false, fmt.Errorf("set payment method: %w", err)
	}
	return rows > 0, nil
}

func (r *PaymentRepository) FindOutstanding(ctx context.Context, customerID uuid.UUID) (*entities.OutstandingPaymentError, error) {
	row, err := postgres.Queries(ctx, r.pool).FindOutstandingPayment(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find outstanding payment: %w", err)
	}
	return entities.NewOutstandingPayment(row.ID, entities.RequestStatus(row.Status)), nil
}

func (r *PaymentRepository) SetRequestPayment(ctx context.Context, requestID uuid.UUID, method entities.PaymentMethod, status entities.RequestPaymentStatus) error {
	err := postgres.Queries(ctx, r.pool).SetRequestPayment(ctx, gen.SetRequestPaymentParams{ID: requestID, PaymentMethod: string(method), PaymentStatus: string(status)})
	if err != nil {
		return fmt.Errorf("set request payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Create(ctx context.Context, p *entities.Payment) error {
	err := postgres.Queries(ctx, r.pool).InsertPayment(ctx, gen.InsertPaymentParams{
		ID: p.ID, RequestID: p.RequestID, CustomerID: p.CustomerID, Kind: string(p.Kind), Method: string(p.Method), Status: string(p.Status), AmountCents: p.AmountCents, RefundedCents: p.RefundedCents,
		Currency: p.Currency, MerchantID: p.MerchantID, ProviderChargeID: p.ProviderChargeID, PayerPhoneMasked: p.PayerPhoneMasked, ReferenceNumber: p.ReferenceNumber,
		ReferenceEntity: p.ReferenceEntity, FailureMessage: p.FailureMessage, ExpiresAt: p.ExpiresAt, NextCheckAt: p.NextCheckAt, PaidAt: p.PaidAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == pendingPaymentIndex {
		return repository.ErrPaymentAlreadyPending
	}
	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Save(ctx context.Context, p *entities.Payment) error {
	err := postgres.Queries(ctx, r.pool).UpdatePayment(ctx, gen.UpdatePaymentParams{
		ID: p.ID, Status: string(p.Status), RefundedCents: p.RefundedCents, ProviderChargeID: p.ProviderChargeID, ReferenceNumber: p.ReferenceNumber, ReferenceEntity: p.ReferenceEntity,
		FailureMessage: p.FailureMessage, ExpiresAt: p.ExpiresAt, NextCheckAt: p.NextCheckAt, PaidAt: p.PaidAt, UpdatedAt: p.UpdatedAt, Duplicate: p.Duplicate,
	})
	if err != nil {
		return fmt.Errorf("save payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Find(ctx context.Context, id uuid.UUID) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPayment(ctx, id)
	return paymentOrNil(&row, err, "get payment")
}

func (r *PaymentRepository) FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPaymentForUpdate(ctx, id)
	return paymentOrNil(&row, err, "lock payment")
}

func (r *PaymentRepository) FindByProviderCharge(ctx context.Context, chargeID string) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPaymentByProviderCharge(ctx, &chargeID)
	return paymentOrNil(&row, err, "get payment by charge")
}

func (r *PaymentRepository) FindByMerchantID(ctx context.Context, merchantID string) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPaymentByMerchantID(ctx, merchantID)
	return paymentOrNil(&row, err, "get payment by merchant id")
}

func (r *PaymentRepository) PendingOfRequest(ctx context.Context, requestID uuid.UUID, kind entities.PaymentKind) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPendingPaymentOfRequest(ctx, gen.GetPendingPaymentOfRequestParams{RequestID: requestID, Kind: string(kind)})
	return paymentOrNil(&row, err, "get pending payment")
}

func (r *PaymentRepository) PaidOfRequest(ctx context.Context, requestID uuid.UUID, kind entities.PaymentKind) (*entities.Payment, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPaidPaymentOfRequest(ctx, gen.GetPaidPaymentOfRequestParams{RequestID: requestID, Kind: string(kind)})
	return paymentOrNil(&row, err, "get paid payment")
}

func (r *PaymentRepository) ListOfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.Payment, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListPaymentsOfRequest(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list payments of request: %w", err)
	}
	out := make([]entities.Payment, 0, len(rows))
	for i := range rows {
		out = append(out, *toPayment(&rows[i]))
	}
	return out, nil
}

func (r *PaymentRepository) Due(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).DuePayments(ctx, gen.DuePaymentsParams{NextCheckAt: &now, Limit: int32(limit)}) // #nosec G115 -- small configured value
	if err != nil {
		return nil, fmt.Errorf("list due payments: %w", err)
	}
	return ids, nil
}

func (r *PaymentRepository) AdminList(ctx context.Context, f repository.PaymentFilter) ([]repository.AdminPayment, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	statuses := emptyStatuses(f.Statuses)
	rows, err := queries.ListAdminPayments(ctx, gen.ListAdminPaymentsParams{
		Statuses: statuses, Method: f.Method, Category: f.Category, RequestID: f.RequestID, CreatedFrom: f.From, CreatedTo: f.To,
		PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list payments: %w", err)
	}
	total, err := queries.CountAdminPayments(ctx, gen.CountAdminPaymentsParams{Statuses: statuses, Method: f.Method, Category: f.Category, RequestID: f.RequestID, CreatedFrom: f.From, CreatedTo: f.To})
	if err != nil {
		return nil, 0, fmt.Errorf("count payments: %w", err)
	}
	out := make([]repository.AdminPayment, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.AdminPayment{Category: row.CategorySlug, CustomerName: row.CustomerName, Payment: *toPayment(&gen.Payment{
			ID: row.ID, RequestID: row.RequestID, CustomerID: row.CustomerID, Method: row.Method, Status: row.Status, AmountCents: row.AmountCents, RefundedCents: row.RefundedCents,
			Currency: row.Currency, MerchantID: row.MerchantID, ProviderChargeID: row.ProviderChargeID, PayerPhoneMasked: row.PayerPhoneMasked, ReferenceNumber: row.ReferenceNumber,
			ReferenceEntity: row.ReferenceEntity, FailureMessage: row.FailureMessage, ExpiresAt: row.ExpiresAt, NextCheckAt: row.NextCheckAt, PaidAt: row.PaidAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})})
	}
	return out, total, nil
}

func (r *PaymentRepository) AddEvent(ctx context.Context, paymentID uuid.UUID, e repository.PaymentEvent) error {
	err := postgres.Queries(ctx, r.pool).InsertPaymentEvent(ctx, gen.InsertPaymentEventParams{PaymentID: paymentID, Source: e.Source, Status: e.Status, Detail: e.Detail, OccurredAt: e.OccurredAt})
	if err != nil {
		return fmt.Errorf("add payment event: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Events(ctx context.Context, paymentID uuid.UUID) ([]repository.PaymentEvent, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListPaymentEvents(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("list payment events: %w", err)
	}
	out := make([]repository.PaymentEvent, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.PaymentEvent{Source: row.Source, Status: row.Status, Detail: row.Detail, OccurredAt: row.OccurredAt})
	}
	return out, nil
}

func (r *PaymentRepository) AddRefund(ctx context.Context, refund *repository.PaymentRefund) error {
	err := postgres.Queries(ctx, r.pool).InsertPaymentRefund(ctx, gen.InsertPaymentRefundParams{
		ID: refund.ID, PaymentID: refund.PaymentID, AmountCents: refund.AmountCents, Reason: refund.Reason, ProviderRefundID: refund.ProviderRefundID, ActorID: refund.ActorID, CreatedAt: refund.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("add payment refund: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Refunds(ctx context.Context, paymentID uuid.UUID) ([]repository.PaymentRefund, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListPaymentRefunds(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("list payment refunds: %w", err)
	}
	out := make([]repository.PaymentRefund, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.PaymentRefund{ID: row.ID, PaymentID: row.PaymentID, AmountCents: row.AmountCents, Reason: row.Reason, ProviderRefundID: row.ProviderRefundID, ActorID: row.ActorID, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (r *PaymentRepository) UnpaidPrepaid(ctx context.Context, before time.Time, limit int) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).UnpaidPrepaidRequests(ctx, gen.UnpaidPrepaidRequestsParams{Before: before, Batch: int32(limit)}) // #nosec G115 -- small batch from the configuration
	if err != nil {
		return nil, fmt.Errorf("unpaid prepaid requests: %w", err)
	}
	return ids, nil
}
