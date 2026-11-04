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

type BookingRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IBookingRepository = (*BookingRepository)(nil)

func NewBookingRepository(pool *pgxpool.Pool) *BookingRepository {
	return &BookingRepository{pool: pool}
}

func (r *BookingRepository) SaveSchedule(ctx context.Context, workshopID uuid.UUID, schedule *entities.WorkshopSchedule) (bool, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.UpdateWorkshopSchedule(ctx, gen.UpdateWorkshopScheduleParams{ID: workshopID, Capacity: int32(schedule.Capacity), About: schedule.About, Warranty: schedule.Warranty}) // #nosec G115 -- validated by the entity
	if err != nil {
		return false, fmt.Errorf("save schedule: %w", err)
	}
	if rows == 0 {
		return false, nil
	}
	if err := queries.DeleteWorkshopHours(ctx, workshopID); err != nil {
		return false, fmt.Errorf("clear hours: %w", err)
	}
	for _, day := range schedule.Hours {
		err := queries.InsertWorkshopHours(ctx, gen.InsertWorkshopHoursParams{WorkshopID: workshopID, Weekday: int16(day.Weekday), OpenMinute: int32(day.OpenMinute), CloseMinute: int32(day.CloseMinute)}) // #nosec G115 -- validated by the entity
		if err != nil {
			return false, fmt.Errorf("save hours: %w", err)
		}
	}
	return true, nil
}

func (r *BookingRepository) Profile(ctx context.Context, workshopID uuid.UUID) (*repository.WorkshopProfile, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetWorkshopProfile(ctx, workshopID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workshop profile: %w", err)
	}
	hours, err := queries.ListWorkshopHours(ctx, workshopID)
	if err != nil {
		return nil, fmt.Errorf("list hours: %w", err)
	}
	profile := &repository.WorkshopProfile{Schedule: entities.WorkshopSchedule{Capacity: int(row.Capacity), About: row.About, Warranty: row.Warranty, Hours: make([]entities.WorkshopDayHours, 0, len(hours))}, RatingSum: row.RatingSum, RatingCount: int(row.RatingCount)}
	for i := range hours {
		profile.Schedule.Hours = append(profile.Schedule.Hours, entities.WorkshopDayHours{Weekday: int(hours[i].Weekday), OpenMinute: int(hours[i].OpenMinute), CloseMinute: int(hours[i].CloseMinute)})
	}
	return profile, nil
}

func (r *BookingRepository) LockWorkshop(ctx context.Context, workshopID uuid.UUID) error {
	_, err := postgres.Queries(ctx, r.pool).LockWorkshop(ctx, workshopID)
	if errors.Is(err, pgx.ErrNoRows) {
		return entities.ErrWorkshopUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock workshop: %w", err)
	}
	return nil
}

func (r *BookingRepository) AddClosure(ctx context.Context, workshopID uuid.UUID, day time.Time, reason string) error {
	if err := postgres.Queries(ctx, r.pool).UpsertWorkshopClosure(ctx, gen.UpsertWorkshopClosureParams{WorkshopID: workshopID, ClosedOn: day, Reason: reason}); err != nil {
		return fmt.Errorf("add closure: %w", err)
	}
	return nil
}

func (r *BookingRepository) RemoveClosure(ctx context.Context, workshopID uuid.UUID, day time.Time) error {
	if err := postgres.Queries(ctx, r.pool).DeleteWorkshopClosure(ctx, gen.DeleteWorkshopClosureParams{WorkshopID: workshopID, ClosedOn: day}); err != nil {
		return fmt.Errorf("remove closure: %w", err)
	}
	return nil
}

func (r *BookingRepository) Closures(ctx context.Context, workshopID uuid.UUID, from, to time.Time) ([]repository.WorkshopClosure, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListWorkshopClosures(ctx, gen.ListWorkshopClosuresParams{WorkshopID: workshopID, FromDay: from, ToDay: to})
	if err != nil {
		return nil, fmt.Errorf("list closures: %w", err)
	}
	out := make([]repository.WorkshopClosure, 0, len(rows))
	for i := range rows {
		out = append(out, repository.WorkshopClosure{Day: rows[i].ClosedOn, Reason: rows[i].Reason})
	}
	return out, nil
}

