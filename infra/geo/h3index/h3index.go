package h3index

import (
	"fmt"
	"math"

	"github.com/uber/h3-go/v4"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

const (
	LiveLongRange = 5
	Coverage      = 6
	LiveUrban     = 7
	Analytics     = 8

	maxSupportedResolution = 15
	ringSafetyMargin       = 1
	ringWidthInEdges       = 1.5
	worstCaseEdgeRatio     = 0.74
)

var measuredMinEdgeKm = map[int]float64{
	LiveLongRange: 7.32,
	Coverage:      2.90,
	LiveUrban:     1.05,
	Analytics:     0.41,
}

var globalAverageEdgeKm = map[int]float64{
	0: 1281.256011, 1: 483.0568391, 2: 182.5129565, 3: 68.97922179, 4: 26.07175968,
	5: 9.854090990, 6: 3.724532667, 7: 1.406475763, 8: 0.531414010, 9: 0.200786148,
	10: 0.075863783, 11: 0.028663897, 12: 0.010830188, 13: 0.004092010, 14: 0.001546100, 15: 0.000584169,
}

type Index struct{}

func New() Index {
	return Index{}
}

func (Index) Cell(point entities.GeoPoint, resolution int) (int64, error) {
	cell, err := h3.LatLngToCell(h3.NewLatLng(point.Lat, point.Lng), resolution)
	if err != nil {
		return 0, fmt.Errorf("h3 cell: %w", err)
	}
	return int64(cell), nil
}

func (Index) Disk(cell int64, rings int) ([]int64, error) {
	cells, err := h3.GridDisk(h3.Cell(cell), rings)
	if err != nil {
		return nil, fmt.Errorf("h3 disk: %w", err)
	}
	return toInt64s(cells), nil
}

func (Index) RingsFor(radiusKm float64, resolution int) int {
	if radiusKm <= 0 {
		return ringSafetyMargin
	}
	return int(math.Ceil(radiusKm/(ringWidthInEdges*minEdgeKm(resolution)))) + ringSafetyMargin
}

func (Index) CellCountFor(rings int) int {
	return 3*rings*rings + 3*rings + 1
}

func (i Index) Covering(center entities.GeoPoint, radiusKm float64, resolution int) ([]int64, error) {
	cell, err := i.Cell(center, resolution)
	if err != nil {
		return nil, err
	}
	return i.Disk(cell, i.RingsFor(radiusKm, resolution))
}

func (Index) Center(cell int64) (entities.GeoPoint, error) {
	center, err := h3.CellToLatLng(h3.Cell(cell))
	if err != nil {
		return entities.GeoPoint{}, fmt.Errorf("h3 center: %w", err)
	}
	return entities.GeoPoint{Lat: center.Lat, Lng: center.Lng}, nil
}

func (Index) Format(cell int64) string {
	return h3.Cell(cell).String()
}

func (Index) InteriorCells(polygons []entities.GeoPolygon, resolution int) ([]int64, error) {
	filled := map[int64]struct{}{}
	for _, polygon := range polygons {
		cells, err := h3.PolygonToCells(toH3Polygon(polygon), resolution)
		if err != nil {
			return nil, fmt.Errorf("h3 polygon: %w", err)
		}
		for _, cell := range cells {
			filled[int64(cell)] = struct{}{}
		}
	}

	interior := make([]int64, 0, len(filled))
	for cell := range filled {
		neighbors, err := h3.GridDisk(h3.Cell(cell), 1)
		if err != nil {
			return nil, fmt.Errorf("h3 neighbors: %w", err)
		}
		if allFilled(neighbors, filled) {
			interior = append(interior, cell)
		}
	}
	return interior, nil
}

func allFilled(cells []h3.Cell, filled map[int64]struct{}) bool {
	for _, cell := range cells {
		if _, ok := filled[int64(cell)]; !ok {
			return false
		}
	}
	return true
}

func minEdgeKm(resolution int) float64 {
	if edge, ok := measuredMinEdgeKm[resolution]; ok {
		return edge
	}
	if resolution < 0 || resolution > maxSupportedResolution {
		return globalAverageEdgeKm[LiveUrban] * worstCaseEdgeRatio
	}
	return globalAverageEdgeKm[resolution] * worstCaseEdgeRatio
}

func toH3Polygon(polygon entities.GeoPolygon) h3.GeoPolygon {
	out := h3.GeoPolygon{GeoLoop: toH3Loop(polygon.Outer)}
	for _, hole := range polygon.Holes {
		out.Holes = append(out.Holes, toH3Loop(hole))
	}
	return out
}

func toH3Loop(points []entities.GeoPoint) h3.GeoLoop {
	loop := make(h3.GeoLoop, 0, len(points))
	for _, p := range points {
		loop = append(loop, h3.NewLatLng(p.Lat, p.Lng))
	}
	return loop
}

func toInt64s(cells []h3.Cell) []int64 {
	out := make([]int64, 0, len(cells))
	for _, cell := range cells {
		if cell != 0 {
			out = append(out, int64(cell))
		}
	}
	return out
}
