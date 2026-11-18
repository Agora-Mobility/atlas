package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

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
			BoostBps: int32(s.Fare.BoostBps), Currency: s.Fare.Currency, IdempotencyKey: s.IdempotencyKey, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, PaymentMethod: string(s.PaymentMethod), PublicCode: s.PublicCode, VehicleTypeID: s.VehicleTypeID, CompletionCode: s.CompletionCode, WorkshopID: s.WorkshopID, Prepay: s.Prepay, RemovalDay: s.RemovalDay, RemovalWindowID: s.RemovalWindowID, RouteGeometry: s.Fare.RouteGeometry, PickupH3R9: heatCellOf(s), // #nosec G115 -- 0 to 2000
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
		ProviderID: s.ProviderID, FareCents: s.Fare.Cents, CommissionCents: s.Fare.CommissionCents, RouteGeometry: s.Fare.RouteGeometry, BoostBps: int32(s.Fare.BoostBps), NeedsOperator: s.NeedsOperator, // #nosec G115 -- 0 to 2000
		OperatorReason: s.OperatorReason, CancelReason: s.CancelReason, CancelledBy: s.CancelledBy, CancelReasonID: s.CancelReasonID, CancelFeeCents: s.CancelFeeCents, VoucherID: s.VoucherID, VoucherCode: s.VoucherCode, DiscountCents: s.DiscountCents, CompletionAttempts: int16(s.CompletionTries), ExtraDueCents: s.ExtraDueCents, // #nosec G115 -- small counter
		AcceptedAt: s.AcceptedAt, ArrivedAt: s.ArrivedAt,
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

