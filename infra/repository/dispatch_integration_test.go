//go:build integration

package repository_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/domain/entities"
	domain "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/realtime"
	"github.com/Agora-Mobility/atlas/infra/repository"
)

const luandaGeoJSON = `{"type":"MultiPolygon","coordinates":[[[[12.9,-9.4],[13.8,-9.4],[13.8,-8.5],[12.9,-8.5],[12.9,-9.4]]]]}`

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:8.8.4-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp")},
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := goredis.NewClient(&goredis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func taxiIndex() []entities.LiveIndex {
	return []entities.LiveIndex{{Slug: "taxi", Resolution: 7}, {Slug: "taxi", Resolution: 5}}
}

func TestLocatorKeepsTheIndexOnlyWhereTheProviderIs(t *testing.T) {
	rdb := redisClient(t)
	geo := h3index.New()
	locator := repository.NewProviderLocator(rdb, geo)
	ctx := context.Background()
	provider := uuid.New()
	at := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	now := time.Now().UTC()

	require.NoError(t, locator.SetOnline(ctx, provider, taxiIndex(), time.Minute))
	require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: at, AccuracyM: 5, RecordedAt: now}, time.Minute))

	cell, err := geo.Cell(at, 7)
	require.NoError(t, err)
	found, err := locator.FindInCells(ctx, "taxi", 7, []int64{cell})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, provider, found[0].ProviderID)
	assert.InDelta(t, at.Lat, found[0].Point.Lat, 1e-5)

	far := entities.GeoPoint{Lat: -8.90, Lng: 13.30}
	require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: far, AccuracyM: 5, RecordedAt: now.Add(time.Minute)}, time.Minute))
	found, err = locator.FindInCells(ctx, "taxi", 7, []int64{cell})
	require.NoError(t, err)
	assert.Empty(t, found, "a change of cell leaves the old set")
	members, err := rdb.SCard(ctx, "live:idx:taxi:r7:"+itoa(cell)).Result()
	require.NoError(t, err)
	assert.Zero(t, members)

	wideCell, err := geo.Cell(far, 5)
	require.NoError(t, err)
	found, err = locator.FindInCells(ctx, "taxi", 5, []int64{wideCell})
	require.NoError(t, err)
	assert.Len(t, found, 1, "one position feeds every resolution of the category")

	require.NoError(t, locator.SetOffline(ctx, provider))
	online, err := locator.IsOnline(ctx, provider)
	require.NoError(t, err)
	assert.False(t, online)
	found, err = locator.FindInCells(ctx, "taxi", 5, []int64{wideCell})
	require.NoError(t, err)
	assert.Empty(t, found)
}

func itoa(cell int64) string {
	encoded, _ := json.Marshal(cell)
	return string(encoded)
}

func TestSameCellDoesNotRewriteTheIndex(t *testing.T) {
	rdb := redisClient(t)
	geo := h3index.New()
	locator := repository.NewProviderLocator(rdb, geo)
	ctx := context.Background()
	provider := uuid.New()
	at := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	require.NoError(t, locator.SetOnline(ctx, provider, taxiIndex()[:1], time.Minute))
	require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: at, RecordedAt: time.Now()}, time.Minute))

	var writes int
	hook := &countingHook{commands: map[string]int{}}
	rdb.AddHook(hook)
	require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: entities.GeoPoint{Lat: -8.84001, Lng: 13.23001}, RecordedAt: time.Now()}, time.Minute))
	writes = hook.commands["sadd"] + hook.commands["srem"]
	assert.Zero(t, writes, "most positions never touch the index")
}

type countingHook struct{ commands map[string]int }

func (h *countingHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *countingHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		h.commands[cmd.Name()]++
		return next(ctx, cmd)
	}
}
func (h *countingHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			h.commands[cmd.Name()]++
		}
		return next(ctx, cmds)
	}
}

func TestExpiredProviderIsCleanedOnRead(t *testing.T) {
	rdb := redisClient(t)
	geo := h3index.New()
	locator := repository.NewProviderLocator(rdb, geo)
	ctx := context.Background()
	provider := uuid.New()
	at := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	require.NoError(t, locator.SetOnline(ctx, provider, taxiIndex()[:1], time.Minute))
	require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: at, RecordedAt: time.Now()}, time.Minute))
	require.NoError(t, rdb.Del(ctx, "live:pos:"+provider.String()).Err())
	cell, err := geo.Cell(at, 7)
	require.NoError(t, err)
	key := "live:idx:taxi:r7:" + itoa(cell)
	require.EqualValues(t, 1, rdb.SCard(ctx, key).Val())

	found, err := locator.FindInCells(ctx, "taxi", 7, []int64{cell})

	require.NoError(t, err)
	assert.Empty(t, found)
	assert.Zero(t, rdb.SCard(ctx, key).Val(), "no leak of dead providers")
}

