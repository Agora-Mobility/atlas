package bookings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/bookings/dtos"
)

const (
	actorCustomer   = "customer"
	actorWorkshop   = "workshop"
	actorSystem     = "system"
	maintenanceSlug = "maintenance"
	dateLayout      = "2006-01-02"
	maxCalendarDays = 62
	metersDecimal   = 0.5
	ratingScale     = 100
)

var (
	ErrOutsideServiceArea = errors.New("this place is outside the service area")
	ErrServiceNotOffered  = errors.New("this service is not offered here")
	ErrBadDate            = errors.New("the date is not valid")
	ErrTooFarAhead        = errors.New("this date is too far ahead")
	ErrVehicleRequired    = errors.New("choose one of your vehicles")
	ErrHomeAddress        = errors.New("a home service needs the address")
	ErrReasonNotValid     = errors.New("this reason does not apply")
	ErrChatClosed         = errors.New("the chat of this booking is closed")
)

type ZoneResolver interface {
	Resolve(ctx context.Context, point entities.GeoPoint) (*entities.ServiceZone, error)
}

type Refunder interface {
	RefundPrepaid(ctx context.Context, requestID uuid.UUID, reason string) error
	OnPrepaidCompleted(ctx context.Context, request *entities.ServiceRequest, now time.Time) error
}

type Deps struct {
	Bookings repository.IBookingRepository
	Requests repository.IRequestRepository
	Fleet    repository.IFleetRepository
	Garage   repository.IGarageRepository
	Config   repository.IDispatchConfigRepository
	Pricing  repository.IPricingRepository
	Payments repository.IPaymentRepository
	Cancels  repository.ICancellationRepository
	Vouchers repository.IVoucherRepository
	Settings repository.IReferenceRepository
	Live     repository.ILiveRepository
	Devices  repository.IDeviceRepository
	Outbox   repository.IOutboxRepository
	Tx       repository.ITransactionManager
	Audit    repository.IAuditLogRepository
	Calls    services.ICallBridge
	Geo      services.IGeoIndex
	Zones    ZoneResolver
	Refunds  Refunder
}

type Usecase struct {
	Deps
	now func() time.Time
}

func NewUsecase(deps Deps) *Usecase {
	return &Usecase{Deps: deps, now: func() time.Time { return time.Now().UTC() }}
}

func (u *Usecase) setting(ctx context.Context, key string) (int, error) {
	raw, err := u.Settings.Setting(ctx, key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(raw)
}

func (u *Usecase) location(ctx context.Context, point entities.GeoPoint) (*entities.ServiceZone, error) {
	zone, err := u.Zones.Resolve(ctx, point)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, ErrOutsideServiceArea
	}
	return zone, nil
}

func (u *Usecase) activeWorkshop(ctx context.Context, id uuid.UUID) (*entities.Workshop, error) {
	workshop, err := u.Fleet.FindWorkshop(ctx, id)
	if err != nil {
		return nil, err
	}
	if workshop == nil || !workshop.IsActive {
		return nil, entities.ErrWorkshopUnavailable
	}
	return workshop, nil
}

func (u *Usecase) ownWorkshop(ctx context.Context, organizationID, id uuid.UUID) (*entities.Workshop, error) {
	workshop, err := u.Fleet.FindWorkshop(ctx, id)
	if err != nil {
		return nil, err
	}
	if workshop == nil || workshop.OrganizationID != organizationID {
		return nil, entities.ErrWorkshopUnavailable
	}
	return workshop, nil
}

func (u *Usecase) audit(ctx context.Context, actor entities.Actor, action, entity, id string, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entity, id, nil, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Audit.Record(ctx, entry)
}

func (u *Usecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Outbox.Save(ctx, envelope)
}

func (u *Usecase) announce(ctx context.Context, booking *entities.Booking, code string, from entities.BookingStatus, actor string, actorID *uuid.UUID, now time.Time) error {
	if err := u.Bookings.RecordMove(ctx, booking.ID, from, booking.Status, actor, actorID, now); err != nil {
		return err
	}
	return u.publish(ctx, events.BookingChangedV1{
		BookingID: booking.ID, RequestID: booking.RequestID, CustomerID: booking.CustomerID, Code: code, WorkshopName: booking.WorkshopName, Status: string(booking.Status), StartsAt: booking.StartsAt, OccurredAt: now,
	})
}

func toPromotion(v *entities.Voucher) dtos.PromotionView {
	return dtos.PromotionView{ID: v.ID, Description: v.Description, DiscountBps: v.DiscountBps, DiscountCents: v.DiscountCents, ServiceID: v.WorkshopService, ValidUntil: v.ValidUntil}
}

func (u *Usecase) promotionsFor(ctx context.Context, workshopID, customerID uuid.UUID, now time.Time) ([]entities.Voucher, error) {
	promotions, err := u.Bookings.Promotions(ctx, workshopID, now)
	if err != nil {
		return nil, err
	}
	out := make([]entities.Voucher, 0, len(promotions))
	for i := range promotions {
		used, err := u.Vouchers.CountCustomerRedemptions(ctx, promotions[i].ID, customerID)
		if err != nil {
			return nil, err
		}
		if used < int64(promotions[i].PerCustomerLimit) {
			out = append(out, promotions[i])
		}
	}
	return out, nil
}

