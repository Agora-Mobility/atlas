package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type WorkshopSearch struct {
	CustomerID    uuid.UUID
	Point         entities.GeoPoint
	Search        string
	Category      string
	OnlyFavorites bool
	OnlyHome      bool
	MinRatingBps  int64
	MaxDistanceM  float64
	ByRating      bool
	Limit         int
	Offset        int
}

type WorkshopCard struct {
	ID                  uuid.UUID
	Name                string
	Line                string
	Point               entities.GeoPoint
	DistanceM           float64
	OffersHomeService   bool
	HomeServiceFeeCents int64
	Currency            string
	RatingSum           int64
	RatingCount         int
	IsFavorite          bool
	CoverKey            string
}

type WorkshopClosure struct {
	Day    time.Time
	Reason string
}

type WorkshopProfile struct {
	Schedule    entities.WorkshopSchedule
	RatingSum   int64
	RatingCount int
}

type IBookingRepository interface {
	WorkshopPhotos(ctx context.Context, workshopID uuid.UUID) ([]entities.MediaUpload, error)
	ReplaceWorkshopPhotos(ctx context.Context, workshopID uuid.UUID, uploadIDs []uuid.UUID, now time.Time) error
	SaveSchedule(ctx context.Context, workshopID uuid.UUID, schedule *entities.WorkshopSchedule) (bool, error)
	Profile(ctx context.Context, workshopID uuid.UUID) (*WorkshopProfile, error)
	LockWorkshop(ctx context.Context, workshopID uuid.UUID) error
	AddClosure(ctx context.Context, workshopID uuid.UUID, day time.Time, reason string) error
	RemoveClosure(ctx context.Context, workshopID uuid.UUID, day time.Time) error
	Closures(ctx context.Context, workshopID uuid.UUID, from, to time.Time) ([]WorkshopClosure, error)
	AddMechanic(ctx context.Context, mechanic *entities.WorkshopMechanic) error
	DeactivateMechanic(ctx context.Context, workshopID, id uuid.UUID) (bool, error)
	Mechanics(ctx context.Context, workshopID uuid.UUID) ([]entities.WorkshopMechanic, error)
	Mechanic(ctx context.Context, id uuid.UUID) (*entities.WorkshopMechanic, error)
	AddFavorite(ctx context.Context, customerID, workshopID uuid.UUID, now time.Time) error
	RemoveFavorite(ctx context.Context, customerID, workshopID uuid.UUID) error
	Search(ctx context.Context, filter WorkshopSearch) ([]WorkshopCard, error)
	Promotions(ctx context.Context, workshopID uuid.UUID, now time.Time) ([]entities.Voucher, error)
	LockVoucher(ctx context.Context, id uuid.UUID) (*entities.Voucher, error)
	Reviews(ctx context.Context, workshopID uuid.UUID, limit, offset int) ([]entities.Rating, int64, error)
	Booked(ctx context.Context, workshopID uuid.UUID, since, until time.Time) ([]entities.BookedSlot, error)
	Create(ctx context.Context, booking *entities.Booking) error
	Update(ctx context.Context, booking *entities.Booking) error
	Find(ctx context.Context, id uuid.UUID) (*entities.Booking, error)
	FindForUpdate(ctx context.Context, id uuid.UUID) (*entities.Booking, error)
	OfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Booking, error)
	OfCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]entities.Booking, int64, error)
	OfWorkshop(ctx context.Context, workshopID uuid.UUID, status string, limit, offset int) ([]entities.Booking, int64, error)
	AddExtra(ctx context.Context, extra *entities.BookingExtra) error
	SaveExtra(ctx context.Context, extra *entities.BookingExtra) error
	RecordMove(ctx context.Context, bookingID uuid.UUID, from, to entities.BookingStatus, actorType string, actorID *uuid.UUID, at time.Time) error
}
