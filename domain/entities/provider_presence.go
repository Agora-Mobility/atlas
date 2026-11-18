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
	CapacityKg         *int
}

func (p *EligibleProvider) CanCarry(minimumKg int64) bool {
	if minimumKg <= 0 {
		return true
	}
	return p.CapacityKg != nil && int64(*p.CapacityKg) >= minimumKg
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
	presenceTTLMultiplier               = 2.5
)

type Cadence struct {
	Interval time.Duration
	Distance int
	SendsGPS bool
}

// CadencePolicy is how often the provider app reports its position in each
// phase: every Interval or every Distance meters, whichever comes first. It is
// configuration (CADENCE_*, ADR 0023); DefaultCadencePolicy holds the values
// the API shipped with.
type CadencePolicy struct {
	IdleInterval     time.Duration
	IdleDistance     int
	ApproachInterval time.Duration
	ApproachDistance int
	TripInterval     time.Duration
	TripDistance     int
	// SlowdownFactor multiplies the interval when the battery is low or the network weak
	SlowdownFactor int
}

func DefaultCadencePolicy() CadencePolicy {
	return CadencePolicy{
		IdleInterval: 30 * time.Second, IdleDistance: 150,
		ApproachInterval: 7 * time.Second, ApproachDistance: 30,
		TripInterval: 10 * time.Second, TripDistance: 50,
		SlowdownFactor: 2,
	}
}

func (p CadencePolicy) For(phase ProviderPhase, batteryLow, networkWeak bool) Cadence {
	cadence := Cadence{SendsGPS: true}
	switch phase {
	case PhaseIdle:
		cadence.Interval, cadence.Distance = p.IdleInterval, p.IdleDistance
	case PhaseOffered, PhaseEnRoute:
		cadence.Interval, cadence.Distance = p.ApproachInterval, p.ApproachDistance
	case PhaseInTrip:
		cadence.Interval, cadence.Distance = p.TripInterval, p.TripDistance
	default:
		return Cadence{}
	}
	if (batteryLow || networkWeak) && p.SlowdownFactor > 1 {
		cadence.Interval *= time.Duration(p.SlowdownFactor)
	}
	return cadence
}

// CadenceFor is the cadence of the default policy.
func CadenceFor(phase ProviderPhase, batteryLow, networkWeak bool) Cadence {
	return DefaultCadencePolicy().For(phase, batteryLow, networkWeak)
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