func (r *BookingRepository) AddMechanic(ctx context.Context, m *entities.WorkshopMechanic) error {
	if err := postgres.Queries(ctx, r.pool).InsertWorkshopMechanic(ctx, gen.InsertWorkshopMechanicParams{ID: m.ID, WorkshopID: m.WorkshopID, Name: m.Name, CreatedAt: m.CreatedAt}); err != nil {
		return fmt.Errorf("add mechanic: %w", err)
	}
	return nil
}

func (r *BookingRepository) DeactivateMechanic(ctx context.Context, workshopID, id uuid.UUID) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).DeactivateWorkshopMechanic(ctx, gen.DeactivateWorkshopMechanicParams{ID: id, WorkshopID: workshopID})
	if err != nil {
		return false, fmt.Errorf("deactivate mechanic: %w", err)
	}
	return rows > 0, nil
}

func toMechanic(row *gen.WorkshopMechanic) entities.WorkshopMechanic {
	return entities.WorkshopMechanic{ID: row.ID, WorkshopID: row.WorkshopID, Name: row.Name, IsActive: row.IsActive, CreatedAt: row.CreatedAt}
}

func (r *BookingRepository) Mechanics(ctx context.Context, workshopID uuid.UUID) ([]entities.WorkshopMechanic, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListWorkshopMechanics(ctx, workshopID)
	if err != nil {
		return nil, fmt.Errorf("list mechanics: %w", err)
	}
	out := make([]entities.WorkshopMechanic, 0, len(rows))
	for i := range rows {
		out = append(out, toMechanic(&rows[i]))
	}
	return out, nil
}

func (r *BookingRepository) Mechanic(ctx context.Context, id uuid.UUID) (*entities.WorkshopMechanic, error) {
	row, err := postgres.Queries(ctx, r.pool).GetWorkshopMechanic(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mechanic: %w", err)
	}
	mechanic := toMechanic(&row)
	return &mechanic, nil
}

func (r *BookingRepository) AddFavorite(ctx context.Context, customerID, workshopID uuid.UUID, now time.Time) error {
	if err := postgres.Queries(ctx, r.pool).AddWorkshopFavorite(ctx, gen.AddWorkshopFavoriteParams{CustomerID: customerID, WorkshopID: workshopID, CreatedAt: now}); err != nil {
		return fmt.Errorf("add favorite: %w", err)
	}
	return nil
}

func (r *BookingRepository) RemoveFavorite(ctx context.Context, customerID, workshopID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).RemoveWorkshopFavorite(ctx, gen.RemoveWorkshopFavoriteParams{CustomerID: customerID, WorkshopID: workshopID}); err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	return nil
}

func (r *BookingRepository) Search(ctx context.Context, f repository.WorkshopSearch) ([]repository.WorkshopCard, error) {
	rows, err := postgres.Queries(ctx, r.pool).SearchWorkshops(ctx, gen.SearchWorkshopsParams{
		Lng: f.Point.Lng, Lat: f.Point.Lat, CustomerID: f.CustomerID, Search: f.Search, Category: f.Category, OnlyFavorites: f.OnlyFavorites, OnlyHome: f.OnlyHome,
		MinRatingBps: f.MinRatingBps, MaxDistanceM: f.MaxDistanceM, ByRating: f.ByRating, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset), // #nosec G115 -- bounded by pagination
	})
	if err != nil {
		return nil, fmt.Errorf("search workshops: %w", err)
	}
	out := make([]repository.WorkshopCard, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, repository.WorkshopCard{
			ID: row.ID, Name: row.Name, Line: row.Line, Point: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}, DistanceM: row.DistanceM, OffersHomeService: row.OffersHomeService,
			HomeServiceFeeCents: row.HomeServiceFeeCents, Currency: row.Currency, RatingSum: row.RatingSum, RatingCount: int(row.RatingCount), IsFavorite: row.IsFavorite,
		})
	}
	return out, nil
}

func (r *BookingRepository) Promotions(ctx context.Context, workshopID uuid.UUID, now time.Time) ([]entities.Voucher, error) {
	id := workshopID
	rows, err := postgres.Queries(ctx, r.pool).ActiveWorkshopVouchers(ctx, gen.ActiveWorkshopVouchersParams{WorkshopID: &id, Now: now})
	if err != nil {
		return nil, fmt.Errorf("list promotions: %w", err)
	}
	out := make([]entities.Voucher, 0, len(rows))
	for i := range rows {
		out = append(out, *toVoucher(&rows[i]))
	}
	return out, nil
}