func TestReservationIsExclusiveAndOnlyItsOfferReleasesIt(t *testing.T) {
	locator := repository.NewProviderLocator(redisClient(t), h3index.New())
	ctx := context.Background()
	provider, first, second := uuid.New(), uuid.New(), uuid.New()

	ok, err := locator.Reserve(ctx, provider, first, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = locator.Reserve(ctx, provider, second, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, locator.Release(ctx, provider, second))
	reserved, err := locator.IsReserved(ctx, provider)
	require.NoError(t, err)
	assert.True(t, reserved, "another offer cannot free the provider")

	require.NoError(t, locator.Release(ctx, provider, first))
	reserved, err = locator.IsReserved(ctx, provider)
	require.NoError(t, err)
	assert.False(t, reserved)
}

func TestMailboxOrderCapAndCursor(t *testing.T) {
	rdb := redisClient(t)
	mailbox := realtime.NewMailbox(rdb, 10, time.Hour)
	ctx := context.Background()
	user := uuid.NewString()
	for range 200 {
		require.NoError(t, mailbox.Deliver(ctx, user, services.MailboxMessage{EventID: uuid.NewString(), Type: "t", Class: services.ClassState, Data: []byte(`{}`)}))
	}
	length, err := rdb.XLen(ctx, "rt:box:"+user).Result()
	require.NoError(t, err)
	assert.LessOrEqual(t, length, int64(120), "the cap holds, approximately")

	entries, err := mailbox.Read(ctx, user, "", 1000)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	require.NoError(t, mailbox.CheckCursor(ctx, user, entries[0].ID))
	assert.ErrorIs(t, mailbox.CheckCursor(ctx, user, "1-0"), services.ErrMailboxCursorLost)
	assert.ErrorIs(t, mailbox.CheckCursor(ctx, user, "not-an-id"), services.ErrMailboxCursorLost)
	assert.ErrorIs(t, mailbox.CheckCursor(ctx, uuid.NewString(), "1-0"), services.ErrMailboxCursorLost)
	require.NoError(t, mailbox.CheckCursor(ctx, uuid.NewString(), ""))

	ttl, err := rdb.PTTL(ctx, "rt:box:"+user).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl, "a forgotten mailbox disappears")
}

func TestMailboxAgedEntriesAreTrimmed(t *testing.T) {
	rdb := redisClient(t)
	mailbox := realtime.NewMailbox(rdb, 100_000, time.Millisecond)
	ctx := context.Background()
	user := uuid.NewString()
	message := func() services.MailboxMessage {
		return services.MailboxMessage{EventID: uuid.NewString(), Type: "t", Class: services.ClassState, Data: []byte(`{}`)}
	}
	require.NoError(t, mailbox.Deliver(ctx, user, message()))
	assert.LessOrEqual(t, rdb.XLen(ctx, "rt:box:"+user).Val(), int64(1))
}

func TestPositionFeedKeepsOnlyTheLastValue(t *testing.T) {
	rdb := redisClient(t)
	mailbox := realtime.NewMailbox(rdb, 10, time.Hour)
	ctx := context.Background()
	require.NoError(t, mailbox.PublishPosition(ctx, "u", "r", []byte(`{"lat":1}`)))
	require.NoError(t, mailbox.PublishPosition(ctx, "u", "r", []byte(`{"lat":2}`)))
	last, err := mailbox.LastPosition(ctx, "r")
	require.NoError(t, err)
	assert.JSONEq(t, `{"lat":2}`, string(last))
	none, err := mailbox.LastPosition(ctx, "other")
	require.NoError(t, err)
	assert.Nil(t, none)
}

func seedUser(t *testing.T, owner *pgxpool.Pool, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := owner.Exec(context.Background(), `INSERT INTO users (id, name, phone, password, is_verified, status, role) VALUES ($1, 'Teste', $2, 'x', true, 'active', $3)`, id, "+2449"+id.String()[:8], role)
	require.NoError(t, err)
	return id
}