func (u *Usecase) cardOf(ctx context.Context, card *repository.WorkshopCard, customerID uuid.UUID, now time.Time) (*dtos.WorkshopCardView, *repository.WorkshopProfile, error) {
	profile, err := u.Bookings.Profile(ctx, card.ID)
	if err != nil {
		return nil, nil, err
	}
	if profile == nil {
		return nil, nil, entities.ErrWorkshopUnavailable
	}
	view := &dtos.WorkshopCardView{
		ID: card.ID, Name: card.Name, Address: card.Line, Lat: card.Point.Lat, Lng: card.Point.Lng, DistanceM: int(card.DistanceM + metersDecimal), Rating: entities.AverageRating(card.RatingSum, card.RatingCount), RatingCount: card.RatingCount,
		OffersHomeService: card.OffersHomeService, HomeServiceFeeCents: card.HomeServiceFeeCents, Currency: card.Currency, IsFavorite: card.IsFavorite, Promotions: []dtos.PromotionView{},
	}
	zone, err := u.Zones.Resolve(ctx, card.Point)
	if err != nil {
		return nil, nil, err
	}
	local := now
	if zone != nil {
		local = now.In(zone.Location())
	}
	if until, open := profile.Schedule.OpenUntil(local); open {
		view.IsOpen, view.OpenUntil = true, &until
	}
	promotions, err := u.promotionsFor(ctx, card.ID, customerID, now)
	if err != nil {
		return nil, nil, err
	}
	for i := range promotions {
		view.Promotions = append(view.Promotions, toPromotion(&promotions[i]))
	}
	return view, profile, nil
}

type SearchInput struct {
	Lat, Lng      float64
	Search        string
	Category      string
	OnlyFavorites bool
	OnlyHome      bool
	MinRating     float64
	MaxDistanceKm float64
	SortByRating  bool
}

func (u *Usecase) Search(ctx context.Context, customerID uuid.UUID, in SearchInput, page pagination.Params) (*dtos.WorkshopCardList, error) {
	page = page.Normalize()
	cards, err := u.Bookings.Search(ctx, repository.WorkshopSearch{
		CustomerID: customerID, Point: entities.GeoPoint{Lat: in.Lat, Lng: in.Lng}, Search: strings.TrimSpace(in.Search), Category: in.Category, OnlyFavorites: in.OnlyFavorites, OnlyHome: in.OnlyHome,
		MinRatingBps: int64(in.MinRating * ratingScale), MaxDistanceM: in.MaxDistanceKm * 1000, ByRating: in.SortByRating, Limit: int(page.Limit()), Offset: int(page.Offset()),
	})
	if err != nil {
		return nil, err
	}
	now := u.now()
	list := &dtos.WorkshopCardList{Items: make([]dtos.WorkshopCardView, 0, len(cards)), Page: page.Page, Size: page.Size}
	for i := range cards {
		view, _, err := u.cardOf(ctx, &cards[i], customerID, now)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, *view)
	}
	return list, nil
}

