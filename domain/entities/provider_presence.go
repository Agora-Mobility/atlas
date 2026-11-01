package entities

import (
	"time"

	"github.com/google/uuid"
)

type EligibleProvider struct {
	UserID             uuid.UUID
	Phone              string
	Base               *GeoPoint
	ServiceRadiusKm    *float64
	LastJobCompletedAt *time.Time
	CreatedAt          time.Time
}

type LiveIndex struct {
	CategoryID uuid.UUID
	Slug       string
	Resolution int
}

type ProviderPhase string

const (
	PhaseOffline          ProviderPhase = "offline"
	PhaseIdle             ProviderPhase = "idle"
	PhaseOffered          ProviderPhase = "offered"
	PhaseEnRoute          ProviderPhase = "en_route"
	PhaseInTrip           ProviderPhase = "in_trip"
	PhaseOnSite           ProviderPhase = "on_site"
	onlineInterval                      = 30 * time.Second
	onlineDistance                      = 150
	approachInterval                    = 7 * time.Second
	approachDistance                    = 30
	tripInterval                        = 10 * time.Second
	tripDistance                        = 50
	cadenceSlowdownFactor               = 2
	presenceTTLMultiplier               = 2.5
)

type Cadence struct {
	Interval time.Duration
	Distance int
	SendsGPS bool
}

func CadenceFor(phase ProviderPhase, batteryLow, networkWeak bool) Cadence {
	cadence := Cadence{SendsGPS: true}
	switch phase {
	case PhaseIdle:
		cadence.Interval, cadence.Distance = onlineInterval, onlineDistance
	case PhaseOffered, PhaseEnRoute:
		cadence.Interval, cadence.Distance = approachInterval, approachDistance
	case PhaseInTrip:
		cadence.Interval, cadence.Distance = tripInterval, tripDistance
	default:
		return Cadence{}
	}
	if batteryLow || networkWeak {
		cadence.Interval *= cadenceSlowdownFactor
	}
	return cadence
}

func (c Cadence) PresenceTTL() time.Duration {
	return time.Duration(float64(c.Interval) * presenceTTLMultiplier)
}

type ProviderPosition struct {
	ProviderID uuid.UUID
	Point      GeoPoint
	Heading    *float64
	AccuracyM  float64
	RecordedAt time.Time
}

type PlausibilityLimits struct {
	MaxAccuracyM float64
	MaxSpeedMps  float64
}

func (l PlausibilityLimits) Accepts(next ProviderPosition, previous *ProviderPosition) bool {
	if !next.Point.IsValid() || next.AccuracyM > l.MaxAccuracyM {
		return false
	}
	if previous == nil {
		return true
	}
	elapsed := next.RecordedAt.Sub(previous.RecordedAt).Seconds()
	if elapsed <= 0 {
		return false
	}
	return DistanceMeters(previous.Point, next.Point)/elapsed <= l.MaxSpeedMps
}

type ActiveTrip struct {
	RequestID  uuid.UUID     `json:"request_id"`
	CustomerID uuid.UUID     `json:"customer_id"`
	Status     RequestStatus `json:"status"`
	Target     *GeoPoint     `json:"target,omitempty"`
	Tracked    bool          `json:"tracked"`
}

func (t ActiveTrip) Phase() ProviderPhase {
	switch t.Status {
	case RequestAccepted, RequestProviderEnRoute:
		return PhaseEnRoute
	case RequestInProgress:
		if t.Tracked {
			return PhaseInTrip
		}
		return PhaseOnSite
	default:
		return PhaseIdle
	}
}

func customerValidation(field string, err error) error {
	return validationError(field, err.Error())
}