func seedRequest(t *testing.T, app, owner *pgxpool.Pool) (*entities.ServiceRequest, *entities.DispatchPolicy) {
	t.Helper()
	ctx := context.Background()
	config := repository.NewDispatchConfigRepository(app)
	categoryID, err := config.FindCategoryIDBySlug(ctx, "taxi")
	require.NoError(t, err)
	offered, err := config.ListServicesOfCategory(ctx, "taxi")
	require.NoError(t, err)
	zone := &entities.ServiceZone{ID: uuid.New(), CountryCode: "AO", Name: uuid.NewString(), GeoJSON: luandaGeoJSON, Timezone: "Africa/Luanda", Currency: "AOA", IsActive: true}
	require.NoError(t, config.CreateZone(ctx, zone))
	policy, err := config.ResolvePolicy(ctx, *categoryID, zone.ID)
	require.NoError(t, err)
	request, err := entities.NewServiceRequest(entities.NewRequestInput{
		ID: uuid.New(), CustomerID: seedUser(t, owner, "customer"), Service: offered[0], Policy: policy, Zone: zone,
		Pickup:         entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, Reference: "Farmácia"},
		Destination:    &entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.92, Lng: 13.18}},
		Stops:          []entities.RequestPlace{{Point: entities.GeoPoint{Lat: -8.88, Lng: 13.2}, Address: "Shopping Fortaleza"}},
		IdempotencyKey: uuid.NewString(), Now: time.Now().UTC(), Fare: entities.RequestFare{Currency: "AOA"},
	})
	require.NoError(t, err)
	require.NoError(t, repository.NewRequestRepository(app).Create(ctx, request))
	return request, policy
}

func TestRequestRoundTripAndOptimisticLock(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	requests := repository.NewRequestRepository(app)
	request, _ := seedRequest(t, app, owner)

	stored, err := requests.Find(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, "Farmácia", stored.Pickup.Reference)
	assert.Equal(t, "Shopping Fortaleza", stored.Stops[0].Address)
	assert.InDelta(t, -8.92, stored.Destination.Point.Lat, 1e-9)

	var inZone bool
	require.NoError(t, owner.QueryRow(ctx, `SELECT ST_DWithin(pickup_geom, ST_SetSRID(ST_MakePoint(13.23, -8.84), 4326)::geography, 1) FROM service_requests WHERE id = $1`, request.ID).Scan(&inZone))
	assert.True(t, inZone, "the pin is stored as a PostGIS point")

	change, err := stored.Transition(entities.RequestSearching, entities.SystemRequestActor(), "", time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, requests.Update(ctx, stored, change.ExpectedVersion))
	assert.ErrorIs(t, requests.Update(ctx, stored, change.ExpectedVersion), domain.ErrRequestVersionConflict, "losing a race changes nothing")

	duplicate := *request
	duplicate.ID = uuid.New()
	assert.ErrorIs(t, requests.Create(ctx, &duplicate), domain.ErrDuplicateIdempotency)

	byKey, err := requests.FindByIdempotencyKey(ctx, request.CustomerID, request.IdempotencyKey)
	require.NoError(t, err)
	assert.Equal(t, request.ID, byKey.ID)
}

func TestDueWorkIsSharedBetweenInstancesWithSkipLocked(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	requests := repository.NewRequestRepository(app)
	request, _ := seedRequest(t, app, owner)
	stored, err := requests.Find(ctx, request.ID)
	require.NoError(t, err)
	now := time.Now().UTC()
	stored.NextActionAt = &now
	require.NoError(t, requests.Update(ctx, stored, stored.Version))

	tx := postgres.NewTxManager(app)
	seen := make(chan []uuid.UUID, 1)
	release := make(chan struct{})
	go func() {
		_ = tx.WithinTransaction(ctx, func(ctx context.Context) error {
			ids, _ := requests.DueRequests(ctx, now.Add(time.Second), 10)
			seen <- ids
			<-release
			return nil
		})
	}()
	require.Len(t, <-seen, 1)
	other, err := requests.DueRequests(ctx, now.Add(time.Second), 10)
	require.NoError(t, err)
	assert.Empty(t, other, "the row locked by another instance is skipped")
	close(release)
}

func TestOfferUniquePerProviderUntilAHigherBoost(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	offers := repository.NewOfferRepository(app)
	request, policy := seedRequest(t, app, owner)
	provider := seedUser(t, owner, "provider")
	now := time.Now().UTC()

	first := entities.NewRequestOffer(uuid.New(), request.ID, provider, policy, 500, nil, 0, now)
	require.NoError(t, offers.Create(ctx, first))
	assert.ErrorIs(t, offers.Create(ctx, entities.NewRequestOffer(uuid.New(), request.ID, provider, policy, 500, nil, 1000, now)), domain.ErrOfferAlreadySent, "an open offer is not replaced")

	first.Close(entities.OfferRejected, now)
	require.NoError(t, offers.Update(ctx, first))
	assert.ErrorIs(t, offers.Create(ctx, entities.NewRequestOffer(uuid.New(), request.ID, provider, policy, 500, nil, 0, now)), domain.ErrOfferAlreadySent, "the same price is not offered twice")
	require.NoError(t, offers.Create(ctx, entities.NewRequestOffer(uuid.New(), request.ID, provider, policy, 500, nil, 1000, now)))

	listed, err := offers.ListOfRequest(ctx, request.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, entities.OfferSent, listed[0].Status)
	assert.Equal(t, 1000, listed[0].BoostBps)
}

