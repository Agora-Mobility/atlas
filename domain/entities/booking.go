package entities

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

type BookingStatus string

const (
	BookingAwaitingPayment BookingStatus = "awaiting_payment"
	BookingConfirmed       BookingStatus = "confirmed"
	BookingVehicleReceived BookingStatus = "vehicle_received"
	BookingInService       BookingStatus = "in_service"
	BookingReady           BookingStatus = "ready"
	BookingCompleted       BookingStatus = "completed"
	BookingCancelled       BookingStatus = "cancelled"

	maxBookingDescription = 2000
	maxExtraDescription   = 300
	maxMileageKm          = 5_000_000
	maxWorkshopCapacity   = 500
	maxWorkshopAbout      = 4000
	maxBookingServices    = 20
	minutesPerDay         = 1440
)

type BookingExtraStatus string

const (
	ExtraPending  BookingExtraStatus = "pending"
	ExtraApproved BookingExtraStatus = "approved"
	ExtraDeclined BookingExtraStatus = "declined"
)

type DayStatus string

const (
	DayFree   DayStatus = "free"
	DayClosed DayStatus = "closed"
	DayFull   DayStatus = "full"
)

var (
	ErrBookingNotFound        = errors.New("booking not found")
	ErrBookingTransition      = errors.New("this booking cannot move to that state")
	ErrBookingSlotFull        = errors.New("this slot is no longer free")
	ErrBookingSlotInvalid     = errors.New("the workshop does not take bookings at this time")
	ErrBookingExtrasPending   = errors.New("the customer still has to answer an extra")
	ErrBookingExtraNotFound   = errors.New("extra not found")
	ErrBookingExtraAnswered   = errors.New("this extra was already answered")
	ErrBookingNotCancellable  = errors.New("the vehicle is already at the workshop")
	ErrBookingManaged         = errors.New("this request is managed through its booking")
	ErrBookingOnly            = errors.New("this service only works through a booking")
	ErrHomeServiceUnavailable = errors.New("this workshop does not offer home service")
	ErrMechanicNotFound       = errors.New("mechanic not found")
	ErrWorkshopServiceInvalid = errors.New("a service does not exist in this workshop")
	ErrWorkshopUnavailable    = errors.New("this workshop is not available")
)

var bookingFlow = map[BookingStatus][]BookingStatus{
	BookingAwaitingPayment: {BookingConfirmed, BookingCancelled},
	BookingConfirmed:       {BookingVehicleReceived, BookingCancelled},
	BookingVehicleReceived: {BookingInService},
	BookingInService:       {BookingReady},
	BookingReady:           {BookingCompleted},
}

func (s BookingStatus) IsValid() bool {
	switch s {
	case BookingAwaitingPayment, BookingConfirmed, BookingVehicleReceived, BookingInService, BookingReady, BookingCompleted, BookingCancelled:
		return true
	}
	return false
}

func (s BookingStatus) AtWorkshop() bool {
	return s == BookingVehicleReceived || s == BookingInService || s == BookingReady
}

type WorkshopDayHours struct {
	Weekday     int
	OpenMinute  int
	CloseMinute int
}

func (h WorkshopDayHours) Validate() error {
	if h.Weekday < 0 || h.Weekday > 6 {
		return validationError("hours.weekday", "O dia da semana tem de estar entre 0 e 6.")
	}
	if h.OpenMinute < 0 || h.CloseMinute > minutesPerDay || h.CloseMinute <= h.OpenMinute {
		return validationError("hours", "O horário não é válido.")
	}
	return nil
}

type WorkshopSchedule struct {
	Capacity int
	About    string
	Warranty string
	Hours    []WorkshopDayHours
}

func (s *WorkshopSchedule) Normalize() error {
	if s.Capacity < 1 || s.Capacity > maxWorkshopCapacity {
		return validationError("capacity", "A capacidade tem de estar entre 1 e 500.")
	}
	s.About, s.Warranty = strings.TrimSpace(s.About), strings.TrimSpace(s.Warranty)
	if len([]rune(s.About)) > maxWorkshopAbout || len([]rune(s.Warranty)) > maxWorkshopAbout {
		return validationError("about", "O texto tem no máximo 4000 caracteres.")
	}
	seen := map[int]bool{}
	for _, day := range s.Hours {
		if err := day.Validate(); err != nil {
			return err
		}
		if seen[day.Weekday] {
			return validationError("hours", "Indica cada dia uma só vez.")
		}
		seen[day.Weekday] = true
	}
	return nil
}

