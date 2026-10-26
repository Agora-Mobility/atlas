// Package valueobjects holds immutable, self-validating domain values.
package valueobjects

import "github.com/Agora-Mobility/atlas/domain/validators"

// GeoPoint is a WGS84 coordinate (SRID 4326).
type GeoPoint struct {
	Latitude  float64
	Longitude float64
}

func NewGeoPoint(latitude, longitude float64) (GeoPoint, error) {
	p := GeoPoint{Latitude: latitude, Longitude: longitude}
	errs := validators.Errors{}
	p.Validate(errs)
	return p, errs.Err()
}

// Validate adds coordinate errors to errs.
func (p GeoPoint) Validate(errs validators.Errors) {
	if p.Latitude < -90 || p.Latitude > 90 {
		errs.Add("latitude", "must be between -90 and 90")
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		errs.Add("longitude", "must be between -180 and 180")
	}
}