func TestZonesAndPolicyResolution(t *testing.T) {
	app, _ := setupDBs(t)
	ctx := context.Background()
	config := repository.NewDispatchConfigRepository(app)
	zone := &entities.ServiceZone{ID: uuid.New(), CountryCode: "AO", Name: "Luanda", GeoJSON: luandaGeoJSON, Timezone: "Africa/Luanda", Currency: "AOA", IsActive: true}
	require.NoError(t, config.CreateZone(ctx, zone))

	covering, err := config.ZoneCoveringPoint(ctx, entities.GeoPoint{Lat: -8.84, Lng: 13.23})
	require.NoError(t, err)
	require.NotNil(t, covering)
	assert.Equal(t, zone.ID, *covering)
	outside, err := config.ZoneCoveringPoint(ctx, entities.GeoPoint{Lat: -14.9, Lng: 13.5})
	require.NoError(t, err)
	assert.Nil(t, outside)

	stored, err := config.FindZone(ctx, zone.ID)
	require.NoError(t, err)
	require.Len(t, stored.Polygons, 1)
	cells, err := h3index.New().InteriorCells(stored.Polygons, h3index.Coverage)
	require.NoError(t, err)
	assert.NotEmpty(t, cells)

	valid, err := config.IsValidGeometry(ctx, luandaGeoJSON)
	require.NoError(t, err)
	assert.True(t, valid)
	valid, err = config.IsValidGeometry(ctx, `{"type":"Nonsense"}`)
	require.NoError(t, err)
	assert.False(t, valid)

	categoryID, err := config.FindCategoryIDBySlug(ctx, "taxi")
	require.NoError(t, err)
	general, err := config.ResolvePolicy(ctx, *categoryID, zone.ID)
	require.NoError(t, err)
	assert.Nil(t, general.ZoneID)
	assert.Equal(t, []float64{3, 5, 10}, general.RadiiKm)

	specific := *general
	specific.ID, specific.ZoneID, specific.OfferTTLSeconds = uuid.New(), &zone.ID, 30
	require.NoError(t, config.CreatePolicy(ctx, &specific))
	resolved, err := config.ResolvePolicy(ctx, *categoryID, zone.ID)
	require.NoError(t, err)
	assert.Equal(t, 30, resolved.OfferTTLSeconds, "the zone wins over the default")
	assert.Error(t, config.CreatePolicy(ctx, &specific), "one policy per category and zone")
}

func TestTariffVersionsAndETA(t *testing.T) {
	app, _ := setupDBs(t)
	ctx := context.Background()
	config := repository.NewDispatchConfigRepository(app)
	pricing := repository.NewPricingRepository(app)
	zone := &entities.ServiceZone{ID: uuid.New(), CountryCode: "AO", Name: "Luanda", GeoJSON: luandaGeoJSON, Timezone: "Africa/Luanda", Currency: "AOA", IsActive: true}
	require.NoError(t, config.CreateZone(ctx, zone))
	offered, err := config.ListServicesOfCategory(ctx, "taxi")
	require.NoError(t, err)
	serviceID := offered[0].ID

	general := &entities.ServiceTariff{ID: uuid.New(), ServiceID: serviceID, Currency: "AOA", BaseFeeCents: 100, ValidFrom: time.Now()}
	require.NoError(t, pricing.ReplaceTariff(ctx, general))
	current, err := pricing.CurrentTariff(ctx, serviceID, zone.ID)
	require.NoError(t, err)
	assert.Equal(t, general.ID, current.ID)

	newer := &entities.ServiceTariff{ID: uuid.New(), ServiceID: serviceID, Currency: "AOA", BaseFeeCents: 200, ValidFrom: time.Now()}
	require.NoError(t, pricing.ReplaceTariff(ctx, newer))
	closed, err := pricing.FindTariff(ctx, general.ID)
	require.NoError(t, err)
	assert.NotNil(t, closed.ValidUntil, "the old version stays to explain old prices")
	assert.EqualValues(t, 100, closed.BaseFeeCents)

	zonal := &entities.ServiceTariff{ID: uuid.New(), ServiceID: serviceID, ZoneID: &zone.ID, Currency: "AOA", BaseFeeCents: 300, ValidFrom: time.Now()}
	require.NoError(t, pricing.ReplaceTariff(ctx, zonal))
	current, err = pricing.CurrentTariff(ctx, serviceID, zone.ID)
	require.NoError(t, err)
	assert.Equal(t, zonal.ID, current.ID, "the zone wins over the default")

	require.NoError(t, pricing.RecordETA(ctx, 1, 2, 8, 10*time.Minute))
	require.NoError(t, pricing.RecordETA(ctx, 1, 2, 8, 20*time.Minute))
	learned, samples, err := pricing.LearnedETA(ctx, 1, 2, 8)
	require.NoError(t, err)
	assert.Equal(t, 2, samples)
	assert.Equal(t, 15*time.Minute, learned)
}
