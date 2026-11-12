package repository

import (
	"context"
	"time"
)

type HeatmapCount struct {
	Category string
	Cell     int64
	Requests int64
}

type HeatmapCell struct {
	Resolution int
	Category   string
	Cell       int64
	Count      int64
	Value      float64
}

type HeatmapRange struct {
	Resolution int
	From       time.Time
	To         time.Time
	Cells      []int64
	Category   string
}

type HeatmapSum struct {
	Cell  int64
	Count int64
	Value float64
}

type IHeatmapRepository interface {
	TryLock(ctx context.Context) (bool, error)
	CountRequests(ctx context.Context, from, to time.Time) ([]HeatmapCount, error)
	SaveWindow(ctx context.Context, windowStart time.Time, cells []HeatmapCell, at time.Time) error
	Purge(ctx context.Context, before time.Time) (int64, error)
	Sum(ctx context.Context, query HeatmapRange) ([]HeatmapSum, error)
}
