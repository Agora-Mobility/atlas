package entities

import "math"

const (
	earthRadiusMeters = 6_371_008.8
	maxLatitude       = 90.0
	maxLongitude      = 180.0
)

type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (p GeoPoint) IsValid() bool {
	if p.Lat == 0 && p.Lng == 0 {
		return false
	}
	return math.Abs(p.Lat) <= maxLatitude && math.Abs(p.Lng) <= maxLongitude
}

func (p GeoPoint) Validate(field string) error {
	if !p.IsValid() {
		return validationError(field, "A localização não é válida.")
	}
	return nil
}

type GeoPolygon struct {
	Outer []GeoPoint
	Holes [][]GeoPoint
}

func DistanceMeters(a, b GeoPoint) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	deltaLat := lat2 - lat1
	deltaLng := radians(b.Lng - a.Lng)
	h := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLng/2)*math.Sin(deltaLng/2)
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))
}

func radians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

// AnalyticsResolution is the H3 resolution of the cells kept on each request.
const AnalyticsResolution = 8

const HeatmapResolution = 9

type GeoBox struct {
	MinLng float64
	MinLat float64
	MaxLng float64
	MaxLat float64
}

func (b GeoBox) IsValid() bool {
	inRange := math.Abs(b.MinLat) <= maxLatitude && math.Abs(b.MaxLat) <= maxLatitude &&
		math.Abs(b.MinLng) <= maxLongitude && math.Abs(b.MaxLng) <= maxLongitude
	return inRange && b.MinLat < b.MaxLat && b.MinLng < b.MaxLng && b.MaxLng-b.MinLng < maxLongitude
}

func (b GeoBox) Polygon() GeoPolygon {
	return GeoPolygon{Outer: []GeoPoint{
		{Lat: b.MinLat, Lng: b.MinLng},
		{Lat: b.MinLat, Lng: b.MaxLng},
		{Lat: b.MaxLat, Lng: b.MaxLng},
		{Lat: b.MaxLat, Lng: b.MinLng},
	}}
}

func (b GeoBox) AreaKm2() float64 {
	radiusKm := earthRadiusMeters / 1000
	return radiusKm * radiusKm * radians(b.MaxLng-b.MinLng) * math.Abs(math.Sin(radians(b.MaxLat))-math.Sin(radians(b.MinLat)))
}

func (b GeoBox) RoundedOut(step float64) GeoBox {
	return GeoBox{
		MinLng: math.Max(-maxLongitude, math.Floor(b.MinLng/step)*step),
		MinLat: math.Max(-maxLatitude, math.Floor(b.MinLat/step)*step),
		MaxLng: math.Min(maxLongitude, math.Ceil(b.MaxLng/step)*step),
		MaxLat: math.Min(maxLatitude, math.Ceil(b.MaxLat/step)*step),
	}
}
