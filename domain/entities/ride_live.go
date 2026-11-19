package entities

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

var ErrRideLiveClosed = errors.New("the ride has ended")

// RidePushToken is what the customer app registers for one request so the lock
// screen follows the ride: the FCM token on Android, the push token of the Live
// Activity on iOS.
type RidePushToken struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	Platform  string
	Token     string
}

// RidePhase is what the lock screen shows; it tells the arrival apart from the
// approach, which share the request status.
type RidePhase string

const (
	RidePhaseSearching RidePhase = "searching"
	RidePhaseEnRoute   RidePhase = "en_route"
	RidePhaseArrived   RidePhase = "arrived"
	RidePhaseInTrip    RidePhase = "in_trip"
	RidePhaseCompleted RidePhase = "completed"
	RidePhaseCancelled RidePhase = "cancelled"
	RidePhaseExpired   RidePhase = "expired"
)

func RidePhaseOf(status RequestStatus, arrived bool) RidePhase {
	switch status {
	case RequestAccepted, RequestProviderEnRoute:
		if arrived {
			return RidePhaseArrived
		}
		return RidePhaseEnRoute
	case RequestInProgress:
		return RidePhaseInTrip
	case RequestCompleted:
		return RidePhaseCompleted
	case RequestCancelled:
		return RidePhaseCancelled
	case RequestExpired:
		return RidePhaseExpired
	default:
		return RidePhaseSearching
	}
}

// RideLiveState is one update of the ride on the lock screen.
type RideLiveState struct {
	RequestID uuid.UUID
	Status    RequestStatus
	Phase     RidePhase
	ETASecond *int
	DistanceM *int
	Driver    *GeoPoint
	Heading   *float64
	At        time.Time
}

// StraightETA estimates the road distance and the time from a to b without a
// routing call: the straight line times the road factor, at the average speed.
func StraightETA(a, b GeoPoint, roadFactor, averageSpeedKmh float64) (meters, seconds int) {
	road := DistanceMeters(a, b) * roadFactor
	meters = int(math.Round(road))
	if averageSpeedKmh <= 0 {
		return meters, 0
	}
	return meters, int(road / (averageSpeedKmh * 1000 / 3600))
}
