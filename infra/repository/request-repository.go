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

type RequestRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRequestRepository = (*RequestRepository)(nil)

func NewRequestRepository(pool *pgxpool.Pool) *RequestRepository {
	return &RequestRepository{pool: pool}
}

func (r *RequestRepository) Create(ctx context.Context, s *entities.ServiceRequest) error {
	return postgres.NewTxManager(r.pool).WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		if err := saveAddressOfPlace(ctx, queries, &s.Pickup); err != nil {
			return err
		}
		params := gen.InsertServiceRequestParams{
			ID: s.ID, CustomerID: s.CustomerID, CategoryID: s.CategoryID, CategorySlug: s.CategorySlug, ServiceID: s.ServiceID, ZoneID: s.ZoneID,
			PolicyID: s.PolicyID, Status: string(s.Status), Version: int32(s.Version), PickupAddressID: s.Pickup.AddressID, ContactPhone: s.ContactPhone, PickupH3R8: s.PickupH3R8, // #nosec G115 -- small counter
			DestinationH3R8: s.DestinationH3, PassengerName: s.PassengerName, PassengerPhone: s.PassengerPhone, Details: s.Details,
			ScheduledFor: s.ScheduledFor, SearchRound: int16(s.SearchRound), SearchExtended: s.SearchExtended, SearchDeadlineAt: s.SearchDeadline, // #nosec G115 -- small counter
			NextActionAt: s.NextActionAt, ReminderAt: s.ReminderAt, FareEstimateID: s.Fare.EstimateID, FareCents: s.Fare.Cents, CommissionCents: s.Fare.CommissionCents,
			BoostBps: int32(s.Fare.BoostBps), Currency: s.Fare.Currency, IdempotencyKey: s.IdempotencyKey, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, // #nosec G115 -- 0 to 2000
		}
		if s.Destination != nil {
			if err := saveAddressOfPlace(ctx, queries, s.Destination); err != nil {
				return err
			}
			params.DestinationAddressID = &s.Destination.AddressID
		}
		err := queries.InsertServiceRequest(ctx, params)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return repository.ErrDuplicateIdempotency
		}
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		return saveStops(ctx, queries, s)
	})
}

func saveStops(ctx context.Context, queries *gen.Queries, s *entities.ServiceRequest) error {
	if err := queries.DeleteServiceRequestStops(ctx, s.ID); err != nil {
		return fmt.Errorf("clear stops: %w", err)
	}
	for i := range s.Stops {
		if err := saveAddressOfPlace(ctx, queries, &s.Stops[i]); err != nil {
			return err
		}
		err := queries.InsertServiceRequestStop(ctx, gen.InsertServiceRequestStopParams{RequestID: s.ID, Position: int32(i), AddressID: s.Stops[i].AddressID}) // #nosec G115 -- at most 3 stops
		if err != nil {
			return fmt.Errorf("save stop: %w", err)
		}
	}
	return nil
}

func (r *RequestRepository) Find(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetServiceRequest(ctx, id)
	return requestOrNil(ctx, queries, &row, err, "get request")
}

func (r *RequestRepository) FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetServiceRequestForUpdate(ctx, id)
	return requestOrNil(ctx, queries, &row, err, "lock request")
}

func (r *RequestRepository) TryLock(ctx context.Context, id uuid.UUID) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.TryLockServiceRequest(ctx, id)
	return requestOrNil(ctx, queries, &row, err, "try lock request")
}

func (r *RequestRepository) FindByIdempotencyKey(ctx context.Context, customerID uuid.UUID, key string) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.FindServiceRequestByIdempotencyKey(ctx, gen.FindServiceRequestByIdempotencyKeyParams{CustomerID: customerID, IdempotencyKey: key})
	return requestOrNil(ctx, queries, &row, err, "find by idempotency key")
}

