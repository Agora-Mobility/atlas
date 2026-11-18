package providers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/usecases/providers/dtos"
)

// EarningsTimezone is where the days, weeks and months of the earnings start.
// Angola has no daylight saving time, so the fixed zone matches Africa/Luanda
// even where the image has no tzdata; the database gets the name.
const EarningsTimezone = "Africa/Luanda"

var earningsZone = func() *time.Location {
	if zone, err := time.LoadLocation(EarningsTimezone); err == nil {
		return zone
	}
	return time.FixedZone("WAT", 3600)
}()

// EarningsPeriod is the span of the earnings screen of the provider app.
type EarningsPeriod string

const (
	EarningsDay   EarningsPeriod = "day"
	EarningsWeek  EarningsPeriod = "week"
	EarningsMonth EarningsPeriod = "month"
)

func (p EarningsPeriod) Valid() bool {
	return p == EarningsDay || p == EarningsWeek || p == EarningsMonth
}

// EarningsRange is the local [from, to) of the period that holds date: the day,
// the week from Monday or the calendar month.
func EarningsRange(period EarningsPeriod, date time.Time) (time.Time, time.Time) {
	local := date.In(earningsZone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, earningsZone)
	switch period {
	case EarningsWeek:
		start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
		return start, start.AddDate(0, 0, 7)
	case EarningsMonth:
		start = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, earningsZone)
		return start, start.AddDate(0, 1, 0)
	default:
		return start, start.AddDate(0, 0, 1)
	}
}

// Earnings is what the provider earned in the day, week or month that holds
// date (today when nil), with one entry per day, also the days without work,
// and the balance still to settle with AGORA.
func (u *Usecase) Earnings(ctx context.Context, providerID uuid.UUID, period EarningsPeriod, date *time.Time) (*dtos.ProviderEarningsView, error) {
	if _, err := u.existingProvider(ctx, providerID); err != nil {
		return nil, err
	}
	at := u.now()
	if date != nil {
		at = *date
	}
	from, to := EarningsRange(period, at)
	daily, err := u.ActivityStats.Daily(ctx, providerID, from, to, EarningsTimezone)
	if err != nil {
		return nil, err
	}
	money, err := u.ActivityStats.Money(ctx, []uuid.UUID{providerID}, &from, &to)
	if err != nil {
		return nil, err
	}

	byDay := make(map[string]dtos.ProviderEarningsDay, len(daily))
	for _, d := range daily {
		key := d.Day.Format(time.DateOnly)
		byDay[key] = dtos.ProviderEarningsDay{
			Date: key, GrossCents: d.GrossCents, EarnedCents: d.EarnedCents,
			CompletedRequests: d.CompletedRequests, CancelledRequests: d.CancelledRequests,
		}
	}
	view := &dtos.ProviderEarningsView{
		Period: string(period), From: from, To: to,
		BalanceCents: money[providerID].BalanceCents, Days: []dtos.ProviderEarningsDay{},
	}
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		entry, ok := byDay[key]
		if !ok {
			entry = dtos.ProviderEarningsDay{Date: key}
		}
		view.GrossCents += entry.GrossCents
		view.EarnedCents += entry.EarnedCents
		view.CompletedRequests += entry.CompletedRequests
		view.CancelledRequests += entry.CancelledRequests
		view.Days = append(view.Days, entry)
	}
	return view, nil
}
