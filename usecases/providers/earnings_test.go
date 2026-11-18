package providers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/usecases/providers"
)

func TestEarningsRangeStartsTheDayTheWeekAndTheMonthInLuanda(t *testing.T) {
	luanda := time.FixedZone("WAT", 3600)
	// Wednesday 18 Nov 2026, 23:30 UTC is already Thursday 19 in Luanda
	at := time.Date(2026, 11, 18, 23, 30, 0, 0, time.UTC)

	from, to := providers.EarningsRange(providers.EarningsDay, at)
	assert.True(t, from.Equal(time.Date(2026, 11, 19, 0, 0, 0, 0, luanda)), from.String())
	assert.True(t, to.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, luanda)), to.String())

	from, to = providers.EarningsRange(providers.EarningsWeek, at)
	assert.True(t, from.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, luanda)), "the week starts on Monday: %s", from)
	assert.True(t, to.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, luanda)), to.String())

	sunday := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	from, _ = providers.EarningsRange(providers.EarningsWeek, sunday)
	assert.True(t, from.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, luanda)), "Sunday closes the week: %s", from)

	from, to = providers.EarningsRange(providers.EarningsMonth, time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))
	assert.True(t, from.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, luanda)), "already January in Luanda: %s", from)
	assert.True(t, to.Equal(time.Date(2027, 2, 1, 0, 0, 0, 0, luanda)), to.String())
}

func TestEarningsPeriodAcceptsOnlyDayWeekAndMonth(t *testing.T) {
	for _, period := range []string{"day", "week", "month"} {
		assert.True(t, providers.EarningsPeriod(period).Valid(), period)
	}
	for _, period := range []string{"", "year", "DAY"} {
		assert.False(t, providers.EarningsPeriod(period).Valid(), period)
	}
}