func (u *Usecase) Detail(ctx context.Context, customerID, workshopID uuid.UUID, lat, lng float64) (*dtos.WorkshopDetailView, error) {
	workshop, err := u.activeWorkshop(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	point := entities.GeoPoint{Lat: workshop.Address.Point.Lat, Lng: workshop.Address.Point.Lng}
	profile, err := u.Bookings.Profile(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, entities.ErrWorkshopUnavailable
	}
	favorites, err := u.Bookings.Search(ctx, repository.WorkshopSearch{CustomerID: customerID, Point: entities.GeoPoint{Lat: lat, Lng: lng}, OnlyFavorites: true, Search: workshop.Name, Limit: 50})
	if err != nil {
		return nil, err
	}
	card := repository.WorkshopCard{ID: workshopID, Name: workshop.Name, Line: workshop.Address.Line, Point: point, OffersHomeService: workshop.OffersHomeService, HomeServiceFeeCents: workshop.HomeServiceFeeCents, Currency: workshop.Currency, RatingSum: profile.RatingSum, RatingCount: profile.RatingCount, DistanceM: entities.DistanceMeters(point, entities.GeoPoint{Lat: lat, Lng: lng})}
	for i := range favorites {
		if favorites[i].ID == workshopID {
			card.IsFavorite = true
		}
	}
	base, _, err := u.cardOf(ctx, &card, customerID, u.now())
	if err != nil {
		return nil, err
	}
	view := &dtos.WorkshopDetailView{WorkshopCardView: *base, About: profile.Schedule.About, Warranty: profile.Schedule.Warranty, Capacity: profile.Schedule.Capacity, Hours: make([]dtos.HoursInput, 0, len(profile.Schedule.Hours)), Services: []dtos.WorkshopServiceView{}}
	for _, day := range profile.Schedule.Hours {
		view.Hours = append(view.Hours, dtos.HoursInput{Weekday: day.Weekday, OpenMinute: day.OpenMinute, CloseMinute: day.CloseMinute})
	}
	for i := range workshop.Services {
		service := &workshop.Services[i]
		if service.IsActive {
			view.Services = append(view.Services, dtos.WorkshopServiceView{ID: service.ID, Name: service.Name, Description: service.Description, PriceCents: service.PriceCents, DurationMinutes: service.DurationMinutes})
		}
	}
	return view, nil
}

func (u *Usecase) Reviews(ctx context.Context, workshopID uuid.UUID, page pagination.Params) (*dtos.ReviewList, error) {
	if _, err := u.activeWorkshop(ctx, workshopID); err != nil {
		return nil, err
	}
	page = page.Normalize()
	ratings, total, err := u.Bookings.Reviews(ctx, workshopID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	profile, err := u.Bookings.Profile(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	list := &dtos.ReviewList{Items: make([]dtos.ReviewView, 0, len(ratings)), Total: total, Page: page.Page, Size: page.Size}
	if profile != nil {
		list.Average = entities.AverageRating(profile.RatingSum, profile.RatingCount)
	}
	for i := range ratings {
		list.Items = append(list.Items, dtos.ReviewView{ID: ratings[i].ID, Stars: ratings[i].Stars, Comment: ratings[i].Comment, CreatedAt: ratings[i].CreatedAt})
	}
	return list, nil
}

func (u *Usecase) SetFavorite(ctx context.Context, customerID, workshopID uuid.UUID, favorite bool) error {
	if !favorite {
		return u.Bookings.RemoveFavorite(ctx, customerID, workshopID)
	}
	if _, err := u.activeWorkshop(ctx, workshopID); err != nil {
		return err
	}
	return u.Bookings.AddFavorite(ctx, customerID, workshopID, u.now())
}

func chosenServices(workshop *entities.Workshop, ids []uuid.UUID) ([]entities.WorkshopService, error) {
	seen := map[uuid.UUID]bool{}
	out := make([]entities.WorkshopService, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, entities.ErrWorkshopServiceInvalid
		}
		seen[id] = true
		found := false
		for i := range workshop.Services {
			if workshop.Services[i].ID == id && workshop.Services[i].IsActive {
				out = append(out, workshop.Services[i])
				found = true
			}
		}
		if !found {
			return nil, entities.ErrWorkshopServiceInvalid
		}
	}
	return out, nil
}

func (u *Usecase) Calendar(ctx context.Context, workshopID uuid.UUID, serviceIDs []uuid.UUID, from string, days int) (*dtos.CalendarView, error) {
	workshop, err := u.activeWorkshop(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	chosen, err := chosenServices(workshop, serviceIDs)
	if err != nil {
		return nil, err
	}
	duration := entities.TotalDuration(chosen)
	zone, err := u.location(ctx, workshop.Address.Point)
	if err != nil {
		return nil, err
	}
	location := zone.Location()
	first, err := time.ParseInLocation(dateLayout, from, location)
	if err != nil {
		return nil, ErrBadDate
	}
	if days < 1 || days > maxCalendarDays {
		return nil, ErrBadDate
	}
	profile, err := u.Bookings.Profile(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	step, err := u.setting(ctx, entities.SettingBookingSlotMinutes)
	if err != nil {
		return nil, err
	}
	last := first.AddDate(0, 0, days)
	closures, err := u.Bookings.Closures(ctx, workshopID, first, last)
	if err != nil {
		return nil, err
	}
	closed := map[string]bool{}
	for i := range closures {
		closed[closures[i].Day.Format(dateLayout)] = true
	}
	booked, err := u.Bookings.Booked(ctx, workshopID, first, last)
	if err != nil {
		return nil, err
	}
	view := &dtos.CalendarView{DurationMinutes: duration, Days: make([]dtos.DayView, 0, days)}
	now := u.now()
	for offset := range days {
		day := first.AddDate(0, 0, offset)
		date := day.Format(dateLayout)
		if closed[date] {
			view.Days = append(view.Days, dtos.DayView{Date: date, Status: string(entities.DayClosed)})
			continue
		}
		slots, status := profile.Schedule.FreeSlots(day, booked, duration, step, now)
		item := dtos.DayView{Date: date, Status: string(status)}
		for i := range slots {
			item.Slots = append(item.Slots, dtos.SlotView{StartsAt: slots[i].StartsAt, EndsAt: slots[i].EndsAt})
		}
		view.Days = append(view.Days, item)
	}
	return view, nil
}

func (u *Usecase) bestPromotion(ctx context.Context, workshopID, customerID uuid.UUID, serviceIDs []uuid.UUID, total int64, now time.Time) (*entities.Voucher, int64, error) {
	promotions, err := u.promotionsFor(ctx, workshopID, customerID, now)
	if err != nil {
		return nil, 0, err
	}
	var best *entities.Voucher
	bestCents := int64(0)
	for i := range promotions {
		candidate, err := u.Bookings.LockVoucher(ctx, promotions[i].ID)
		if err != nil {
			return nil, 0, err
		}
		if candidate == nil || !candidate.AppliesToBooking(workshopID, serviceIDs) || candidate.CheckApplies(nil, now) != nil {
			continue
		}
		used, err := u.Vouchers.CountCustomerRedemptions(ctx, candidate.ID, customerID)
		if err != nil {
			return nil, 0, err
		}
		if used >= int64(candidate.PerCustomerLimit) {
			continue
		}
		if candidate.MaxRedemptions != nil {
			all, err := u.Vouchers.CountRedemptions(ctx, candidate.ID)
			if err != nil {
				return nil, 0, err
			}
			if all >= int64(*candidate.MaxRedemptions) {
				continue
			}
		}
		cents, err := candidate.DiscountFor(total)
		if err != nil {
			if errors.Is(err, entities.ErrVoucherBelowMinimum) {
				continue
			}
			return nil, 0, err
		}
		if cents > bestCents {
			best, bestCents = candidate, cents
		}
	}
	return best, bestCents, nil
}

func (u *Usecase) Create(ctx context.Context, customerID uuid.UUID, idempotencyKey string, in dtos.CreateBookingInput) (*dtos.BookingView, bool, error) {
	existing, err := u.Requests.FindByIdempotencyKey(ctx, customerID, idempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		booking, err := u.Bookings.OfRequest(ctx, existing.ID)
		if err != nil {
			return nil, false, err
		}
		if booking == nil {
			return nil, false, entities.ErrBookingNotFound
		}
		view, err := u.viewOf(ctx, booking)
		return view, false, err
	}
	workshop, err := u.activeWorkshop(ctx, in.WorkshopID)
	if err != nil {
		return nil, false, err
	}
	chosen, err := chosenServices(workshop, in.ServiceIDs)
	if err != nil {
		return nil, false, err
	}
	duration := entities.TotalDuration(chosen)
	vehicle, err := u.Garage.Vehicle(ctx, in.VehicleID, customerID)
	if err != nil {
		return nil, false, err
	}
	if vehicle == nil {
		return nil, false, ErrVehicleRequired
	}
	if in.AtHome && !workshop.OffersHomeService {
		return nil, false, entities.ErrHomeServiceUnavailable
	}
	place := entities.RequestPlace{Point: workshop.Address.Point, Address: workshop.Address.Line}
	homeFee := int64(0)
	if in.AtHome {
		if in.HomeLat == nil || in.HomeLng == nil {
			return nil, false, ErrHomeAddress
		}
		place = entities.RequestPlace{Point: entities.GeoPoint{Lat: *in.HomeLat, Lng: *in.HomeLng}, Address: in.HomeAddress}
		homeFee = workshop.HomeServiceFeeCents
	}
	zone, err := u.location(ctx, workshop.Address.Point)
	if err != nil {
		return nil, false, err
	}
	offered, err := u.Config.ListServicesOfCategory(ctx, maintenanceSlug)
	if err != nil {
		return nil, false, err
	}
	if len(offered) == 0 || !offered[0].IsOffered {
		return nil, false, ErrServiceNotOffered
	}
	service := offered[0]
	policy, err := u.Config.ResolvePolicy(ctx, service.CategoryID, zone.ID)
	if err != nil {
		return nil, false, err
	}
	if policy == nil {
		return nil, false, ErrServiceNotOffered
	}
	method := entities.PaymentMethod(in.PaymentMethod)
	if method == "" {
		method = entities.PaymentGPO
	}
	outstanding, err := u.Payments.HasOutstanding(ctx, customerID)
	if err != nil {
		return nil, false, err
	}
	if outstanding {
		return nil, false, entities.ErrPaymentOutstanding
	}
	enabled, err := u.Payments.IsMethodEnabled(ctx, service.CategoryID, method)
	if err != nil {
		return nil, false, err
	}
	if !enabled {
		return nil, false, entities.ErrPaymentMethodUnavailable
	}
	tariff, err := u.Pricing.CurrentTariff(ctx, service.ID, zone.ID)
	if err != nil {
		return nil, false, err
	}
	if tariff == nil {
		return nil, false, ErrServiceNotOffered
	}
	maxDays, err := u.setting(ctx, entities.SettingBookingMaxDays)
	if err != nil {
		return nil, false, err
	}
	step, err := u.setting(ctx, entities.SettingBookingSlotMinutes)
	if err != nil {
		return nil, false, err
	}
	now := u.now()
	start := in.StartsAt.In(zone.Location())
	if start.After(now.AddDate(0, 0, maxDays)) {
		return nil, false, ErrTooFarAhead
	}
	servicesCents := int64(0)
	items := make([]entities.BookingItem, 0, len(chosen))
	ids := make([]uuid.UUID, 0, len(chosen))
	for i := range chosen {
		itemID, err := uuid.NewV7()
		if err != nil {
			return nil, false, err
		}
		servicesCents += chosen[i].PriceCents
		ids = append(ids, chosen[i].ID)
		items = append(items, entities.BookingItem{ID: itemID, ServiceID: chosen[i].ID, Name: chosen[i].Name, PriceCents: chosen[i].PriceCents, DurationMinutes: chosen[i].DurationMinutes})
	}
	total := servicesCents + homeFee
	commission := tariff.CommissionFor(total)
	requestID, err := uuid.NewV7()
	if err != nil {
		return nil, false, err
	}
	bookingID, err := uuid.NewV7()
	if err != nil {
		return nil, false, err
	}
	request, err := entities.NewServiceRequest(entities.NewRequestInput{
		ID: requestID, CustomerID: customerID, Service: service, Policy: policy, Zone: zone, Pickup: place, Fare: entities.RequestFare{Cents: &total, CommissionCents: &commission, Currency: workshop.Currency},
		PaymentMethod: method, IdempotencyKey: idempotencyKey, Now: now,
	})
	if err != nil {
		return nil, false, err
	}
	startUTC := start.UTC()
	request.ScheduledFor, request.WorkshopID, request.Status = &startUTC, &workshop.ID, entities.RequestScheduled
	request.Prepay = method == entities.PaymentReference
	if request.PublicCode, err = u.Requests.NextPublicCode(ctx, service.CategoryID); err != nil {
		return nil, false, err
	}
	if request.PickupH3R8, err = u.Geo.Cell(request.Pickup.Point, entities.AnalyticsResolution); err != nil {
		return nil, false, err
	}
	if request.PickupH3R9, err = u.Geo.Cell(request.Pickup.Point, entities.HeatmapResolution); err != nil {
		return nil, false, err
	}
	status := entities.BookingConfirmed
	if request.Prepay {
		status = entities.BookingAwaitingPayment
	}
	booking := &entities.Booking{
		ID: bookingID, RequestID: requestID, Code: request.PublicCode, WorkshopID: workshop.ID, WorkshopName: workshop.Name, OrganizationID: workshop.OrganizationID, CustomerID: customerID, VehicleID: vehicle.ID, Status: status,
		AtHome: in.AtHome, HomeFeeCents: homeFee, ServicesCents: servicesCents, StartsAt: startUTC, EndsAt: startUTC.Add(time.Duration(duration) * time.Minute), Description: in.Description, MileageKm: in.MileageKm, Items: items, CreatedAt: now, UpdatedAt: now,
	}
	if err := booking.Validate(); err != nil {
		return nil, false, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Bookings.LockWorkshop(ctx, workshop.ID); err != nil {
			return err
		}
		if err := u.checkSlot(ctx, workshop.ID, start, duration, step, now); err != nil {
			return err
		}
		promotion, discount, err := u.bestPromotion(ctx, workshop.ID, customerID, ids, total, now)
		if err != nil {
			return err
		}
		if promotion != nil {
			request.VoucherID, request.DiscountCents = &promotion.ID, discount
			request.VoucherCode = promotion.Description
		}
		if err := u.Requests.Create(ctx, request); err != nil {
			return err
		}
		if promotion != nil {
			if err := u.Vouchers.Redeem(ctx, promotion.ID, customerID, &request.ID, discount); err != nil {
				return err
			}
			if err := u.Requests.Update(ctx, request, request.Touch(now)); err != nil {
				return err
			}
		}
		if err := u.Bookings.Create(ctx, booking); err != nil {
			return err
		}
		return u.announce(ctx, booking, request.PublicCode, status, actorCustomer, &customerID, now)
	})
	if errors.Is(err, repository.ErrDuplicateIdempotency) {
		winner, err := u.Requests.FindByIdempotencyKey(ctx, customerID, idempotencyKey)
		if err != nil {
			return nil, false, err
		}
		found, err := u.Bookings.OfRequest(ctx, winner.ID)
		if err != nil {
			return nil, false, err
		}
		view, err := u.viewOf(ctx, found)
		return view, false, err
	}
	if err != nil {
		return nil, false, err
	}
	view, err := u.viewOf(ctx, booking)
	return view, true, err
}

func (u *Usecase) checkSlot(ctx context.Context, workshopID uuid.UUID, start time.Time, duration, step int, now time.Time) error {
	profile, err := u.Bookings.Profile(ctx, workshopID)
	if err != nil {
		return err
	}
	if profile == nil {
		return entities.ErrWorkshopUnavailable
	}
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	closures, err := u.Bookings.Closures(ctx, workshopID, day, day)
	if err != nil {
		return err
	}
	if len(closures) > 0 {
		return entities.ErrBookingSlotInvalid
	}
	booked, err := u.Bookings.Booked(ctx, workshopID, day, day.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	return profile.Schedule.CheckSlot(start, booked, duration, step, now)
}

func (u *Usecase) viewOf(ctx context.Context, b *entities.Booking) (*dtos.BookingView, error) {
	request, err := u.Requests.Find(ctx, b.RequestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, entities.ErrBookingNotFound
	}
	view := &dtos.BookingView{
		ID: b.ID, RequestID: b.RequestID, Code: request.PublicCode, Status: string(b.Status), WorkshopID: b.WorkshopID, WorkshopName: b.WorkshopName, AtHome: b.AtHome, StartsAt: b.StartsAt, EndsAt: b.EndsAt, ExpectedReadyAt: b.ExpectedReadyAt,
		Mechanic: b.MechanicName, Description: b.Description, MileageKm: b.MileageKm, Items: make([]dtos.BookingItemView, 0, len(b.Items)), Extras: make([]dtos.ExtraView, 0, len(b.Extras)),
		ServicesCents: b.ServicesCents, HomeFeeCents: b.HomeFeeCents, ExtrasCents: b.ApprovedExtrasCents(), DiscountCents: request.DiscountCents, Promotion: request.VoucherCode, TotalCents: b.BaseCents() + b.ApprovedExtrasCents() - request.DiscountCents,
		AmountDueCents: request.AmountDueCents(), Currency: request.Fare.Currency, PaymentMethod: string(request.PaymentMethod), PaymentStatus: string(request.PaymentStatus), CreatedAt: b.CreatedAt,
	}
	if vehicle, err := u.Garage.Vehicle(ctx, b.VehicleID, b.CustomerID); err == nil && vehicle != nil {
		view.Vehicle = &dtos.VehicleSummary{Brand: vehicle.Brand, Model: vehicle.Model, Plate: vehicle.Plate}
	}
	for i := range b.Items {
		view.Items = append(view.Items, dtos.BookingItemView{ServiceID: b.Items[i].ServiceID, Name: b.Items[i].Name, PriceCents: b.Items[i].PriceCents, DurationMinutes: b.Items[i].DurationMinutes})
	}
	for i := range b.Extras {
		view.Extras = append(view.Extras, dtos.ExtraView{ID: b.Extras[i].ID, Description: b.Extras[i].Description, AmountCents: b.Extras[i].AmountCents, Status: string(b.Extras[i].Status), DecidedAt: b.Extras[i].DecidedAt})
	}
	return view, nil
}

func (u *Usecase) customerBooking(ctx context.Context, customerID, id uuid.UUID) (*entities.Booking, error) {
	booking, err := u.Bookings.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if booking == nil || booking.CustomerID != customerID {
		return nil, entities.ErrBookingNotFound
	}
	return booking, nil
}

func (u *Usecase) Get(ctx context.Context, customerID, id uuid.UUID) (*dtos.BookingView, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) listView(ctx context.Context, bookings []entities.Booking, total int64, page pagination.Params) (*dtos.BookingList, error) {
	list := &dtos.BookingList{Items: make([]dtos.BookingView, 0, len(bookings)), Total: total, Page: page.Page, Size: page.Size}
	for i := range bookings {
		view, err := u.viewOf(ctx, &bookings[i])
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, *view)
	}
	return list, nil
}

func (u *Usecase) List(ctx context.Context, customerID uuid.UUID, page pagination.Params) (*dtos.BookingList, error) {
	page = page.Normalize()
	bookings, total, err := u.Bookings.OfCustomer(ctx, customerID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	return u.listView(ctx, bookings, total, page)
}

func icsStamp(at time.Time) string {
	return at.UTC().Format("20060102T150405Z")
}

func icsEscape(text string) string {
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n", "\r", "").Replace(text)
}

func (u *Usecase) CalendarFile(ctx context.Context, customerID, id uuid.UUID) ([]byte, string, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, "", err
	}
	summary := icsEscape("AGORA " + booking.Code + " " + booking.WorkshopName)
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//AGORA//Atlas//PT", "BEGIN:VEVENT", "UID:" + booking.ID.String() + "@agora", "DTSTAMP:" + icsStamp(booking.UpdatedAt),
		"DTSTART:" + icsStamp(booking.StartsAt), "DTEND:" + icsStamp(booking.EndsAt), "SUMMARY:" + summary, "END:VEVENT", "END:VCALENDAR", "",
	}
	return []byte(strings.Join(lines, "\r\n")), booking.Code, nil
}

func (u *Usecase) cancelInside(ctx context.Context, booking *entities.Booking, request *entities.ServiceRequest, actor entities.RequestActor, reason string, reasonID *uuid.UUID, now time.Time) error {
	if booking.Status.AtWorkshop() {
		return entities.ErrBookingNotCancellable
	}
	from, err := booking.Move(entities.BookingCancelled, now)
	if err != nil {
		return err
	}
	if err := u.Bookings.Update(ctx, booking); err != nil {
		return err
	}
	change := request.FollowBooking(entities.RequestCancelled, actor, reason, now)
	request.CancelReasonID = reasonID
	if err := u.Requests.Update(ctx, request, change.ExpectedVersion); err != nil {
		return err
	}
	if err := u.Requests.RecordChange(ctx, request.ID, change); err != nil {
		return err
	}
	if request.VoucherID != nil {
		if err := u.Vouchers.Release(ctx, request.ID); err != nil {
			return err
		}
	}
	if request.Prepay && request.PaymentStatus == entities.RequestPaid {
		if err := u.Refunds.RefundPrepaid(ctx, request.ID, "Marcação cancelada"); err != nil {
			return err
		}
	}
	return u.announce(ctx, booking, request.PublicCode, from, string(actor.Type), actor.ID, now)
}

func (u *Usecase) Cancel(ctx context.Context, customerID, id uuid.UUID, in dtos.CancelBookingInput) (*dtos.BookingView, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		locked, err := u.Bookings.FindForUpdate(ctx, booking.ID)
		if err != nil {
			return err
		}
		request, err := u.Requests.FindForUpdate(ctx, locked.RequestID)
		if err != nil {
			return err
		}
		reason, err := u.Cancels.ActiveReason(ctx, in.ReasonID, entities.RejectionScopeCustomerCancel)
		if err != nil {
			return err
		}
		if reason == nil || !reason.AppliesTo(request.CategoryID) {
			return ErrReasonNotValid
		}
		text, err := entities.ComposeRejection([]entities.RejectionReason{*reason}, in.Note)
		if err != nil {
			return err
		}
		booking = locked
		return u.cancelInside(ctx, locked, request, entities.RequestActor{Type: entities.RequestActorCustomer, ID: &customerID}, text, &in.ReasonID, u.now())
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) lockCustomer(ctx context.Context, customerID, id uuid.UUID) (*entities.Booking, *entities.ServiceRequest, error) {
	booking, err := u.Bookings.FindForUpdate(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if booking == nil || booking.CustomerID != customerID {
		return nil, nil, entities.ErrBookingNotFound
	}
	request, err := u.Requests.FindForUpdate(ctx, booking.RequestID)
	if err != nil {
		return nil, nil, err
	}
	if request == nil {
		return nil, nil, entities.ErrBookingNotFound
	}
	return booking, request, nil
}

func (u *Usecase) AnswerExtra(ctx context.Context, customerID, id, extraID uuid.UUID, approve bool) (*dtos.BookingView, error) {
	var booking *entities.Booking
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var request *entities.ServiceRequest
		var err error
		if booking, request, err = u.lockCustomer(ctx, customerID, id); err != nil {
			return err
		}
		now := u.now()
		for i := range booking.Extras {
			extra := &booking.Extras[i]
			if extra.ID != extraID {
				continue
			}
			if err := extra.Answer(approve, now); err != nil {
				return err
			}
			if err := u.Bookings.SaveExtra(ctx, extra); err != nil {
				return err
			}
			if !approve {
				return nil
			}
			tariff, err := u.Pricing.CurrentTariff(ctx, request.ServiceID, request.ZoneID)
			if err != nil {
				return err
			}
			if tariff == nil {
				return ErrServiceNotOffered
			}
			request.AddToFare(extra.AmountCents)
			commission := tariff.CommissionFor(*request.Fare.Cents)
			request.Fare.CommissionCents = &commission
			return u.Requests.Update(ctx, request, request.Touch(now))
		}
		return entities.ErrBookingExtraNotFound
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

// Approve is the step that makes the booking payable: only after it does the
// payment request go out.
func (u *Usecase) Approve(ctx context.Context, customerID, id uuid.UUID) (*dtos.BookingView, error) {
	var booking *entities.Booking
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var request *entities.ServiceRequest
		var err error
		if booking, request, err = u.lockCustomer(ctx, customerID, id); err != nil {
			return err
		}
		if booking.HasPendingExtras() {
			return entities.ErrBookingExtrasPending
		}
		now := u.now()
		from, err := booking.Move(entities.BookingCompleted, now)
		if err != nil {
			return err
		}
		if err := u.Bookings.Update(ctx, booking); err != nil {
			return err
		}
		change := request.FollowBooking(entities.RequestCompleted, entities.RequestActor{Type: entities.RequestActorCustomer, ID: &customerID}, "", now)
		if request.Prepay {
			request.ExtraDueCents = booking.ApprovedExtrasCents()
		}
		if err := u.Requests.Update(ctx, request, change.ExpectedVersion); err != nil {
			return err
		}
		if err := u.Requests.RecordChange(ctx, request.ID, change); err != nil {
			return err
		}
		if request.Prepay && request.ExtraDueCents > 0 {
			if err := u.Payments.SetRequestPayment(ctx, request.ID, request.PaymentMethod, entities.RequestUnpaid); err != nil {
				return err
			}
		}
		if err := u.Refunds.OnPrepaidCompleted(ctx, request, now); err != nil {
			return err
		}
		return u.announce(ctx, booking, request.PublicCode, from, actorCustomer, &customerID, now)
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) OnPrepaid(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	booking, err := u.Bookings.OfRequest(ctx, request.ID)
	if err != nil || booking == nil || booking.Status != entities.BookingAwaitingPayment {
		return err
	}
	from, err := booking.Move(entities.BookingConfirmed, now)
	if err != nil {
		return err
	}
	if err := u.Bookings.Update(ctx, booking); err != nil {
		return err
	}
	return u.announce(ctx, booking, request.PublicCode, from, actorSystem, nil, now)
}

func (u *Usecase) OnPrepayExpired(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	booking, err := u.Bookings.OfRequest(ctx, request.ID)
	if err != nil || booking == nil || booking.Status != entities.BookingAwaitingPayment {
		return err
	}
	return u.cancelInside(ctx, booking, request, entities.SystemRequestActor(), "A referência expirou sem pagamento", nil, now)
}

func parseDay(raw string) (time.Time, error) {
	day, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, ErrBadDate
	}
	return day, nil
}

func (u *Usecase) SetSchedule(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.ScheduleInput) error {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return err
	}
	schedule := &entities.WorkshopSchedule{Capacity: in.Capacity, About: in.About, Warranty: in.Warranty}
	for _, day := range in.Hours {
		schedule.Hours = append(schedule.Hours, entities.WorkshopDayHours{Weekday: day.Weekday, OpenMinute: day.OpenMinute, CloseMinute: day.CloseMinute})
	}
	if err := schedule.Normalize(); err != nil {
		return err
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := u.Bookings.SaveSchedule(ctx, workshopID, schedule); err != nil {
			return err
		}
		return u.audit(ctx, actor, "workshop.schedule.set", "workshop", workshopID.String(), in)
	})
}

func (u *Usecase) AddClosure(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.ClosureInput) error {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return err
	}
	day, err := parseDay(in.Date)
	if err != nil {
		return err
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Bookings.AddClosure(ctx, workshopID, day, strings.TrimSpace(in.Reason)); err != nil {
			return err
		}
		return u.audit(ctx, actor, "workshop.closure.add", "workshop", workshopID.String(), in)
	})
}