func (s *WorkshopSchedule) HoursOf(weekday time.Weekday) (WorkshopDayHours, bool) {
	for _, day := range s.Hours {
		if day.Weekday == int(weekday) {
			return day, true
		}
	}
	return WorkshopDayHours{}, false
}

func ClockOf(day time.Time, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, minute, 0, 0, day.Location())
}

// OpenUntil tells whether the workshop is open at this moment, and until when.
func (s *WorkshopSchedule) OpenUntil(now time.Time) (time.Time, bool) {
	hours, known := s.HoursOf(now.Weekday())
	if !known {
		return time.Time{}, false
	}
	opens, closes := ClockOf(now, hours.OpenMinute), ClockOf(now, hours.CloseMinute)
	if now.Before(opens) || !now.Before(closes) {
		return time.Time{}, false
	}
	return closes, true
}

type BookedSlot struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// Overlapping counts the bookings on the busiest instant of the interval. Two
// bookings that touch the interval at different moments can share a bay, so the
// count looks at each start and not at every booking that overlaps.
func Overlapping(booked []BookedSlot, start, end time.Time) int {
	peak := 0
	moments := []time.Time{start}
	for _, slot := range booked {
		if slot.StartsAt.After(start) && slot.StartsAt.Before(end) {
			moments = append(moments, slot.StartsAt)
		}
	}
	for _, moment := range moments {
		count := 0
		for _, slot := range booked {
			if !moment.Before(slot.StartsAt) && moment.Before(slot.EndsAt) {
				count++
			}
		}
		peak = max(peak, count)
	}
	return peak
}

type CalendarSlot struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// FreeSlots lists the starts where the whole job fits inside the opening hours
// without going over the capacity.
func (s *WorkshopSchedule) FreeSlots(day time.Time, booked []BookedSlot, durationMinutes, stepMinutes int, now time.Time) ([]CalendarSlot, DayStatus) {
	hours, open := s.HoursOf(day.Weekday())
	if !open || durationMinutes <= 0 || stepMinutes <= 0 {
		return nil, DayClosed
	}
	out := []CalendarSlot{}
	for minute := hours.OpenMinute; minute+durationMinutes <= hours.CloseMinute; minute += stepMinutes {
		start := ClockOf(day, minute)
		end := start.Add(time.Duration(durationMinutes) * time.Minute)
		if start.Before(now) || Overlapping(booked, start, end) >= s.Capacity {
			continue
		}
		out = append(out, CalendarSlot{StartsAt: start, EndsAt: end})
	}
	if len(out) == 0 {
		return out, DayFull
	}
	return out, DayFree
}

// CheckSlot accepts a start only if it falls on the grid of the calendar.
func (s *WorkshopSchedule) CheckSlot(start time.Time, booked []BookedSlot, durationMinutes, stepMinutes int, now time.Time) error {
	hours, open := s.HoursOf(start.Weekday())
	if !open || durationMinutes <= 0 || stepMinutes <= 0 || !start.After(now) || start.Second() != 0 || start.Nanosecond() != 0 {
		return ErrBookingSlotInvalid
	}
	minute := start.Hour()*60 + start.Minute()
	if minute < hours.OpenMinute || minute+durationMinutes > hours.CloseMinute || (minute-hours.OpenMinute)%stepMinutes != 0 {
		return ErrBookingSlotInvalid
	}
	if Overlapping(booked, start, start.Add(time.Duration(durationMinutes)*time.Minute)) >= s.Capacity {
		return ErrBookingSlotFull
	}
	return nil
}

type BookingItem struct {
	ID              uuid.UUID
	ServiceID       uuid.UUID
	Name            string
	PriceCents      int64
	DurationMinutes int
}