func (r *BookingRepository) LockVoucher(ctx context.Context, id uuid.UUID) (*entities.Voucher, error) {
	row, err := postgres.Queries(ctx, r.pool).LockVoucher(ctx, id)
	return voucherOrNil(&row, err, "lock voucher")
}

func (r *BookingRepository) Reviews(ctx context.Context, workshopID uuid.UUID, limit, offset int) ([]entities.Rating, int64, error) {
	id := workshopID
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListWorkshopRatings(ctx, gen.ListWorkshopRatingsParams{WorkshopID: &id, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}
	total, err := queries.CountWorkshopRatings(ctx, &id)
	if err != nil {
		return nil, 0, fmt.Errorf("count reviews: %w", err)
	}
	out := make([]entities.Rating, 0, len(rows))
	for i := range rows {
		out = append(out, entities.Rating{ID: rows[i].ID, RequestID: rows[i].RequestID, Stars: int(rows[i].Stars), ComplimentIDs: rows[i].ComplimentIds, Comment: rows[i].Comment, CreatedAt: rows[i].CreatedAt})
	}
	return out, total, nil
}

func (r *BookingRepository) Booked(ctx context.Context, workshopID uuid.UUID, since, until time.Time) ([]entities.BookedSlot, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListBookedSlots(ctx, gen.ListBookedSlotsParams{WorkshopID: workshopID, Since: since, Until: until})
	if err != nil {
		return nil, fmt.Errorf("list booked: %w", err)
	}
	out := make([]entities.BookedSlot, 0, len(rows))
	for i := range rows {
		out = append(out, entities.BookedSlot{StartsAt: rows[i].StartsAt, EndsAt: rows[i].EndsAt})
	}
	return out, nil
}

func (r *BookingRepository) Create(ctx context.Context, b *entities.Booking) error {
	queries := postgres.Queries(ctx, r.pool)
	var mileage *int32
	if b.MileageKm != nil {
		value := int32(*b.MileageKm) // #nosec G115 -- validated by the entity
		mileage = &value
	}
	err := queries.InsertBooking(ctx, gen.InsertBookingParams{
		ID: b.ID, RequestID: b.RequestID, WorkshopID: b.WorkshopID, CustomerID: b.CustomerID, VehicleID: b.VehicleID, Status: string(b.Status), AtHome: b.AtHome, HomeFeeCents: b.HomeFeeCents,
		ServicesCents: b.ServicesCents, StartsAt: b.StartsAt, EndsAt: b.EndsAt, Description: b.Description, MileageKm: mileage, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("create booking: %w", err)
	}
	for i := range b.Items {
		item := &b.Items[i]
		err := queries.InsertBookingItem(ctx, gen.InsertBookingItemParams{ID: item.ID, BookingID: b.ID, ServiceID: item.ServiceID, Name: item.Name, PriceCents: item.PriceCents, DurationMinutes: int32(item.DurationMinutes)}) // #nosec G115 -- validated by the workshop service
		if err != nil {
			return fmt.Errorf("create booking item: %w", err)
		}
	}
	return nil
}

func (r *BookingRepository) Update(ctx context.Context, b *entities.Booking) error {
	err := postgres.Queries(ctx, r.pool).UpdateBooking(ctx, gen.UpdateBookingParams{ID: b.ID, Status: string(b.Status), MechanicID: b.MechanicID, ExpectedReadyAt: b.ExpectedReadyAt, UpdatedAt: b.UpdatedAt})
	if err != nil {
		return fmt.Errorf("update booking: %w", err)
	}
	return nil
}

func (r *BookingRepository) Find(ctx context.Context, id uuid.UUID) (*entities.Booking, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetBookingDetail(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get booking: %w", err)
	}
	booking := &entities.Booking{
		ID: row.ID, RequestID: row.RequestID, Code: row.PublicCode, WorkshopID: row.WorkshopID, WorkshopName: row.WorkshopName, OrganizationID: row.OrganizationID, CustomerID: row.CustomerID, VehicleID: row.VehicleID,
		MechanicID: row.MechanicID, MechanicName: row.MechanicName, Status: entities.BookingStatus(row.Status), AtHome: row.AtHome, HomeFeeCents: row.HomeFeeCents, ServicesCents: row.ServicesCents,
		StartsAt: row.StartsAt, EndsAt: row.EndsAt, ExpectedReadyAt: row.ExpectedReadyAt, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.MileageKm != nil {
		value := int(*row.MileageKm)
		booking.MileageKm = &value
	}
	items, err := queries.ListBookingItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list booking items: %w", err)
	}
	for i := range items {
		booking.Items = append(booking.Items, entities.BookingItem{ID: items[i].ID, ServiceID: items[i].ServiceID, Name: items[i].Name, PriceCents: items[i].PriceCents, DurationMinutes: int(items[i].DurationMinutes)})
	}
	extras, err := queries.ListBookingExtras(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list booking extras: %w", err)
	}
	for i := range extras {
		booking.Extras = append(booking.Extras, entities.BookingExtra{ID: extras[i].ID, BookingID: id, Description: extras[i].Description, AmountCents: extras[i].AmountCents, Status: entities.BookingExtraStatus(extras[i].Status), DecidedAt: extras[i].DecidedAt, CreatedAt: extras[i].CreatedAt})
	}
	return booking, nil
}

func (r *BookingRepository) FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.Booking, error) {
	_, err := postgres.Queries(ctx, r.pool).LockBooking(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock booking: %w", err)
	}
	return r.Find(ctx, id)
}

func (r *BookingRepository) OfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Booking, error) {
	id, err := postgres.Queries(ctx, r.pool).GetBookingIDOfRequest(ctx, requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("booking of request: %w", err)
	}
	return r.FindForUpdate(ctx, id)
}