func (r *RequestRepository) Update(ctx context.Context, s *entities.ServiceRequest, expectedVersion int) error {
	queries := postgres.Queries(ctx, r.pool)
	params := gen.UpdateServiceRequestParams{
		ID: s.ID, ExpectedVersion: int32(expectedVersion), Status: string(s.Status), Version: int32(s.Version), DestinationH3R8: s.DestinationH3, // #nosec G115 -- small counter
		SearchRound: int16(s.SearchRound), SearchExtended: s.SearchExtended, SearchDeadlineAt: s.SearchDeadline, NextActionAt: s.NextActionAt, ReminderAt: s.ReminderAt, // #nosec G115 -- small counter
		ProviderID: s.ProviderID, FareCents: s.Fare.Cents, CommissionCents: s.Fare.CommissionCents, BoostBps: int32(s.Fare.BoostBps), NeedsOperator: s.NeedsOperator, // #nosec G115 -- 0 to 2000
		OperatorReason: s.OperatorReason, CancelReason: s.CancelReason, CancelledBy: s.CancelledBy, AcceptedAt: s.AcceptedAt, ArrivedAt: s.ArrivedAt,
		StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.Destination != nil {
		params.DestinationAddressID = &s.Destination.AddressID
		if s.RouteChanged {
			if err := saveAddressOfPlace(ctx, queries, s.Destination); err != nil {
				return err
			}
		}
	}
	rows, err := queries.UpdateServiceRequest(ctx, params)
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	if rows == 0 {
		return repository.ErrRequestVersionConflict
	}
	if s.RouteChanged {
		s.RouteChanged = false
		return saveStops(ctx, queries, s)
	}
	return nil
}

func (r *RequestRepository) RecordChange(ctx context.Context, requestID uuid.UUID, c *entities.RequestStatusChange) error {
	err := postgres.Queries(ctx, r.pool).InsertServiceRequestEvent(ctx, gen.InsertServiceRequestEventParams{
		RequestID: requestID, FromStatus: string(c.From), ToStatus: string(c.To), Version: int32(c.Version), ActorType: string(c.Actor.Type), // #nosec G115 -- small counter
		ActorID: c.Actor.ID, Reason: c.Reason, OccurredAt: c.At,
	})
	if err != nil {
		return fmt.Errorf("record request change: %w", err)
	}
	return nil
}

func (r *RequestRepository) DueRequests(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).DueServiceRequests(ctx, gen.DueServiceRequestsParams{Now: now, Batch: int32(batch)}) // #nosec G115 -- small batch
	if err != nil {
		return nil, fmt.Errorf("due requests: %w", err)
	}
	return ids, nil
}

func (r *RequestRepository) DueReminders(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).DueServiceRequestReminders(ctx, gen.DueServiceRequestRemindersParams{Now: now, Batch: int32(batch)}) // #nosec G115 -- small batch
	if err != nil {
		return nil, fmt.Errorf("due reminders: %w", err)
	}
	return ids, nil
}

func (r *RequestRepository) ActiveOfCustomer(ctx context.Context, customerID uuid.UUID) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.ActiveRequestOfCustomer(ctx, customerID)
	return requestOrNil(ctx, queries, &row, err, "active request of customer")
}

func (r *RequestRepository) ActiveOfProvider(ctx context.Context, providerID uuid.UUID) (*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.ActiveRequestOfProvider(ctx, &providerID)
	return requestOrNil(ctx, queries, &row, err, "active request of provider")
}

