package entities

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	bpsDenominator        = 10_000
	BoostTenPercentBps    = 1_000
	BoostTwentyPercentBps = 2_000
	metersPerKm           = 1000
	secondsPerMinute      = 60
	maxPinDriftMeters     = 300
	maxFreeWaitSeconds    = 86_400
)

var (
	ErrBoostNotAllowed = errors.New("this request cannot raise its offer")
	ErrBoostNotHigher  = errors.New("the raise must be higher than the current one")
)

type ServiceTariff struct {
	ID                uuid.UUID
	ServiceID         uuid.UUID
	ZoneID            *uuid.UUID
	Currency          string
	BaseFeeCents      int64
	PerKmFeeCents     int64
	PerMinuteFeeCents int64
	MinimumFeeCents   int64
	ZoneFeeCents      int64
	NightFeeCents     int64
	StopFeeCents      int64
	NightStartHour    int
	NightEndHour      int
	FreeWaitSeconds   int
	CommissionBps     int
	ValidFrom         time.Time
	ValidUntil        *time.Time
}

func (t *ServiceTariff) Validate() error {
	fees := []int64{t.BaseFeeCents, t.PerKmFeeCents, t.PerMinuteFeeCents, t.MinimumFeeCents, t.ZoneFeeCents, t.NightFeeCents, t.StopFeeCents}
	for _, fee := range fees {
		if fee < 0 || fee > MaxFeeCents {
			return validationError("fees", "Os valores da tarifa não são válidos.")
		}
	}
	if len(t.Currency) != 3 {
		return validationError("currency", "A moeda tem de ter 3 letras.")
	}
	if t.CommissionBps < 0 || t.CommissionBps > bpsDenominator {
		return validationError("commission_bps", "A comissão tem de estar entre 0 e 10000.")
	}
	if t.NightStartHour < 0 || t.NightStartHour > 23 || t.NightEndHour < 0 || t.NightEndHour > 23 {
		return validationError("night", "As horas da noite têm de estar entre 0 e 23.")
	}
	if t.FreeWaitSeconds < 0 || t.FreeWaitSeconds > maxFreeWaitSeconds {
		return validationError("free_wait_seconds", "A espera gratuita tem de estar entre 0 e 86400 segundos.")
	}
	return nil
}

func (t *ServiceTariff) FareFor(distanceMeters int, duration time.Duration, localTime time.Time) int64 {
	distanceFee := roundedDivide(int64(distanceMeters)*t.PerKmFeeCents, metersPerKm)
	timeFee := roundedDivide(int64(duration.Seconds())*t.PerMinuteFeeCents, secondsPerMinute)
	fare := t.BaseFeeCents + distanceFee + timeFee
	if fare < t.MinimumFeeCents {
		fare = t.MinimumFeeCents
	}
	fare += t.ZoneFeeCents
	if t.isNight(localTime) {
		fare += t.NightFeeCents
	}
	return fare
}

func (t *ServiceTariff) FareForRoute(distanceMeters int, duration time.Duration, localTime time.Time, stops int) int64 {
	return t.FareFor(distanceMeters, duration, localTime) + int64(stops)*t.StopFeeCents
}

func (t *ServiceTariff) Quote(baseCents int64, demand DemandQuote) QuotedFare {
	return QuoteFare(baseCents, t.CommissionBps, demand)
}

func (t *ServiceTariff) CommissionFor(fareCents int64) int64 {
	return roundedDivide(fareCents*int64(t.CommissionBps), bpsDenominator)
}

func (t *ServiceTariff) WaitingFeeFor(waited time.Duration) int64 {
	chargeable := int64(waited.Seconds()) - int64(t.FreeWaitSeconds)
	if chargeable <= 0 {
		return 0
	}
	return roundedDivide(chargeable*t.PerMinuteFeeCents, secondsPerMinute)
}

func (t *ServiceTariff) isNight(local time.Time) bool {
	if t.NightFeeCents == 0 || t.NightStartHour == t.NightEndHour {
		return false
	}
	hour := local.Hour()
	if t.NightStartHour < t.NightEndHour {
		return hour >= t.NightStartHour && hour < t.NightEndHour
	}
	return hour >= t.NightStartHour || hour < t.NightEndHour
}

func roundedDivide(value, divisor int64) int64 {
	return int64(math.Round(float64(value) / float64(divisor)))
}

type FareEstimate struct {
	ID                 uuid.UUID
	CustomerID         uuid.UUID
	TariffID           uuid.UUID
	ServiceID          uuid.UUID
	Origin             GeoPoint
	Destination        *GeoPoint
	Stops              []GeoPoint
	ScheduledFor       *time.Time
	UsedAt             *time.Time
	VehicleTypeID      *uuid.UUID
	Details            json.RawMessage
	DistanceMeters     int
	DurationSeconds    int
	RouteSource        string
	RouteGeometry      string
	TotalCents         int64
	CommissionCents    int64
	DemandFactorBps    int
	DemandCents        int64
	DemandCommissioned bool
	Currency           string
	ExpiresAt          time.Time
	CreatedAt          time.Time
}

func (e *FareEstimate) Demand() DemandQuote {
	if e.DemandFactorBps < NeutralDemandFactorBps {
		return NeutralDemand()
	}
	return DemandQuote{FactorBps: e.DemandFactorBps, Commissions: e.DemandCommissioned}
}

func (e *FareEstimate) Apply(quote QuotedFare) {
	e.TotalCents, e.CommissionCents = quote.TotalCents, quote.CommissionCents
	e.DemandFactorBps, e.DemandCents, e.DemandCommissioned = quote.DemandFactorBps, quote.DemandCents, quote.DemandCommissioned
}

func (e *FareEstimate) IsExpired(now time.Time) bool {
	return !now.Before(e.ExpiresAt)
}

func (e *FareEstimate) Matches(pickup GeoPoint, destination *GeoPoint) bool {
	if DistanceMeters(e.Origin, pickup) > maxPinDriftMeters {
		return false
	}
	if (e.Destination == nil) != (destination == nil) {
		return false
	}
	return e.Destination == nil || DistanceMeters(*e.Destination, *destination) <= maxPinDriftMeters
}

func (e *FareEstimate) MatchesRoute(pickup GeoPoint, destination *GeoPoint, stops []GeoPoint, scheduledFor *time.Time) bool {
	if !e.Matches(pickup, destination) || len(e.Stops) != len(stops) {
		return false
	}
	for i := range stops {
		if DistanceMeters(e.Stops[i], stops[i]) > maxPinDriftMeters {
			return false
		}
	}
	if (e.ScheduledFor == nil) != (scheduledFor == nil) {
		return false
	}
	return e.ScheduledFor == nil || e.ScheduledFor.Equal(*scheduledFor)
}

type FareAdjustmentReason string

const (
	AdjustmentBoost       FareAdjustmentReason = "boost"
	AdjustmentWaiting     FareAdjustmentReason = "waiting"
	AdjustmentRouteChange FareAdjustmentReason = "route_change"
	AdjustmentDemand      FareAdjustmentReason = "demand"
)

type FareAdjustment struct {
	RequestID   uuid.UUID
	Reason      FareAdjustmentReason
	AmountCents int64
	ActorID     *uuid.UUID
	CreatedAt   time.Time
}