type BookingExtra struct {
	ID          uuid.UUID
	BookingID   uuid.UUID
	Description string
	AmountCents int64
	Status      BookingExtraStatus
	DecidedAt   *time.Time
	CreatedAt   time.Time
}

func NewBookingExtra(bookingID uuid.UUID, description string, amountCents int64, now time.Time) (*BookingExtra, error) {
	description = strings.TrimSpace(description)
	if description == "" || len([]rune(description)) > maxExtraDescription {
		return nil, validationError("description", "A descrição é obrigatória e tem no máximo 300 caracteres.")
	}
	if amountCents <= 0 || amountCents > MaxFeeCents {
		return nil, validationError("amount_cents", "O valor não é válido.")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &BookingExtra{ID: id, BookingID: bookingID, Description: description, AmountCents: amountCents, Status: ExtraPending, CreatedAt: now}, nil
}

func (e *BookingExtra) Answer(approve bool, now time.Time) error {
	if e.Status != ExtraPending {
		return ErrBookingExtraAnswered
	}
	e.Status = ExtraDeclined
	if approve {
		e.Status = ExtraApproved
	}
	e.DecidedAt = &now
	return nil
}

type Booking struct {
	ID              uuid.UUID
	RequestID       uuid.UUID
	Code            string
	WorkshopID      uuid.UUID
	WorkshopName    string
	OrganizationID  uuid.UUID
	CustomerID      uuid.UUID
	VehicleID       uuid.UUID
	MechanicID      *uuid.UUID
	MechanicName    string
	Status          BookingStatus
	AtHome          bool
	HomeFeeCents    int64
	ServicesCents   int64
	StartsAt        time.Time
	EndsAt          time.Time
	ExpectedReadyAt *time.Time
	Description     string
	MileageKm       *int
	Items           []BookingItem
	Extras          []BookingExtra
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (b *Booking) Validate() error {
	b.Description = strings.TrimSpace(b.Description)
	if len([]rune(b.Description)) > maxBookingDescription {
		return validationError("description", "A descrição tem no máximo 2000 caracteres.")
	}
	if b.MileageKm != nil && (*b.MileageKm < 0 || *b.MileageKm > maxMileageKm) {
		return validationError("mileage_km", "A quilometragem não é válida.")
	}
	if len(b.Items) == 0 || len(b.Items) > maxBookingServices {
		return validationError("service_ids", "Escolhe entre 1 e 20 serviços.")
	}
	return nil
}

func TotalDuration(services []WorkshopService) int {
	total := 0
	for i := range services {
		total += services[i].DurationMinutes
	}
	return total
}

func (b *Booking) Move(to BookingStatus, now time.Time) (BookingStatus, error) {
	from := b.Status
	if !slices.Contains(bookingFlow[from], to) {
		return from, ErrBookingTransition
	}
	b.Status, b.UpdatedAt = to, now
	return from, nil
}

func (b *Booking) HasPendingExtras() bool {
	return slices.ContainsFunc(b.Extras, func(extra BookingExtra) bool { return extra.Status == ExtraPending })
}

// ApprovedExtrasCents is the only part of the extras that reaches the total: a
// declined extra never counts.
func (b *Booking) ApprovedExtrasCents() int64 {
	total := int64(0)
	for i := range b.Extras {
		if b.Extras[i].Status == ExtraApproved {
			total += b.Extras[i].AmountCents
		}
	}
	return total
}

func (b *Booking) BaseCents() int64 {
	return b.ServicesCents + b.HomeFeeCents
}

type WorkshopMechanic struct {
	ID         uuid.UUID
	WorkshopID uuid.UUID
	Name       string
	IsActive   bool
	CreatedAt  time.Time
}

func NewWorkshopMechanic(workshopID uuid.UUID, name string, now time.Time) (*WorkshopMechanic, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxWorkshopTextLength {
		return nil, validationError("name", "O nome é obrigatório e tem no máximo 120 caracteres.")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &WorkshopMechanic{ID: id, WorkshopID: workshopID, Name: name, IsActive: true, CreatedAt: now}, nil
}