func (u *Usecase) RemoveClosure(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, date string) error {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return err
	}
	day, err := parseDay(date)
	if err != nil {
		return err
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Bookings.RemoveClosure(ctx, workshopID, day); err != nil {
			return err
		}
		return u.audit(ctx, actor, "workshop.closure.remove", "workshop", workshopID.String(), map[string]string{"date": date})
	})
}

func (u *Usecase) AddMechanic(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.MechanicInput) (*dtos.MechanicView, error) {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	mechanic, err := entities.NewWorkshopMechanic(workshopID, in.Name, u.now())
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Bookings.AddMechanic(ctx, mechanic); err != nil {
			return err
		}
		return u.audit(ctx, actor, "workshop.mechanic.add", "workshop", workshopID.String(), in)
	})
	if err != nil {
		return nil, err
	}
	return &dtos.MechanicView{ID: mechanic.ID, Name: mechanic.Name}, nil
}

func (u *Usecase) Mechanics(ctx context.Context, organizationID, workshopID uuid.UUID) (*dtos.MechanicList, error) {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	mechanics, err := u.Bookings.Mechanics(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	list := &dtos.MechanicList{Items: make([]dtos.MechanicView, 0, len(mechanics))}
	for i := range mechanics {
		list.Items = append(list.Items, dtos.MechanicView{ID: mechanics[i].ID, Name: mechanics[i].Name})
	}
	return list, nil
}

func (u *Usecase) RemoveMechanic(ctx context.Context, actor entities.Actor, organizationID, workshopID, id uuid.UUID) error {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return err
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		found, err := u.Bookings.DeactivateMechanic(ctx, workshopID, id)
		if err != nil {
			return err
		}
		if !found {
			return entities.ErrMechanicNotFound
		}
		return u.audit(ctx, actor, "workshop.mechanic.remove", "workshop", workshopID.String(), map[string]string{"mechanic_id": id.String()})
	})
}

