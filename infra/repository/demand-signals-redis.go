package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
)

const (
	demandQuotesTTL = 30 * time.Minute
	demandRatiosTTL = 15 * time.Minute
	demandRefresh   = "demand:refresh"
)

type DemandSignals struct {
	rdb *goredis.Client
}

var _ repository.IDemandSignals = (*DemandSignals)(nil)

func NewDemandSignals(rdb *goredis.Client) *DemandSignals {
	return &DemandSignals{rdb: rdb}
}

func demandScope(key repository.DemandKey) string {
	return key.ZoneID.String() + partSeparator + key.Category
}

func quotesKey(key repository.DemandKey) string {
	return "demand:quotes:" + demandScope(key)
}

func ratiosKey(key repository.DemandKey) string {
	return "demand:ratio:" + demandScope(key)
}

func factorsKey(key repository.DemandKey) string {
	return "demand:factor:" + demandScope(key)
}

func (s *DemandSignals) ClaimRefresh(ctx context.Context, every time.Duration) (bool, error) {
	err := s.rdb.SetArgs(ctx, demandRefresh, "1", goredis.SetArgs{Mode: "NX", TTL: every}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim demand refresh: %w", err)
	}
	return true, nil
}

func (s *DemandSignals) RecordQuote(ctx context.Context, key repository.DemandKey, area int64, customerID uuid.UUID, at time.Time) error {
	quotes := quotesKey(key)
	pipe := s.rdb.Pipeline()
	pipe.ZAdd(ctx, quotes, goredis.Z{Score: float64(at.UnixMilli()), Member: strconv.FormatInt(area, 10) + partSeparator + customerID.String()})
	pipe.ZRemRangeByScore(ctx, quotes, "-inf", "("+strconv.FormatInt(at.Add(-demandQuotesTTL).UnixMilli(), 10))
	pipe.Expire(ctx, quotes, demandQuotesTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record quote: %w", err)
	}
	return nil
}

func (s *DemandSignals) RecentQuotes(ctx context.Context, key repository.DemandKey, since, until time.Time) (map[int64][]uuid.UUID, error) {
	members, err := s.rdb.ZRangeArgs(ctx, goredis.ZRangeArgs{
		Key: quotesKey(key), ByScore: true, Start: strconv.FormatInt(since.UnixMilli(), 10), Stop: strconv.FormatInt(until.UnixMilli(), 10),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read quotes: %w", err)
	}
	out := map[int64][]uuid.UUID{}
	for _, member := range members {
		areaText, customerText, ok := strings.Cut(member, partSeparator)
		if !ok {
			continue
		}
		area, areaErr := strconv.ParseInt(areaText, 10, 64)
		customerID, customerErr := uuid.Parse(customerText)
		if areaErr != nil || customerErr != nil {
			continue
		}
		out[area] = append(out[area], customerID)
	}
	return out, nil
}

func (s *DemandSignals) FreeProviders(ctx context.Context, category string, resolution int, cells []int64) (int, error) {
	members := s.rdb.Pipeline()
	sets := make([]*goredis.StringSliceCmd, 0, len(cells))
	for _, cell := range cells {
		sets = append(sets, members.SMembers(ctx, indexKey(category, resolution, cell)))
	}
	if _, err := members.Exec(ctx); err != nil {
		return 0, fmt.Errorf("read index: %w", err)
	}
	ids := map[string]struct{}{}
	for _, set := range sets {
		for _, id := range set.Val() {
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	type state struct{ online, reserved, busy *goredis.IntCmd }
	checks := s.rdb.Pipeline()
	states := make([]state, 0, len(ids))
	for id := range ids {
		providerID, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		states = append(states, state{
			online:   checks.Exists(ctx, posKey(providerID)),
			reserved: checks.Exists(ctx, lockKey(providerID)),
			busy:     checks.Exists(ctx, tripKey(providerID)),
		})
	}
	if len(states) == 0 {
		return 0, nil
	}
	if _, err := checks.Exec(ctx); err != nil {
		return 0, fmt.Errorf("check providers: %w", err)
	}
	free := 0
	for _, st := range states {
		if st.online.Val() > 0 && st.reserved.Val() == 0 && st.busy.Val() == 0 {
			free++
		}
	}
	return free, nil
}

func (s *DemandSignals) Ratios(ctx context.Context, key repository.DemandKey) (map[int64]int, error) {
	values, err := s.rdb.HGetAll(ctx, ratiosKey(key)).Result()
	if err != nil {
		return nil, fmt.Errorf("read demand ratios: %w", err)
	}
	return parseCounts(values), nil
}

func (s *DemandSignals) Publish(ctx context.Context, key repository.DemandKey, ratios, factors map[int64]int, factorTTL time.Duration) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, ratiosKey(key), factorsKey(key))
	if len(ratios) > 0 {
		pipe.HSet(ctx, ratiosKey(key), countFields(ratios)...)
		pipe.Expire(ctx, ratiosKey(key), demandRatiosTTL)
	}
	if len(factors) > 0 {
		pipe.HSet(ctx, factorsKey(key), countFields(factors)...)
		pipe.Expire(ctx, factorsKey(key), factorTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("publish demand: %w", err)
	}
	return nil
}

func (s *DemandSignals) Factor(ctx context.Context, key repository.DemandKey, area int64) (int, error) {
	value, err := s.rdb.HGet(ctx, factorsKey(key), strconv.FormatInt(area, 10)).Int()
	if errors.Is(err, goredis.Nil) {
		return entities.NeutralDemandFactorBps, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read demand factor: %w", err)
	}
	return value, nil
}

func (s *DemandSignals) Factors(ctx context.Context, key repository.DemandKey) (map[int64]int, error) {
	values, err := s.rdb.HGetAll(ctx, factorsKey(key)).Result()
	if err != nil {
		return nil, fmt.Errorf("read demand factors: %w", err)
	}
	return parseCounts(values), nil
}

func parseCounts(values map[string]string) map[int64]int {
	out := make(map[int64]int, len(values))
	for field, value := range values {
		area, areaErr := strconv.ParseInt(field, 10, 64)
		count, countErr := strconv.Atoi(value)
		if areaErr != nil || countErr != nil {
			continue
		}
		out[area] = count
	}
	return out
}

func countFields(values map[int64]int) []any {
	fields := make([]any, 0, len(values)*2)
	for area, value := range values {
		fields = append(fields, strconv.FormatInt(area, 10), value)
	}
	return fields
}
