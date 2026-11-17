package dispatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	MetricCount = "count"
	MetricValue = "value"

	maxMapZoom     = 24
	boxRoundingDeg = 0.001
)

var (
	ErrHeatmapBox    = errors.New("bbox must be minLng,minLat,maxLng,maxLat with longitudes between -180 and 180, latitudes between -90 and 90 and each minimum below its maximum")
	ErrHeatmapZoom   = errors.New("zoom must be between 0 and 24")
	ErrHeatmapMetric = errors.New("metric must be count or value")
	ErrHeatmapPeriod = errors.New("the period must end after it starts and span at most the allowed days")
	ErrHeatmapArea   = errors.New("the area is too large even at the coarsest resolution, zoom in")
)

func (u *Usecase) HeatCells(ctx context.Context, query dtos.HeatCellsQuery) (*dtos.HeatCellsView, error) {
	if !query.Box.IsValid() {
		return nil, ErrHeatmapBox
	}
	if query.Zoom < 0 || query.Zoom > maxMapZoom {
		return nil, ErrHeatmapZoom
	}
	metric := query.Metric
	if metric == "" {
		metric = MetricValue
	}
	if metric != MetricCount && metric != MetricValue {
		return nil, ErrHeatmapMetric
	}
	from, to, err := u.heatPeriod(query.From, query.To)
	if err != nil {
		return nil, err
	}
	box := query.Box.RoundedOut(boxRoundingDeg)
	resolution := u.zoomResolution(query.Zoom)
	key := fmt.Sprintf("heatmap:cells:%d:%s:%s:%d:%d:%.3f,%.3f,%.3f,%.3f", resolution, query.Category, metric, from.Unix(), to.Unix(), box.MinLng, box.MinLat, box.MaxLng, box.MaxLat)
	var cached dtos.HeatCellsView
	if u.readHeatCache(ctx, key, &cached) {
		return &cached, nil
	}
	resolution, cells, err := u.boxCells(box, resolution)
	if err != nil {
		return nil, err
	}
	sums, err := u.Heatmap.Sum(ctx, repository.HeatmapRange{Resolution: resolution, From: from, To: to, Cells: cells, Category: query.Category})
	if err != nil {
		return nil, err
	}
	view := &dtos.HeatCellsView{Resolution: resolution, Window: dtos.HeatWindow{From: from, To: to}, Metric: metric, Cells: make([]dtos.HeatValue, 0, len(sums))}
	for _, sum := range sums {
		value := sum.Value
		if metric == MetricCount {
			value = float64(sum.Count)
		}
		if value <= 0 {
			continue
		}
		view.Max = max(view.Max, value)
		view.Cells = append(view.Cells, dtos.HeatValue{H3: u.Geo.Format(sum.Cell), Value: value})
	}
	sort.Slice(view.Cells, func(i, j int) bool { return view.Cells[i].H3 < view.Cells[j].H3 })
	u.writeHeatCache(ctx, key, view)
	return view, nil
}

func (u *Usecase) heatPeriod(givenFrom, givenTo *time.Time) (from, to time.Time, err error) {
	window := u.settings.Heatmap.Window
	current := u.now().Truncate(window)
	from, to = current.Add(-window), current
	switch {
	case givenFrom != nil && givenTo != nil:
		from, to = givenFrom.UTC(), givenTo.UTC()
	case givenFrom != nil:
		from, to = givenFrom.UTC(), u.now()
	case givenTo != nil:
		from, to = givenTo.UTC().Add(-window), givenTo.UTC()
	}
	if !from.Before(to) || to.Sub(from) > u.settings.Heatmap.MaxPeriod {
		return time.Time{}, time.Time{}, ErrHeatmapPeriod
	}
	return from.Truncate(window), ceilTo(to, window), nil
}

func ceilTo(at time.Time, step time.Duration) time.Time {
	floor := at.Truncate(step)
	if floor.Equal(at) {
		return floor
	}
	return floor.Add(step)
}

func (u *Usecase) zoomResolution(zoom int) int {
	resolution := u.settings.Heatmap.Zooms[0].Resolution
	for _, step := range u.settings.Heatmap.Zooms {
		if zoom >= step.MinZoom {
			resolution = step.Resolution
		}
	}
	return resolution
}

func (u *Usecase) storedResolutions() []int {
	stored := append([]int{entities.HeatmapResolution}, u.settings.Heatmap.Rollups...)
	slices.Sort(stored)
	slices.Reverse(stored)
	return stored
}

func (u *Usecase) boxCells(box entities.GeoBox, wanted int) (int, []int64, error) {
	for _, resolution := range u.storedResolutions() {
		if resolution > wanted {
			continue
		}
		cells, err := u.Geo.BoxCells(box, resolution, u.settings.Heatmap.MaxCells)
		if errors.Is(err, services.ErrTooManyCells) {
			continue
		}
		return resolution, cells, err
	}
	return 0, nil, ErrHeatmapArea
}

func (u *Usecase) readHeatCache(ctx context.Context, key string, dest *dtos.HeatCellsView) bool {
	if u.Cache == nil || u.settings.Heatmap.CacheTTL <= 0 {
		return false
	}
	found, err := u.Cache.Get(ctx, key, dest)
	if err != nil {
		logger.FromContext(ctx).Warn("heatmap cache read failed", zap.Error(err))
		return false
	}
	return found
}

func (u *Usecase) writeHeatCache(ctx context.Context, key string, view *dtos.HeatCellsView) {
	if u.Cache == nil || u.settings.Heatmap.CacheTTL <= 0 {
		return
	}
	if err := u.Cache.Set(ctx, key, view, u.settings.Heatmap.CacheTTL); err != nil {
		logger.FromContext(ctx).Warn("heatmap cache write failed", zap.Error(err))
	}
}