func (u *Usecase) WorkshopBookings(ctx context.Context, organizationID, workshopID uuid.UUID, status string, page pagination.Params) (*dtos.BookingList, error) {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	page = page.Normalize()
	bookings, total, err := u.Bookings.OfWorkshop(ctx, workshopID, status, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	return u.listView(ctx, bookings, total, page)
}

func (u *Usecase) lockWorkshopBooking(ctx context.Context, organizationID, id uuid.UUID) (*entities.Booking, *entities.ServiceRequest, error) {
	booking, err := u.Bookings.FindForUpdate(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if booking == nil || booking.OrganizationID != organizationID {
		return nil, nil, entities.ErrBookingNotFound
	}
	request, err := u.Requests.FindForUpdate(ctx, booking.RequestID)
	if err != nil {
		return nil, nil, err
	}
	if request == nil {
		return nil, nil, entities.ErrBookingNotFound
	}
	return booking, request, nil
}

func (u *Usecase) Move(ctx context.Context, actor entities.Actor, organizationID, id uuid.UUID, in dtos.MoveBookingInput) (*dtos.BookingView, error) {
	var booking *entities.Booking
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var request *entities.ServiceRequest
		var err error
		if booking, request, err = u.lockWorkshopBooking(ctx, organizationID, id); err != nil {
			return err
		}
		to := entities.BookingStatus(in.Status)
		if to == entities.BookingReady && booking.HasPendingExtras() {
			return entities.ErrBookingExtrasPending
		}
		now := u.now()
		from, err := booking.Move(to, now)
		if err != nil {
			return err
		}
		if err := u.Bookings.Update(ctx, booking); err != nil {
			return err
		}
		if to == entities.BookingVehicleReceived {
			change := request.FollowBooking(entities.RequestInProgress, entities.RequestActor{Type: entities.RequestActorOperator, ID: actor.ID}, "", now)
			if err := u.Requests.Update(ctx, request, change.ExpectedVersion); err != nil {
				return err
			}
			if err := u.Requests.RecordChange(ctx, request.ID, change); err != nil {
				return err
			}
		}
		return u.announce(ctx, booking, request.PublicCode, from, actorWorkshop, actor.ID, now)
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) Assign(ctx context.Context, actor entities.Actor, organizationID, id uuid.UUID, in dtos.AssignInput) (*dtos.BookingView, error) {
	var booking *entities.Booking
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if booking, _, err = u.lockWorkshopBooking(ctx, organizationID, id); err != nil {
			return err
		}
		if in.MechanicID != nil {
			mechanic, err := u.Bookings.Mechanic(ctx, *in.MechanicID)
			if err != nil {
				return err
			}
			if mechanic == nil || mechanic.WorkshopID != booking.WorkshopID || !mechanic.IsActive {
				return entities.ErrMechanicNotFound
			}
			booking.MechanicID, booking.MechanicName = &mechanic.ID, mechanic.Name
		}
		if in.ExpectedReadyAt != nil {
			booking.ExpectedReadyAt = in.ExpectedReadyAt
		}
		booking.UpdatedAt = u.now()
		if err := u.Bookings.Update(ctx, booking); err != nil {
			return err
		}
		return u.audit(ctx, actor, "booking.assign", "booking", id.String(), in)
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) AddExtra(ctx context.Context, actor entities.Actor, organizationID, id uuid.UUID, in dtos.ExtraInput) (*dtos.BookingView, error) {
	var booking *entities.Booking
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if booking, _, err = u.lockWorkshopBooking(ctx, organizationID, id); err != nil {
			return err
		}
		if booking.Status != entities.BookingVehicleReceived && booking.Status != entities.BookingInService {
			return entities.ErrBookingTransition
		}
		now := u.now()
		extra, err := entities.NewBookingExtra(booking.ID, in.Description, in.AmountCents, now)
		if err != nil {
			return err
		}
		if err := u.Bookings.AddExtra(ctx, extra); err != nil {
			return err
		}
		booking.Extras = append(booking.Extras, *extra)
		if err := u.publish(ctx, events.BookingExtraV1{BookingID: booking.ID, RequestID: booking.RequestID, CustomerID: booking.CustomerID, ExtraID: extra.ID, Description: extra.Description, AmountCents: extra.AmountCents, OccurredAt: now}); err != nil {
			return err
		}
		return u.audit(ctx, actor, "booking.extra.add", "booking", id.String(), in)
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) WorkshopCancel(ctx context.Context, actor entities.Actor, organizationID, id uuid.UUID, reason string) (*dtos.BookingView, error) {
	var booking *entities.Booking
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonNotValid
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var request *entities.ServiceRequest
		var err error
		if booking, request, err = u.lockWorkshopBooking(ctx, organizationID, id); err != nil {
			return err
		}
		return u.cancelInside(ctx, booking, request, entities.RequestActor{Type: entities.RequestActorOperator, ID: actor.ID}, reason, nil, u.now())
	})
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) sendMessage(ctx context.Context, booking *entities.Booking, senderID uuid.UUID, sender, body string) (*dtos.MessageView, error) {
	if !booking.Status.AtWorkshop() && booking.Status != entities.BookingConfirmed {
		return nil, ErrChatClosed
	}
	text, err := entities.NormalizeMessage(body)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	message := &entities.RequestMessage{ID: id, RequestID: booking.RequestID, SenderID: senderID, SenderType: sender, Body: text, CreatedAt: u.now()}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Live.AddMessage(ctx, message); err != nil {
			return err
		}
		if sender != actorWorkshop {
			return nil
		}
		return u.publish(ctx, events.RequestMessageV1{MessageID: id, RequestID: booking.RequestID, SenderType: sender, RecipientID: booking.CustomerID, Body: text, OccurredAt: message.CreatedAt})
	})
	if err != nil {
		return nil, err
	}
	return &dtos.MessageView{ID: id, Sender: sender, Body: text, CreatedAt: message.CreatedAt}, nil
}

