package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 11, 9, hour, minute, 0, 0, time.UTC)
}

func monday() *entities.WorkshopSchedule {
	return &entities.WorkshopSchedule{Capacity: 2, Hours: []entities.WorkshopDayHours{{Weekday: 1, OpenMinute: 8 * 60, CloseMinute: 18 * 60}}}
}

func TestOverlappingCountsTheBusiestInstantAndNotEveryOverlap(t *testing.T) {
	morning := entities.BookedSlot{StartsAt: at(8, 0), EndsAt: at(9, 0)}
	later := entities.BookedSlot{StartsAt: at(9, 30), EndsAt: at(10, 0)}
	assert.Equal(t, 1, entities.Overlapping([]entities.BookedSlot{morning, later}, at(8, 0), at(10, 0)), "they never run at the same time")
	both := entities.BookedSlot{StartsAt: at(8, 30), EndsAt: at(9, 30)}
	assert.Equal(t, 2, entities.Overlapping([]entities.BookedSlot{morning, both}, at(8, 0), at(10, 0)))
	assert.Equal(t, 0, entities.Overlapping([]entities.BookedSlot{morning}, at(9, 0), at(10, 0)), "the end of one is free for the next")
}

func TestFreeSlotsNeedTheWholeDurationInsideTheHoursAndTheCapacity(t *testing.T) {
	schedule := monday()
	slots, status := schedule.FreeSlots(at(0, 0), nil, 120, 60, at(0, 0))
	assert.Equal(t, entities.DayFree, status)
	require.Len(t, slots, 9)
	assert.Equal(t, at(16, 0), slots[len(slots)-1].StartsAt, "at 17:00 two hours do not fit")

	_, status = schedule.FreeSlots(at(0, 0).AddDate(0, 0, 1), nil, 120, 60, at(0, 0))
	assert.Equal(t, entities.DayClosed, status, "without hours on this weekday")

	schedule.Capacity = 1
	full := []entities.BookedSlot{{StartsAt: at(8, 0), EndsAt: at(18, 0)}}
	slots, status = schedule.FreeSlots(at(0, 0), full, 120, 60, at(0, 0))
	assert.Equal(t, entities.DayFull, status)
	assert.Empty(t, slots)

	slots, _ = monday().FreeSlots(at(0, 0), nil, 60, 60, at(15, 30))
	assert.Equal(t, at(16, 0), slots[0].StartsAt, "what passed is not offered")
}

func TestCheckSlotRefusesWhatTheCalendarDoesNotOffer(t *testing.T) {
	schedule := monday()
	now := at(0, 0)
	require.NoError(t, schedule.CheckSlot(at(8, 30), nil, 60, 30, now))
	assert.ErrorIs(t, schedule.CheckSlot(at(8, 45), nil, 60, 30, now), entities.ErrBookingSlotInvalid, "outside the grid")
	assert.ErrorIs(t, schedule.CheckSlot(at(7, 30), nil, 60, 30, now), entities.ErrBookingSlotInvalid, "before opening")
	assert.ErrorIs(t, schedule.CheckSlot(at(17, 30), nil, 60, 30, now), entities.ErrBookingSlotInvalid, "ends after closing")
	assert.ErrorIs(t, schedule.CheckSlot(at(8, 30), nil, 60, 30, at(9, 0)), entities.ErrBookingSlotInvalid, "in the past")
	busy := []entities.BookedSlot{{StartsAt: at(8, 0), EndsAt: at(10, 0)}, {StartsAt: at(8, 0), EndsAt: at(10, 0)}}
	assert.ErrorIs(t, schedule.CheckSlot(at(9, 0), busy, 60, 30, now), entities.ErrBookingSlotFull)
}

func TestOpenUntilFollowsTheHours(t *testing.T) {
	schedule := monday()
	until, open := schedule.OpenUntil(at(10, 0))
	assert.True(t, open)
	assert.Equal(t, at(18, 0), until)
	_, open = schedule.OpenUntil(at(18, 0))
	assert.False(t, open)
	_, open = schedule.OpenUntil(at(10, 0).AddDate(0, 0, 1))
	assert.False(t, open)
}

func TestScheduleValidatesItsLimits(t *testing.T) {
	assert.Error(t, (&entities.WorkshopSchedule{Capacity: 0}).Normalize())
	assert.Error(t, (&entities.WorkshopSchedule{Capacity: 501}).Normalize())
	assert.Error(t, (&entities.WorkshopSchedule{Capacity: 1, Hours: []entities.WorkshopDayHours{{Weekday: 7, OpenMinute: 0, CloseMinute: 10}}}).Normalize())
	assert.Error(t, (&entities.WorkshopSchedule{Capacity: 1, Hours: []entities.WorkshopDayHours{{Weekday: 1, OpenMinute: 10, CloseMinute: 10}}}).Normalize())
	assert.Error(t, (&entities.WorkshopSchedule{Capacity: 1, Hours: []entities.WorkshopDayHours{{Weekday: 1, OpenMinute: 0, CloseMinute: 10}, {Weekday: 1, OpenMinute: 20, CloseMinute: 30}}}).Normalize(), "the same day twice")
}

