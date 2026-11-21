package dispatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func TestArrivalIsTheWaitForTheCarPlusTheTrip(t *testing.T) {
	economy, comfort, idle := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 11, 21, 13, 43, 0, 0, time.FixedZone("WAT", 3600))
	items := []dtos.FareEstimateView{{ServiceID: economy, DurationS: 1080}, {ServiceID: comfort, DurationS: 1080}, {ServiceID: idle, DurationS: 1080}}

	applyArrival(items, map[uuid.UUID]int{economy: 240, comfort: 420}, nil, true, now)

	require.NotNil(t, items[0].PickupETAS)
	assert.Equal(t, 240, *items[0].PickupETAS)
	assert.Equal(t, time.Date(2026, 11, 21, 13, 5, 0, 0, time.UTC), *items[0].ArrivalAt, "13:43 local + 4 min + 18 min, in UTC")
	assert.Equal(t, 420, *items[1].PickupETAS)
	assert.Nil(t, items[2].PickupETAS, "no free car of the service near")
	assert.Nil(t, items[2].ArrivalAt, "the arrival is not guessed without the wait")
}

func TestScheduledTripArrivesAfterTheTripFromTheScheduledTime(t *testing.T) {
	service := uuid.New()
	scheduled := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	items := []dtos.FareEstimateView{{ServiceID: service, DurationS: 900}, {ServiceID: service, DurationS: 900, Removal: &entities.RemovalQuote{}}}

	applyArrival(items, nil, &scheduled, true, scheduled.Add(-24*time.Hour))

	assert.Nil(t, items[0].PickupETAS, "the cars near now say nothing about tomorrow")
	assert.Equal(t, scheduled.Add(15*time.Minute), *items[0].ArrivalAt)
	assert.Nil(t, items[1].ArrivalAt, "a removal takes the loading, not the drive")
}

func TestWithoutDestinationOnlyTheWaitIsKnown(t *testing.T) {
	service := uuid.New()
	items := []dtos.FareEstimateView{{ServiceID: service}}

	applyArrival(items, map[uuid.UUID]int{service: 180}, nil, false, time.Now())

	assert.Equal(t, 180, *items[0].PickupETAS)
	assert.Nil(t, items[0].ArrivalAt)
}
