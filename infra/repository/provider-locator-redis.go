package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	liveTripTTL    = 12 * time.Hour
	fieldLat       = "lat"
	fieldLng       = "lng"
	fieldHeading   = "heading"
	fieldAccuracy  = "accuracy"
	fieldTimestamp = "ts"
	fieldStatus    = "status"
	fieldIndexes   = "categorias"
	fieldCells     = "cells"
	statusOnline   = "online"
	listSeparator  = ","
	partSeparator  = ":"
)

var releaseReservation = goredis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`)

type ProviderLocator struct {
	rdb *goredis.Client
	geo services.IGeoIndex
}

var _ repository.IProviderLocator = (*ProviderLocator)(nil)

func NewProviderLocator(rdb *goredis.Client, geo services.IGeoIndex) *ProviderLocator {
	return &ProviderLocator{rdb: rdb, geo: geo}
}

func posKey(providerID uuid.UUID) string {
	return "live:pos:" + providerID.String()
}

func indexKey(category string, resolution int, cell int64) string {
	return fmt.Sprintf("live:idx:%s:r%d:%d", category, resolution, cell)
}

func tripKey(providerID uuid.UUID) string {
	return "live:trip:" + providerID.String()
}

func lockKey(providerID uuid.UUID) string {
	return "offer:lock:" + providerID.String()
}

func sampleKey(providerID uuid.UUID) string {
	return "live:sample:" + providerID.String()
}

func (l *ProviderLocator) SetOnline(ctx context.Context, providerID uuid.UUID, indexes []entities.LiveIndex, ttl time.Duration) error {
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, index.Slug+partSeparator+strconv.Itoa(index.Resolution))
	}
	pipe := l.rdb.Pipeline()
	pipe.HSet(ctx, posKey(providerID), fieldStatus, statusOnline, fieldIndexes, strings.Join(parts, listSeparator))
	pipe.Expire(ctx, posKey(providerID), ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set online: %w", err)
	}
	return nil
}

func (l *ProviderLocator) SetOffline(ctx context.Context, providerID uuid.UUID) error {
	cells, err := l.rdb.HGet(ctx, posKey(providerID), fieldCells).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("read cells: %w", err)
	}
	pipe := l.rdb.Pipeline()
	for _, key := range splitList(cells) {
		pipe.SRem(ctx, key, providerID.String())
	}
	pipe.Del(ctx, posKey(providerID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("set offline: %w", err)
	}
	return nil
}

func (l *ProviderLocator) IsOnline(ctx context.Context, providerID uuid.UUID) (bool, error) {
	exists, err := l.rdb.Exists(ctx, posKey(providerID)).Result()
	if err != nil {
		return false, fmt.Errorf("check online: %w", err)
	}
	return exists > 0, nil
}

func (l *ProviderLocator) LastPosition(ctx context.Context, providerID uuid.UUID) (*entities.ProviderPosition, error) {
	values, err := l.rdb.HMGet(ctx, posKey(providerID), fieldLat, fieldLng, fieldTimestamp, fieldAccuracy).Result()
	if err != nil {
		return nil, fmt.Errorf("last position: %w", err)
	}
	point, recordedAt, ok := parsePosition(values[0], values[1], values[2])
	if !ok {
		return nil, nil
	}
	position := &entities.ProviderPosition{ProviderID: providerID, Point: point, RecordedAt: recordedAt}
	if accuracy, ok := values[3].(string); ok {
		position.AccuracyM, _ = strconv.ParseFloat(accuracy, 64)
	}
	return position, nil
}

func (l *ProviderLocator) UpdatePosition(ctx context.Context, position entities.ProviderPosition, ttl time.Duration) error {
	key := posKey(position.ProviderID)
	current, err := l.rdb.HMGet(ctx, key, fieldIndexes, fieldCells).Result()
	if err != nil {
		return fmt.Errorf("read presence: %w", err)
	}
	indexes, _ := current[0].(string)
	previous, _ := current[1].(string)
	next, err := l.indexKeys(indexes, position.Point)
	if err != nil {
		return err
	}
	previousKeys := splitList(previous)

	pipe := l.rdb.Pipeline()
	fields := []any{
		fieldLat, formatFloat(position.Point.Lat), fieldLng, formatFloat(position.Point.Lng),
		fieldAccuracy, formatFloat(position.AccuracyM), fieldTimestamp, position.RecordedAt.UnixMilli(),
		fieldCells, strings.Join(next, listSeparator),
	}
	if position.Heading != nil {
		fields = append(fields, fieldHeading, formatFloat(*position.Heading))
	}
	pipe.HSet(ctx, key, fields...)
	pipe.Expire(ctx, key, ttl)
	for _, stale := range difference(previousKeys, next) {
		pipe.SRem(ctx, stale, position.ProviderID.String())
	}
	for _, fresh := range difference(next, previousKeys) {
		pipe.SAdd(ctx, fresh, position.ProviderID.String())
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("update position: %w", err)
	}
	return nil
}

func (l *ProviderLocator) Touch(ctx context.Context, providerID uuid.UUID, ttl time.Duration) error {
	if err := l.rdb.Expire(ctx, posKey(providerID), ttl).Err(); err != nil {
		return fmt.Errorf("touch presence: %w", err)
	}
	return nil
}

func indexSlugs(indexes string) []string {
	entries := splitList(indexes)
	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if slug, _, ok := strings.Cut(entry, partSeparator); ok && slug != "" {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

func (l *ProviderLocator) indexKeys(indexes string, point entities.GeoPoint) ([]string, error) {
	keys := make([]string, 0, 2)
	for _, entry := range splitList(indexes) {
		slug, resolutionText, ok := strings.Cut(entry, partSeparator)
		if !ok {
			continue
		}
		resolution, err := strconv.Atoi(resolutionText)
		if err != nil {
			return nil, fmt.Errorf("parse index %q: %w", entry, err)
		}
		cell, err := l.geo.Cell(point, resolution)
		if err != nil {
			return nil, err
		}
		keys = append(keys, indexKey(slug, resolution, cell))
	}
	return keys, nil
}

func (l *ProviderLocator) FindInCells(ctx context.Context, category string, resolution int, cells []int64) ([]repository.LiveCandidate, error) {
	members := l.rdb.Pipeline()
	sets := make([]*goredis.StringSliceCmd, 0, len(cells))
	keys := make([]string, 0, len(cells))
	for _, cell := range cells {
		key := indexKey(category, resolution, cell)
		keys = append(keys, key)
		sets = append(sets, members.SMembers(ctx, key))
	}
	if _, err := members.Exec(ctx); err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}

	ids := make([]string, 0)
	cellOf := map[string]string{}
	for i, set := range sets {
		for _, id := range set.Val() {
			if _, seen := cellOf[id]; !seen {
				ids = append(ids, id)
			}
			cellOf[id] = keys[i]
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	positions := l.rdb.Pipeline()
	reads := make([]*goredis.SliceCmd, 0, len(ids))
	for _, id := range ids {
		reads = append(reads, positions.HMGet(ctx, "live:pos:"+id, fieldLat, fieldLng, fieldTimestamp))
	}
	if _, err := positions.Exec(ctx); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}

	candidates := make([]repository.LiveCandidate, 0, len(ids))
	cleanup := l.rdb.Pipeline()
	stale := 0
	for i, id := range ids {
		values := reads[i].Val()
		providerID, parseErr := uuid.Parse(id)
		point, recordedAt, valid := parsePosition(values[0], values[1], values[2])
		if parseErr != nil || !valid {
			cleanup.SRem(ctx, cellOf[id], id)
			stale++
			continue
		}
		candidates = append(candidates, repository.LiveCandidate{ProviderID: providerID, Point: point, RecordedAt: recordedAt})
	}
	if stale > 0 {
		if _, err := cleanup.Exec(ctx); err != nil {
			return nil, fmt.Errorf("clean index: %w", err)
		}
	}
	return candidates, nil
}

func (l *ProviderLocator) CountInCells(ctx context.Context, category string, resolution int, cells []int64) (map[int64]int64, error) {
	pipe := l.rdb.Pipeline()
	counts := make([]*goredis.IntCmd, 0, len(cells))
	for _, cell := range cells {
		counts = append(counts, pipe.SCard(ctx, indexKey(category, resolution, cell)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("count index: %w", err)
	}
	out := make(map[int64]int64, len(cells))
	for i, cell := range cells {
		if count := counts[i].Val(); count > 0 {
			out[cell] = count
		}
	}
	return out, nil
}

func (l *ProviderLocator) AllPositions(ctx context.Context) ([]repository.LiveCandidate, error) {
	var out []repository.LiveCandidate
	iterator := l.rdb.Scan(ctx, 0, "live:pos:*", 500).Iterator()
	for iterator.Next(ctx) {
		values, err := l.rdb.HMGet(ctx, iterator.Val(), fieldLat, fieldLng, fieldTimestamp, fieldIndexes).Result()
		if err != nil {
			return nil, fmt.Errorf("read position: %w", err)
		}
		point, recordedAt, ok := parsePosition(values[0], values[1], values[2])
		if !ok {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(iterator.Val(), "live:pos:"))
		if err != nil {
			continue
		}
		indexes, _ := values[3].(string)
		out = append(out, repository.LiveCandidate{ProviderID: id, Point: point, RecordedAt: recordedAt, Categories: indexSlugs(indexes)})
	}
	if err := iterator.Err(); err != nil {
		return nil, fmt.Errorf("scan positions: %w", err)
	}
	return out, nil
}

func (l *ProviderLocator) Reserve(ctx context.Context, providerID, offerID uuid.UUID, ttl time.Duration) (bool, error) {
	err := l.rdb.SetArgs(ctx, lockKey(providerID), offerID.String(), goredis.SetArgs{Mode: "NX", TTL: ttl}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve provider: %w", err)
	}
	return true, nil
}

func (l *ProviderLocator) Release(ctx context.Context, providerID, offerID uuid.UUID) error {
	if err := releaseReservation.Run(ctx, l.rdb, []string{lockKey(providerID)}, offerID.String()).Err(); err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("release provider: %w", err)
	}
	return nil
}

func (l *ProviderLocator) IsReserved(ctx context.Context, providerID uuid.UUID) (bool, error) {
	exists, err := l.rdb.Exists(ctx, lockKey(providerID)).Result()
	if err != nil {
		return false, fmt.Errorf("check reservation: %w", err)
	}
	return exists > 0, nil
}

func (l *ProviderLocator) ReservedAmong(ctx context.Context, providerIDs []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	pipe := l.rdb.Pipeline()
	checks := make([]*goredis.IntCmd, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		checks = append(checks, pipe.Exists(ctx, lockKey(providerID)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("check reservations: %w", err)
	}
	reserved := map[uuid.UUID]struct{}{}
	for i, check := range checks {
		if check.Val() > 0 {
			reserved[providerIDs[i]] = struct{}{}
		}
	}
	return reserved, nil
}

func (l *ProviderLocator) SetTrip(ctx context.Context, providerID uuid.UUID, trip entities.ActiveTrip) error {
	body, err := json.Marshal(trip)
	if err != nil {
		return fmt.Errorf("encode trip: %w", err)
	}
	if err := l.rdb.Set(ctx, tripKey(providerID), body, liveTripTTL).Err(); err != nil {
		return fmt.Errorf("set trip: %w", err)
	}
	return nil
}

func (l *ProviderLocator) ClearTrip(ctx context.Context, providerID uuid.UUID) error {
	if err := l.rdb.Del(ctx, tripKey(providerID)).Err(); err != nil {
		return fmt.Errorf("clear trip: %w", err)
	}
	return nil
}

func (l *ProviderLocator) Trip(ctx context.Context, providerID uuid.UUID) (*entities.ActiveTrip, error) {
	body, err := l.rdb.Get(ctx, tripKey(providerID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}
	var trip entities.ActiveTrip
	if err := json.Unmarshal(body, &trip); err != nil {
		return nil, fmt.Errorf("decode trip: %w", err)
	}
	return &trip, nil
}

func (l *ProviderLocator) ClaimSample(ctx context.Context, providerID uuid.UUID, every time.Duration) (bool, error) {
	err := l.rdb.SetArgs(ctx, sampleKey(providerID), "1", goredis.SetArgs{Mode: "NX", TTL: every}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim sample: %w", err)
	}
	return true, nil
}

func (l *ProviderLocator) ClaimLostAlert(ctx context.Context, requestID uuid.UUID, every time.Duration) (bool, error) {
	err := l.rdb.SetArgs(ctx, "live:lost:"+requestID.String(), "1", goredis.SetArgs{Mode: "NX", TTL: every}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim lost alert: %w", err)
	}
	return true, nil
}

func (l *ProviderLocator) ClaimRideLivePush(ctx context.Context, requestID uuid.UUID, every time.Duration) (bool, error) {
	err := l.rdb.SetArgs(ctx, "live:ridepush:"+requestID.String(), "1", goredis.SetArgs{Mode: "NX", TTL: every}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim ride live push: %w", err)
	}
	return true, nil
}

func parsePosition(lat, lng, timestamp any) (entities.GeoPoint, time.Time, bool) {
	latText, latOK := lat.(string)
	lngText, lngOK := lng.(string)
	timestampText, timestampOK := timestamp.(string)
	if !latOK || !lngOK || !timestampOK {
		return entities.GeoPoint{}, time.Time{}, false
	}
	latValue, latErr := strconv.ParseFloat(latText, 64)
	lngValue, lngErr := strconv.ParseFloat(lngText, 64)
	millis, timestampErr := strconv.ParseInt(timestampText, 10, 64)
	if latErr != nil || lngErr != nil || timestampErr != nil {
		return entities.GeoPoint{}, time.Time{}, false
	}
	return entities.GeoPoint{Lat: latValue, Lng: lngValue}, time.UnixMilli(millis).UTC(), true
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, listSeparator)
}

func difference(from, without []string) []string {
	var out []string
	for _, candidate := range from {
		found := false
		for _, other := range without {
			if candidate == other {
				found = true
				break
			}
		}
		if !found {
			out = append(out, candidate)
		}
	}
	return out
}