func TestBookingMovesOnlyOneStepAtATime(t *testing.T) {
	booking := &entities.Booking{Status: entities.BookingConfirmed}
	_, err := booking.Move(entities.BookingReady, at(9, 0))
	assert.ErrorIs(t, err, entities.ErrBookingTransition)
	for _, to := range []entities.BookingStatus{entities.BookingVehicleReceived, entities.BookingInService, entities.BookingReady, entities.BookingCompleted} {
		_, err := booking.Move(to, at(9, 0))
		require.NoError(t, err)
	}
	_, err = booking.Move(entities.BookingCancelled, at(9, 0))
	assert.ErrorIs(t, err, entities.ErrBookingTransition)
}

func TestOnlyApprovedExtrasCount(t *testing.T) {
	booking := &entities.Booking{ID: uuid.New()}
	approved, err := entities.NewBookingExtra(booking.ID, "Pastilhas", 1000, at(9, 0))
	require.NoError(t, err)
	declined, err := entities.NewBookingExtra(booking.ID, "Filtro", 500, at(9, 0))
	require.NoError(t, err)
	booking.Extras = []entities.BookingExtra{*approved, *declined}
	assert.True(t, booking.HasPendingExtras())
	assert.Zero(t, booking.ApprovedExtrasCents(), "not answered is not in the total")
	require.NoError(t, booking.Extras[0].Answer(true, at(9, 0)))
	require.NoError(t, booking.Extras[1].Answer(false, at(9, 0)))
	assert.False(t, booking.HasPendingExtras())
	assert.EqualValues(t, 1000, booking.ApprovedExtrasCents())
	assert.ErrorIs(t, booking.Extras[1].Answer(true, at(9, 0)), entities.ErrBookingExtraAnswered)
	_, err = entities.NewBookingExtra(booking.ID, "", 1, at(9, 0))
	assert.Error(t, err)
	_, err = entities.NewBookingExtra(booking.ID, "X", 0, at(9, 0))
	assert.Error(t, err)
}

func TestPrepaidRequestIsDueBeforeTheServiceAndOnlyForExtrasAfter(t *testing.T) {
	fare := int64(1000)
	request := &entities.ServiceRequest{Status: entities.RequestScheduled, Prepay: true, Fare: entities.RequestFare{Cents: &fare}, DiscountCents: 100}
	assert.EqualValues(t, 900, request.AmountDueCents())
	assert.Equal(t, entities.PaymentKindFare, request.DueKind())
	request.Status = entities.RequestInProgress
	assert.Zero(t, request.AmountDueCents())
	request.Status, request.ExtraDueCents = entities.RequestCompleted, 300
	assert.EqualValues(t, 300, request.AmountDueCents())
	assert.Equal(t, entities.PaymentKindExtra, request.DueKind())

	normal := &entities.ServiceRequest{Status: entities.RequestScheduled, Fare: entities.RequestFare{Cents: &fare}}
	assert.Zero(t, normal.AmountDueCents(), "without prepayment a booking is paid only when completed")
}

func TestPromotionCanBeTiedToAWorkshopAndAService(t *testing.T) {
	workshop, service := uuid.New(), uuid.New()
	voucher := &entities.Voucher{WorkshopID: &workshop, WorkshopService: &service}
	assert.True(t, voucher.AppliesToBooking(workshop, []uuid.UUID{uuid.New(), service}))
	assert.False(t, voucher.AppliesToBooking(workshop, []uuid.UUID{uuid.New()}))
	assert.False(t, voucher.AppliesToBooking(uuid.New(), []uuid.UUID{service}))
}

func TestWorkshopLedgerUsesItsOwnAccount(t *testing.T) {
	workshop, customer := uuid.New(), uuid.New()
	legs := entities.Settlement{FareCents: 1000, CommissionCents: 100, PaidCents: 1000, Method: entities.PaymentGPO, ProviderID: &workshop, PayeeType: entities.AccountWorkshop, CustomerID: customer}.Legs()
	found := false
	total := int64(0)
	for _, leg := range legs {
		total += leg.AmountCents
		if leg.AccountType == entities.AccountWorkshop {
			found = true
			assert.EqualValues(t, 900, leg.AmountCents)
		}
		assert.NotEqual(t, entities.AccountProvider, leg.AccountType)
	}
	assert.True(t, found)
	assert.Zero(t, total)
}