func (r *RequestRepository) ListOfProvider(ctx context.Context, f repository.RequestFilter) ([]*entities.ServiceRequest, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	statuses := f.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	providerID := f.ProviderID
	rows, err := queries.ListRequestsOfProvider(ctx, gen.ListRequestsOfProviderParams{
		ProviderID: &providerID, Statuses: statuses, CategorySlug: f.Category, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list requests of provider: %w", err)
	}
	total, err := queries.CountRequestsOfProvider(ctx, gen.CountRequestsOfProviderParams{ProviderID: &providerID, Statuses: statuses, CategorySlug: f.Category})
	if err != nil {
		return nil, 0, fmt.Errorf("count requests of provider: %w", err)
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

func (r *RequestRepository) DemandByCell(ctx context.Context, filter repository.HeatFilter) (map[int64]int64, error) {
	rows, err := postgres.Queries(ctx, r.pool).DemandByCell(ctx, gen.DemandByCellParams{FromAt: filter.From, ToAt: filter.To, ZoneID: filter.ZoneID, Category: filter.Category})
	if err != nil {
		return nil, fmt.Errorf("demand by cell: %w", err)
	}
	demand := make(map[int64]int64, len(rows))
	for _, row := range rows {
		demand[row.PickupH3R8] = row.Requests
	}
	return demand, nil
}

func (r *RequestRepository) FlowsByCell(ctx context.Context, filter repository.HeatFilter) ([]repository.CellFlow, error) {
	rows, err := postgres.Queries(ctx, r.pool).FlowsByCell(ctx, gen.FlowsByCellParams{FromAt: filter.From, ToAt: filter.To, ZoneID: filter.ZoneID, Category: filter.Category})
	if err != nil {
		return nil, fmt.Errorf("flows by cell: %w", err)
	}
	flows := make([]repository.CellFlow, 0, len(rows))
	for _, row := range rows {
		flows = append(flows, repository.CellFlow{Origin: row.PickupH3R8, Destination: row.DestinationH3R8, Requests: row.Requests})
	}
	return flows, nil
}

func (r *RequestRepository) ListWithProviderOnTheWay(ctx context.Context, limit int) ([]*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListRequestsWithProviderOnTheWay(ctx, int32(limit)) // #nosec G115 -- small batch
	if err != nil {
		return nil, fmt.Errorf("list requests on the way: %w", err)
	}
	return toRequests(ctx, queries, rows)
}

func emptyStatuses(statuses []string) []string {
	if statuses == nil {
		return []string{}
	}
	return statuses
}

func (r *RequestRepository) AdminList(ctx context.Context, f repository.AdminRequestFilter) ([]repository.AdminRequest, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	statuses := emptyStatuses(f.Statuses)
	search := newRequestSearch(f.Search)
	rows, err := queries.ListAdminRequests(ctx, gen.ListAdminRequestsParams{
		Statuses: statuses, Category: f.Category, ServiceID: f.ServiceID, ZoneID: f.ZoneID, OrganizationID: f.OrganizationID, ProviderID: f.ProviderID, CustomerID: f.CustomerID,
		CreatedFrom: f.From, CreatedTo: f.To, RouteReview: f.RouteReview, Search: search.text, SearchDigits: search.digits, SearchPlate: search.plate, Sort: f.Sort, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list admin requests: %w", err)
	}
	total, err := queries.CountAdminRequests(ctx, gen.CountAdminRequestsParams{
		Statuses: statuses, Category: f.Category, ServiceID: f.ServiceID, ZoneID: f.ZoneID, OrganizationID: f.OrganizationID, ProviderID: f.ProviderID, CustomerID: f.CustomerID,
		CreatedFrom: f.From, CreatedTo: f.To, RouteReview: f.RouteReview, Search: search.text, SearchDigits: search.digits, SearchPlate: search.plate,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count admin requests: %w", err)
	}
	out := make([]repository.AdminRequest, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		item := repository.AdminRequest{
			ID: row.ID, Code: row.PublicCode, Status: row.Status, Version: int(row.Version), Category: row.CategorySlug, CategoryName: row.CategoryName, ServiceID: row.ServiceID, ServiceName: row.ServiceName,
			CustomerID: row.CustomerID, CustomerName: row.CustomerName, ProviderID: row.ProviderID, OrganizationID: row.OrganizationID, ZoneID: row.ZoneID, ZoneName: row.ZoneName,
			FareCents: row.FareCents, Currency: row.Currency, NeedsOperator: row.NeedsOperator, ScheduledFor: row.ScheduledFor, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt,
			PickupLine: row.PickupLine, PickupLat: row.PickupLat, PickupLng: row.PickupLng, RouteReview: row.RouteReview,
		}
		if row.DistanceDeviationBps != nil {
			deviation := int(*row.DistanceDeviationBps)
			item.DeviationBps = &deviation
		}
		if row.ProviderName != nil {
			item.ProviderName = *row.ProviderName
		}
		if row.OrganizationName != nil {
			item.OrganizationName = *row.OrganizationName
		}
		if row.DestinationLine != nil {
			item.DestinationLine = *row.DestinationLine
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *RequestRepository) Summary(ctx context.Context, f repository.AdminRequestFilter) ([]repository.RequestSummaryRow, error) {
	rows, err := postgres.Queries(ctx, r.pool).SummarizeRequests(ctx, gen.SummarizeRequestsParams{Category: f.Category, ZoneID: f.ZoneID, OrganizationID: f.OrganizationID, CreatedFrom: f.From, CreatedTo: f.To})
	if err != nil {
		return nil, fmt.Errorf("summarize requests: %w", err)
	}
	out := make([]repository.RequestSummaryRow, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		item := repository.RequestSummaryRow{
			Category: row.CategorySlug, CategoryName: row.CategoryName, ServiceID: row.ServiceID, ServiceName: row.ServiceName, Status: row.Status, ZoneID: row.ZoneID, ZoneName: row.ZoneName,
			OrganizationID: row.OrganizationID, Currency: row.Currency, Total: row.Total, FareCents: row.FareCents,
		}
		if row.OrganizationName != nil {
			item.OrganizationName = *row.OrganizationName
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *RequestRepository) History(ctx context.Context, requestID uuid.UUID) ([]repository.RequestHistoryEntry, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListServiceRequestEvents(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list request events: %w", err)
	}
	out := make([]repository.RequestHistoryEntry, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.RequestHistoryEntry{From: row.FromStatus, To: row.ToStatus, Version: int(row.Version), ActorType: row.ActorType, ActorID: row.ActorID, Reason: row.Reason, OccurredAt: row.OccurredAt})
	}
	return out, nil
}

func (r *RequestRepository) NextPublicCode(ctx context.Context, categoryID uuid.UUID) (string, error) {
	code, err := postgres.Queries(ctx, r.pool).NextRequestCode(ctx, categoryID)
	if err != nil {
		return "", fmt.Errorf("next request code: %w", err)
	}
	return code, nil
}

func (r *RequestRepository) MarkStopArrived(ctx context.Context, requestID uuid.UUID, position int, at time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).MarkStopArrived(ctx, gen.MarkStopArrivedParams{RequestID: requestID, Position: int32(position), ArrivedAt: &at}) // #nosec G115 -- bound in the controller
	if err != nil {
		return false, fmt.Errorf("mark stop arrived: %w", err)
	}
	return rows > 0, nil
}

func (r *RequestRepository) RecentDestinations(ctx context.Context, customerID uuid.UUID, limit int) ([]repository.RecentDestination, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRecentDestinations(ctx, gen.ListRecentDestinationsParams{CustomerID: customerID, PageLimit: int32(limit)}) // #nosec G115 -- bound in the controller
	if err != nil {
		return nil, fmt.Errorf("list recent destinations: %w", err)
	}
	out := make([]repository.RecentDestination, 0, len(rows))
	for i := range rows {
		out = append(out, repository.RecentDestination{Point: entities.GeoPoint{Lat: rows[i].Lat, Lng: rows[i].Lng}, Line: rows[i].Line, Reference: rows[i].Reference, UsedAt: rows[i].CreatedAt})
	}
	return out, nil
}

func (r *RequestRepository) LockCustomer(ctx context.Context, customerID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).LockCustomerRequests(ctx, customerID); err != nil {
		return fmt.Errorf("lock customer requests: %w", err)
	}
	return nil
}

func (r *RequestRepository) ActiveOfCustomerService(ctx context.Context, customerID, serviceID uuid.UUID) ([]*entities.ServiceRequest, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListActiveRequestsOfCustomerService(ctx, gen.ListActiveRequestsOfCustomerServiceParams{CustomerID: customerID, ServiceID: serviceID})
	if err != nil {
		return nil, fmt.Errorf("list active requests of service: %w", err)
	}
	return toRequests(ctx, queries, rows)
}

func (r *RequestRepository) PassengerNotice(ctx context.Context, requestID uuid.UUID) (*repository.PassengerNotice, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPassengerNotice(ctx, requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get passenger notice: %w", err)
	}
	return &repository.PassengerNotice{Template: row.PassengerSms, ProviderName: row.ProviderName, VehiclePlate: row.VehiclePlate}, nil
}

func (r *RequestRepository) OrganizationOf(ctx context.Context, requestID uuid.UUID) (*uuid.UUID, error) {
	organizationID, err := postgres.Queries(ctx, r.pool).GetRequestOrganization(ctx, requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get request organization: %w", err)
	}
	return organizationID, nil
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
		place := entities.RequestPlace{AddressID: row.ID, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, Reference: row.Reference, Address: row.Line, ArrivedAt: row.ArrivedAt}
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
		ID: row.ID, PublicCode: row.PublicCode, CustomerID: row.CustomerID, CategoryID: row.CategoryID, CategorySlug: row.CategorySlug, ServiceID: row.ServiceID, ZoneID: row.ZoneID, PolicyID: row.PolicyID,
		Status: entities.RequestStatus(row.Status), Version: int(row.Version), Stops: []entities.RequestPlace{},
		PaymentMethod: entities.PaymentMethod(row.PaymentMethod), PaymentStatus: entities.RequestPaymentStatus(row.PaymentStatus),
		PickupH3R8: row.PickupH3R8, PickupH3R9: cellOrZero(row.PickupH3R9), DestinationH3: row.DestinationH3R8, ContactPhone: row.ContactPhone, PassengerName: row.PassengerName, PassengerPhone: row.PassengerPhone,
		Details: row.Details, ScheduledFor: row.ScheduledFor, SearchRound: int(row.SearchRound), SearchExtended: row.SearchExtended, SearchDeadline: row.SearchDeadlineAt,
		NextActionAt: row.NextActionAt, ReminderAt: row.ReminderAt, ProviderID: row.ProviderID,
		Fare:          entities.RequestFare{EstimateID: row.FareEstimateID, Cents: row.FareCents, CommissionCents: row.CommissionCents, BoostBps: int(row.BoostBps), Currency: row.Currency, RouteGeometry: row.RouteGeometry},
		NeedsOperator: row.NeedsOperator, OperatorReason: row.OperatorReason, IdempotencyKey: row.IdempotencyKey, CancelReason: row.CancelReason, CancelledBy: row.CancelledBy, CancelReasonID: row.CancelReasonID, CancelFeeCents: row.CancelFeeCents, VoucherID: row.VoucherID, VoucherCode: row.VoucherCode, DiscountCents: row.DiscountCents, VehicleTypeID: row.VehicleTypeID, WorkshopID: row.WorkshopID, Prepay: row.Prepay, ExtraDueCents: row.ExtraDueCents, RemovalDay: row.RemovalDay, RemovalWindowID: row.RemovalWindowID, CompletionCode: row.CompletionCode, CompletionTries: int(row.CompletionAttempts),
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
		NextActionAt: o.NextActionAt, FallbackStep: int16(o.FallbackStep), QuoteCents: o.QuoteCents, QuoteNote: o.QuoteNote, RejectReasonID: o.RejectReason, RejectNote: o.RejectNote, // #nosec G115 -- small counter
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

func heatCellOf(s *entities.ServiceRequest) *int64 {
	if s.PickupH3R9 == 0 {
		return nil
	}
	return &s.PickupH3R9
}

func cellOrZero(cell *int64) int64 {
	if cell == nil {
		return 0
	}
	return *cell
}

type requestSearch struct {
	text, digits, plate string
}

func newRequestSearch(raw string) requestSearch {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return requestSearch{}
	}
	keep := func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}
	search := requestSearch{text: "%" + escapeLike(raw) + "%"}
	if digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, raw); digits != "" {
		search.digits = "%" + digits + "%"
	}
	if plate := strings.Map(keep, raw); plate != "" {
		search.plate = "%" + plate + "%"
	}
	return search
}

func escapeLike(text string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(text)
}