func (u *Usecase) messages(ctx context.Context, booking *entities.Booking, page pagination.Params) (*dtos.MessageList, error) {
	page = page.Normalize()
	messages, err := u.Live.Messages(ctx, booking.RequestID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.MessageList{Items: make([]dtos.MessageView, 0, len(messages))}
	for i := range messages {
		list.Items = append(list.Items, dtos.MessageView{ID: messages[i].ID, Sender: messages[i].SenderType, Body: messages[i].Body, CreatedAt: messages[i].CreatedAt})
	}
	return list, nil
}

func (u *Usecase) CustomerSend(ctx context.Context, customerID, id uuid.UUID, in dtos.MessageInput) (*dtos.MessageView, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	return u.sendMessage(ctx, booking, customerID, actorCustomer, in.Body)
}

func (u *Usecase) CustomerMessages(ctx context.Context, customerID, id uuid.UUID, page pagination.Params) (*dtos.MessageList, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	return u.messages(ctx, booking, page)
}

func (u *Usecase) workshopBooking(ctx context.Context, organizationID, id uuid.UUID) (*entities.Booking, error) {
	booking, err := u.Bookings.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if booking == nil || booking.OrganizationID != organizationID {
		return nil, entities.ErrBookingNotFound
	}
	return booking, nil
}

func (u *Usecase) WorkshopSend(ctx context.Context, actor entities.Actor, organizationID, id uuid.UUID, in dtos.MessageInput) (*dtos.MessageView, error) {
	booking, err := u.workshopBooking(ctx, organizationID, id)
	if err != nil {
		return nil, err
	}
	if actor.ID == nil {
		return nil, entities.ErrBookingNotFound
	}
	return u.sendMessage(ctx, booking, *actor.ID, actorWorkshop, in.Body)
}

func (u *Usecase) WorkshopMessages(ctx context.Context, organizationID, id uuid.UUID, page pagination.Params) (*dtos.MessageList, error) {
	booking, err := u.workshopBooking(ctx, organizationID, id)
	if err != nil {
		return nil, err
	}
	return u.messages(ctx, booking, page)
}

func (u *Usecase) GetForWorkshop(ctx context.Context, organizationID, id uuid.UUID) (*dtos.BookingView, error) {
	booking, err := u.workshopBooking(ctx, organizationID, id)
	if err != nil {
		return nil, err
	}
	return u.viewOf(ctx, booking)
}

func (u *Usecase) Call(ctx context.Context, customerID, id uuid.UUID) (*dtos.CallView, error) {
	booking, err := u.customerBooking(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	if !booking.Status.AtWorkshop() && booking.Status != entities.BookingConfirmed {
		return nil, entities.ErrCallNotAvailable
	}
	if !u.Calls.Enabled() {
		return nil, services.ErrCallsUnavailable
	}
	workshop, err := u.Fleet.FindWorkshop(ctx, booking.WorkshopID)
	if err != nil {
		return nil, err
	}
	if workshop == nil || workshop.Phone == "" {
		return nil, entities.ErrCallNotAvailable
	}
	_, from, err := u.Devices.Contact(ctx, customerID)
	if err != nil {
		return nil, err
	}
	call, err := u.Calls.Connect(ctx, from, workshop.Phone, booking.Code)
	if err != nil {
		return nil, fmt.Errorf("connect call: %w", err)
	}
	view := &dtos.CallView{ProxyNumber: call.ProxyNumber}
	if call.ExpiresAt != nil {
		view.ExpiresAt = *call.ExpiresAt
	}
	return view, nil
}
