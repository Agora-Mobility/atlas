package services

import (
	"context"
	"errors"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrRoutingUnavailable = errors.New("routing provider unavailable")

type RouteSource string

const (
	RouteSourceMapbox   RouteSource = "mapbox"
	RouteSourceFallback RouteSource = "fallback"
)

type Route struct {
	DistanceMeters int
	Duration       time.Duration
	Geometry       string
	Source         RouteSource
}

type IRoutingProvider interface {
	Route(ctx context.Context, from, to entities.GeoPoint, via ...entities.GeoPoint) (Route, error)
	ETAs(ctx context.Context, origins []entities.GeoPoint, to entities.GeoPoint) ([]time.Duration, error)
}

type IGeoIndex interface {
	Cell(point entities.GeoPoint, resolution int) (int64, error)
	Disk(cell int64, rings int) ([]int64, error)
	RingsFor(radiusKm float64, resolution int) int
	CellCountFor(rings int) int
	Covering(center entities.GeoPoint, radiusKm float64, resolution int) ([]int64, error)
	Center(cell int64) (entities.GeoPoint, error)
	Format(cell int64) string
	InteriorCells(polygons []entities.GeoPolygon, resolution int) ([]int64, error)
}