func (r *BookingRepository) hydrate(ctx context.Context, ids []uuid.UUID) ([]entities.Booking, error) {
	out := make([]entities.Booking, 0, len(ids))
	for _, id := range ids {
		booking, err := r.Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if booking != nil {
			out = append(out, *booking)
		}
	}
	return out, nil
}

func (r *BookingRepository) OfCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]entities.Booking, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	ids, err := queries.ListCustomerBookings(ctx, gen.ListCustomerBookingsParams{CustomerID: customerID, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list bookings: %w", err)
	}
	total, err := queries.CountCustomerBookings(ctx, customerID)
	if err != nil {
		return nil, 0, fmt.Errorf("count bookings: %w", err)
	}
	bookings, err := r.hydrate(ctx, ids)
	return bookings, total, err
}

func (r *BookingRepository) OfWorkshop(ctx context.Context, workshopID uuid.UUID, status string, limit, offset int) ([]entities.Booking, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	ids, err := queries.ListWorkshopBookings(ctx, gen.ListWorkshopBookingsParams{WorkshopID: workshopID, Status: status, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list workshop bookings: %w", err)
	}
	total, err := queries.CountWorkshopBookings(ctx, gen.CountWorkshopBookingsParams{WorkshopID: workshopID, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("count workshop bookings: %w", err)
	}
	bookings, err := r.hydrate(ctx, ids)
	return bookings, total, err
}

func (r *BookingRepository) AddExtra(ctx context.Context, e *entities.BookingExtra) error {
	err := postgres.Queries(ctx, r.pool).InsertBookingExtra(ctx, gen.InsertBookingExtraParams{ID: e.ID, BookingID: e.BookingID, Description: e.Description, AmountCents: e.AmountCents, Status: string(e.Status), CreatedAt: e.CreatedAt})
	if err != nil {
		return fmt.Errorf("add extra: %w", err)
	}
	return nil
}

func (r *BookingRepository) SaveExtra(ctx context.Context, e *entities.BookingExtra) error {
	err := postgres.Queries(ctx, r.pool).UpdateBookingExtra(ctx, gen.UpdateBookingExtraParams{ID: e.ID, Status: string(e.Status), DecidedAt: e.DecidedAt})
	if err != nil {
		return fmt.Errorf("save extra: %w", err)
	}
	return nil
}

func (r *BookingRepository) RecordMove(ctx context.Context, bookingID uuid.UUID, from, to entities.BookingStatus, actorType string, actorID *uuid.UUID, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).InsertBookingEvent(ctx, gen.InsertBookingEventParams{BookingID: bookingID, FromStatus: string(from), ToStatus: string(to), ActorType: actorType, ActorID: actorID, OccurredAt: at})
	if err != nil {
		return fmt.Errorf("record booking move: %w", err)
	}
	return nil
}
