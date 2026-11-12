package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
)

var ErrHeatmapBusy = errors.New("another instance is aggregating the heatmap")

type ZoomStep struct {
	MinZoom    int
	Resolution int
}

type HeatmapSettings struct {
	Interval        time.Duration
	Window          time.Duration
	LookbackWindows int
	Rollups         []int
	SelfWeight      float64
	NeighborWeight  float64
	Zooms           []ZoomStep
	MaxCells        int
	MaxPeriod       time.Duration
	CacheTTL        time.Duration
	Retention       time.Duration
}

type heatValue struct {
	count int64
	value float64
}

func (u *Usecase) RunHeatmapAggregator(ctx context.Context) {
	ticker := time.NewTicker(u.settings.Heatmap.Interval)
	defer ticker.Stop()
	for {
		if err := u.AggregateHeatmap(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.FromContext(ctx).Warn("heatmap aggregation failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (u *Usecase) AggregateHeatmap(ctx context.Context) error {
	settings := u.settings.Heatmap
	now := u.now()
	current := now.Truncate(settings.Window)
	from := current.Add(-time.Duration(settings.LookbackWindows-1) * settings.Window)
	err := u.AggregateHeatmapBetween(ctx, from, current.Add(settings.Window))
	if errors.Is(err, ErrHeatmapBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	if settings.Retention <= 0 {
		return nil
	}
	_, err = u.Heatmap.Purge(ctx, current.Add(-settings.Retention))
	return err
}

func (u *Usecase) AggregateHeatmapBetween(ctx context.Context, from, to time.Time) error {
	window := u.settings.Heatmap.Window
	at := u.now().Truncate(time.Microsecond)
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		acquired, err := u.Heatmap.TryLock(ctx)
		if err != nil {
			return err
		}
		if !acquired {
			return ErrHeatmapBusy
		}
		for start := from.UTC().Truncate(window); start.Before(to); start = start.Add(window) {
			if err := u.aggregateHeatWindow(ctx, start, at); err != nil {
				return err
			}
		}
		return nil
	})
}

func (u *Usecase) aggregateHeatWindow(ctx context.Context, start, at time.Time) error {
	began := time.Now()
	counts, err := u.Heatmap.CountRequests(ctx, start, start.Add(u.settings.Heatmap.Window))
	if err != nil {
		return err
	}
	byCategory := map[string]map[int64]int64{}
	var events int64
	for _, count := range counts {
		if byCategory[count.Category] == nil {
			byCategory[count.Category] = map[int64]int64{}
		}
		byCategory[count.Category][count.Cell] += count.Requests
		events += count.Requests
	}
	var cells []repository.HeatmapCell
	for category, base := range byCategory {
		layers, err := u.heatLayers(base)
		if err != nil {
			return err
		}
		for resolution, layer := range layers {
			for cell, heat := range layer {
				cells = append(cells, repository.HeatmapCell{Resolution: resolution, Category: category, Cell: cell, Count: heat.count, Value: heat.value})
			}
		}
	}
	if err := u.Heatmap.SaveWindow(ctx, start, cells, at); err != nil {
		return err
	}
	logger.FromContext(ctx).Info("heatmap window aggregated",
		zap.Time("window_start", start), zap.Int64("events", events), zap.Int("cells", len(cells)), zap.Duration("duration", time.Since(began)))
	return nil
}

func (u *Usecase) heatLayers(base map[int64]int64) (map[int]map[int64]heatValue, error) {
	counts := map[int]map[int64]int64{entities.HeatmapResolution: base}
	for _, resolution := range u.settings.Heatmap.Rollups {
		parents, err := u.rollUp(base, resolution)
		if err != nil {
			return nil, err
		}
		counts[resolution] = parents
	}
	layers := make(map[int]map[int64]heatValue, len(counts))
	for resolution, layer := range counts {
		smoothed, err := u.smooth(layer)
		if err != nil {
			return nil, err
		}
		layers[resolution] = smoothed
	}
	return layers, nil
}

func (u *Usecase) rollUp(cells map[int64]int64, resolution int) (map[int64]int64, error) {
	parents := make(map[int64]int64, len(cells))
	for cell, count := range cells {
		parent, err := u.Geo.Parent(cell, resolution)
		if err != nil {
			return nil, err
		}
		parents[parent] += count
	}
	return parents, nil
}

func (u *Usecase) smooth(counts map[int64]int64) (map[int64]heatValue, error) {
	out := make(map[int64]heatValue, len(counts))
	for cell := range counts {
		around, err := u.Geo.Disk(cell, 1)
		if err != nil {
			return nil, err
		}
		for _, candidate := range around {
			if _, done := out[candidate]; done {
				continue
			}
			value, err := u.smoothedValue(candidate, counts)
			if err != nil {
				return nil, err
			}
			if value > 0 {
				out[candidate] = heatValue{count: counts[candidate], value: value}
			}
		}
	}
	return out, nil
}

func (u *Usecase) smoothedValue(cell int64, counts map[int64]int64) (float64, error) {
	around, err := u.Geo.Disk(cell, 1)
	if err != nil {
		return 0, err
	}
	var sum float64
	neighbors := 0
	for _, neighbor := range around {
		if neighbor == cell {
			continue
		}
		neighbors++
		sum += float64(counts[neighbor])
	}
	mean := 0.0
	if neighbors > 0 {
		mean = sum / float64(neighbors)
	}
	return u.settings.Heatmap.SelfWeight*float64(counts[cell]) + u.settings.Heatmap.NeighborWeight*mean, nil
}

func (u *Usecase) BackfillPickupCells(ctx context.Context, batch int) (int64, error) {
	var after uuid.UUID
	var total int64
	for {
		pickups, err := u.Heatmap.PickupsWithoutCell(ctx, after, batch)
		if err != nil || len(pickups) == 0 {
			return total, err
		}
		cells := make(map[uuid.UUID]int64, len(pickups))
		for _, pickup := range pickups {
			after = pickup.ID
			cell, err := u.Geo.Cell(pickup.Pickup, entities.HeatmapResolution)
			if err != nil {
				logger.FromContext(ctx).Warn("pickup without a heat cell", zap.String("request_id", pickup.ID.String()), zap.Error(err))
				continue
			}
			cells[pickup.ID] = cell
		}
		updated, err := u.Heatmap.SetPickupCells(ctx, cells)
		if err != nil {
			return total, err
		}
		total += updated
		logger.FromContext(ctx).Info("pickup heat cells backfilled", zap.Int("batch", len(pickups)), zap.Int64("updated", total))
	}
}
