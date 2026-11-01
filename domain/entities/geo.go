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
