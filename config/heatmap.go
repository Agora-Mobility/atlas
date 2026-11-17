package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	heatmapBaseResolution = 9
	dayLength             = 24 * time.Hour
)

type HeatmapZoom struct {
	MinZoom    int
	Resolution int
}

type HeatmapConfig struct {
	Interval        time.Duration `mapstructure:"HEATMAP_INTERVAL"`
	Window          time.Duration `mapstructure:"HEATMAP_WINDOW"`
	LookbackWindows int           `mapstructure:"HEATMAP_LOOKBACK_WINDOWS"`
	RollupList      []string      `mapstructure:"HEATMAP_ROLLUP_RESOLUTIONS"`
	SelfWeight      float64       `mapstructure:"HEATMAP_SELF_WEIGHT"`
	NeighborWeight  float64       `mapstructure:"HEATMAP_NEIGHBOR_WEIGHT"`
	ZoomList        []string      `mapstructure:"HEATMAP_ZOOM_RESOLUTIONS"`
	MaxCells        int           `mapstructure:"HEATMAP_MAX_CELLS"`
	MaxPeriod       time.Duration `mapstructure:"HEATMAP_MAX_PERIOD"`
	CacheTTL        time.Duration `mapstructure:"HEATMAP_CACHE_TTL"`
	Retention       time.Duration `mapstructure:"HEATMAP_RETENTION"`

	Rollups []int         `mapstructure:"-"`
	Zooms   []HeatmapZoom `mapstructure:"-"`
}

func (h *HeatmapConfig) prepare() []error {
	var errs []error
	if h.Interval <= 0 || h.Window <= 0 || dayLength%h.Window != 0 {
		errs = append(errs, errors.New("HEATMAP_INTERVAL must be positive and HEATMAP_WINDOW must divide a day"))
	}
	if h.LookbackWindows < 1 || h.MaxCells < 1 || h.MaxPeriod <= 0 {
		errs = append(errs, errors.New("HEATMAP_LOOKBACK_WINDOWS, HEATMAP_MAX_CELLS and HEATMAP_MAX_PERIOD must be positive"))
	}
	if h.SelfWeight < 0 || h.NeighborWeight < 0 || h.SelfWeight+h.NeighborWeight == 0 {
		errs = append(errs, errors.New("HEATMAP_SELF_WEIGHT and HEATMAP_NEIGHBOR_WEIGHT cannot be negative or both zero"))
	}
	if h.CacheTTL < 0 || h.Retention < 0 || (h.Retention > 0 && h.Retention < h.MaxPeriod) {
		errs = append(errs, errors.New("HEATMAP_CACHE_TTL cannot be negative and HEATMAP_RETENTION must be 0 or cover HEATMAP_MAX_PERIOD"))
	}
	rollups, err := parseRollups(h.RollupList)
	if err != nil {
		return append(errs, err)
	}
	zooms, err := parseZooms(h.ZoomList, append([]int{heatmapBaseResolution}, rollups...))
	if err != nil {
		return append(errs, err)
	}
	h.Rollups, h.Zooms = rollups, zooms
	return errs
}

func parseRollups(values []string) ([]int, error) {
	rollups := []int{}
	for _, raw := range compact(values) {
		resolution, err := strconv.Atoi(raw)
		if err != nil || resolution < 0 || resolution >= heatmapBaseResolution || slices.Contains(rollups, resolution) {
			return nil, fmt.Errorf("HEATMAP_ROLLUP_RESOLUTIONS must list distinct resolutions from 0 to %d, got %q", heatmapBaseResolution-1, raw)
		}
		rollups = append(rollups, resolution)
	}
	slices.Sort(rollups)
	slices.Reverse(rollups)
	return rollups, nil
}

func parseZooms(values []string, stored []int) ([]HeatmapZoom, error) {
	var zooms []HeatmapZoom
	for _, raw := range compact(values) {
		zoomText, resolutionText, found := strings.Cut(raw, ":")
		zoom, zoomErr := strconv.Atoi(strings.TrimSpace(zoomText))
		resolution, resolutionErr := strconv.Atoi(strings.TrimSpace(resolutionText))
		if !found || zoomErr != nil || resolutionErr != nil || !slices.Contains(stored, resolution) {
			return nil, fmt.Errorf("HEATMAP_ZOOM_RESOLUTIONS entries are min_zoom:resolution with a stored resolution, got %q", raw)
		}
		if len(zooms) > 0 && zoom <= zooms[len(zooms)-1].MinZoom {
			return nil, errors.New("HEATMAP_ZOOM_RESOLUTIONS must be sorted by zoom")
		}
		zooms = append(zooms, HeatmapZoom{MinZoom: zoom, Resolution: resolution})
	}
	if len(zooms) == 0 || zooms[0].MinZoom != 0 {
		return nil, errors.New("HEATMAP_ZOOM_RESOLUTIONS must start at zoom 0")
	}
	return zooms, nil
}