func (r *RequestRepository) ListOfCustomer(ctx context.Context, f repository.RequestFilter) ([]*entities.ServiceRequest, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	statuses := f.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := queries.ListRequestsOfCustomer(ctx, gen.ListRequestsOfCustomerParams{
		CustomerID: f.CustomerID, Statuses: statuses, CategorySlug: f.Category, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list requests: %w", err)
	}
	total, err := queries.CountRequestsOfCustomer(ctx, gen.CountRequestsOfCustomerParams{CustomerID: f.CustomerID, Statuses: statuses, CategorySlug: f.Category})
	if err != nil {
		return nil, 0, fmt.Errorf("count requests: %w", err)
	}
	requests, err := toRequests(ctx, queries, rows)
	return requests, total, err
}

func (r *RequestRepository) OperatorQueue(ctx context.Context, limit, offset int) ([]*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListOperatorQueue(ctx, gen.ListOperatorQueueParams{PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, fmt.Errorf("operator queue: %w", err)
	}
	return toRequests(ctx, queries, rows)
}

func (r *RequestRepository) AddFareAdjustment(ctx context.Context, a entities.FareAdjustment) error {
	err := postgres.Queries(ctx, r.pool).InsertFareAdjustment(ctx, gen.InsertFareAdjustmentParams{
		RequestID: a.RequestID, Reason: string(a.Reason), AmountCents: a.AmountCents, ActorID: a.ActorID, CreatedAt: a.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("add fare adjustment: %w", err)
	}
	return nil
}

func (r *RequestRepository) ListFareAdjustments(ctx context.Context, requestID uuid.UUID) ([]entities.FareAdjustment, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListFareAdjustments(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list fare adjustments: %w", err)
	}
	out := make([]entities.FareAdjustment, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.FareAdjustment{RequestID: requestID, Reason: entities.FareAdjustmentReason(row.Reason), AmountCents: row.AmountCents, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (r *RequestRepository) DemandByCell(ctx context.Context, since time.Time, zoneID *uuid.UUID) (map[int64]int64, error) {
	rows, err := postgres.Queries(ctx, r.pool).DemandByCell(ctx, gen.DemandByCellParams{Since: since, ZoneID: zoneID})
	if err != nil {
		return nil, fmt.Errorf("demand by cell: %w", err)
	}
	demand := make(map[int64]int64, len(rows))
	for _, row := range rows {
		demand[row.PickupH3R8] = row.Requests
	}
	return demand, nil
}

func (r *RequestRepository) ListWithProviderOnTheWay(ctx context.Context, limit int) ([]*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListRequestsWithProviderOnTheWay(ctx, int32(limit)) // #nosec G115 -- small batch
	if err != nil {
		return nil, fmt.Errorf("list requests on the way: %w", err)
	}
	return toRequests(ctx, queries, rows)
}

func requestOrNil(ctx context.Context, queries *gen.Queries, row *gen.ServiceRequest, err error, action string) (*entities.ServiceRequest, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	requests, err := toRequests(ctx, queries, []gen.ServiceRequest{*row})
	if err != nil {
		return nil, err
	}
	return requests[0], nil
}

func toRequests(ctx context.Context, queries *gen.Queries, rows []gen.ServiceRequest) ([]*entities.ServiceRequest, error) {
	out := make([]*entities.ServiceRequest, 0, len(rows))
	byID := make(map[uuid.UUID]*entities.ServiceRequest, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		request := toRequest(&rows[i])
		out = append(out, request)
		byID[request.ID] = request
		ids = append(ids, request.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	places, err := queries.ListRequestPlaces(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list request places: %w", err)
	}
	for i := range places {
		row := &places[i]
		place := entities.RequestPlace{AddressID: row.ID, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Reference: row.Reference, Address: row.Line}
		request := byID[row.RequestID]
		switch row.Role {
		case "pickup":
			request.Pickup = place
		case "destination":
			request.Destination = &place
		default:
			request.Stops = append(request.Stops, place)
		}
	}
	return out, nil
}

func toRequest(row *gen.ServiceRequest) *entities.ServiceRequest {
	return &entities.ServiceRequest{
		ID: row.ID, CustomerID: row.CustomerID, CategoryID: row.CategoryID, CategorySlug: row.CategorySlug, ServiceID: row.ServiceID, ZoneID: row.ZoneID, PolicyID: row.PolicyID,
		Status: entities.RequestStatus(row.Status), Version: int(row.Version), Stops: []entities.RequestPlace{},
		PickupH3R8: row.PickupH3R8, DestinationH3: row.DestinationH3R8, ContactPhone: row.ContactPhone, PassengerName: row.PassengerName, PassengerPhone: row.PassengerPhone,
		Details: row.Details, ScheduledFor: row.ScheduledFor, SearchRound: int(row.SearchRound), SearchExtended: row.SearchExtended, SearchDeadline: row.SearchDeadlineAt,
		NextActionAt: row.NextActionAt, ReminderAt: row.ReminderAt, ProviderID: row.ProviderID,
		Fare:          entities.RequestFare{EstimateID: row.FareEstimateID, Cents: row.FareCents, CommissionCents: row.CommissionCents, BoostBps: int(row.BoostBps), Currency: row.Currency},
		NeedsOperator: row.NeedsOperator, OperatorReason: row.OperatorReason, IdempotencyKey: row.IdempotencyKey, CancelReason: row.CancelReason, CancelledBy: row.CancelledBy,
		AcceptedAt: row.AcceptedAt, ArrivedAt: row.ArrivedAt, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

type OfferRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IOfferRepository = (*OfferRepository)(nil)

func NewOfferRepository(pool *pgxpool.Pool) *OfferRepository {
	return &OfferRepository{pool: pool}
}

func (r *OfferRepository) Create(ctx context.Context, o *entities.RequestOffer) error {
	var distance *int32
	if o.DistanceM != nil {
		value := int32(*o.DistanceM) // #nosec G115 -- a distance fits in int32
		distance = &value
	}
	rows, err := postgres.Queries(ctx, r.pool).UpsertRequestOffer(ctx, gen.UpsertRequestOfferParams{
		ID: o.ID, RequestID: o.RequestID, ProviderID: o.ProviderID, SentAt: o.SentAt, ExpiresAt: o.ExpiresAt, NextActionAt: o.NextActionAt,
		DistanceM: distance, NetCents: o.NetCents, BoostBps: int32(o.BoostBps), // #nosec G115 -- 0 to 2000
	})
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}
	if rows == 0 {
		return repository.ErrOfferAlreadySent
	}
	return nil
}

func (r *OfferRepository) Find(ctx context.Context, id uuid.UUID) (*entities.RequestOffer, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRequestOffer(ctx, id)
	return offerOrNil(&row, err, "get offer")
}

func (r *OfferRepository) FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.RequestOffer, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRequestOfferForUpdate(ctx, id)
	return offerOrNil(&row, err, "lock offer")
}

func (r *OfferRepository) Update(ctx context.Context, o *entities.RequestOffer) error {
	var via *string
	if o.DeliveredVia != "" {
		via = &o.DeliveredVia
	}
	err := postgres.Queries(ctx, r.pool).UpdateRequestOffer(ctx, gen.UpdateRequestOfferParams{
		ID: o.ID, Status: string(o.Status), DeliveredAt: o.DeliveredAt, DeliveredVia: via, ExpiresAt: o.ExpiresAt, RespondedAt: o.RespondedAt,
		NextActionAt: o.NextActionAt, FallbackStep: int16(o.FallbackStep), QuoteCents: o.QuoteCents, QuoteNote: o.QuoteNote, // #nosec G115 -- small counter
	})
	if err != nil {
		return fmt.Errorf("update offer: %w", err)
	}
	return nil
}

func (r *OfferRepository) ListOfRequest(ctx context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRequestOffers(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list offers: %w", err)
	}
	return toOffers(rows), nil
}

func (r *OfferRepository) ListOfRequestForUpdate(ctx context.Context, requestID uuid.UUID) ([]*entities.RequestOffer, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRequestOffersForUpdate(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("lock offers: %w", err)
	}
	return toOffers(rows), nil
}

func (r *OfferRepository) Due(ctx context.Context, now time.Time, batch int) ([]uuid.UUID, error) {
	ids, err := postgres.Queries(ctx, r.pool).DueRequestOffers(ctx, gen.DueRequestOffersParams{Now: now, Batch: int32(batch)}) // #nosec G115 -- small batch
	if err != nil {
		return nil, fmt.Errorf("due offers: %w", err)
	}
	return ids, nil
}

func (r *OfferRepository) OpenOfProvider(ctx context.Context, providerID uuid.UUID, now time.Time) ([]*entities.RequestOffer, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListOpenOffersOfProvider(ctx, gen.ListOpenOffersOfProviderParams{ProviderID: providerID, Now: now})
	if err != nil {
		return nil, fmt.Errorf("open offers: %w", err)
	}
	return toOffers(rows), nil
}

func offerOrNil(row *gen.RequestOffer, err error, action string) (*entities.RequestOffer, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return toOffer(row), nil
}

func toOffers(rows []gen.RequestOffer) []*entities.RequestOffer {
	out := make([]*entities.RequestOffer, 0, len(rows))
	for i := range rows {
		out = append(out, toOffer(&rows[i]))
	}
	return out
}

func toOffer(row *gen.RequestOffer) *entities.RequestOffer {
	offer := &entities.RequestOffer{
		ID: row.ID, RequestID: row.RequestID, ProviderID: row.ProviderID, Status: entities.OfferStatus(row.Status), SentAt: row.SentAt, DeliveredAt: row.DeliveredAt,
		ExpiresAt: row.ExpiresAt, RespondedAt: row.RespondedAt, NextActionAt: row.NextActionAt, FallbackStep: int(row.FallbackStep), QuoteCents: row.QuoteCents,
		QuoteNote: row.QuoteNote, NetCents: row.NetCents, BoostBps: int(row.BoostBps),
	}
	if row.DeliveredVia != nil {
		offer.DeliveredVia = *row.DeliveredVia
	}
	if row.DistanceM != nil {
		distance := int(*row.DistanceM)
		offer.DistanceM = &distance
	}
	return offer
}
