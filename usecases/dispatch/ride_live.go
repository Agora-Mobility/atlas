package dispatch

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

// RegisterRidePushToken keeps the token the customer app got for the lock screen
// of this request: the FCM token on Android, the push token of the Live Activity
// on iOS. A new token of the same platform replaces the old one.
func (u *Usecase) RegisterRidePushToken(ctx context.Context, customerID, requestID uuid.UUID, in dtos.RidePushTokenInput) error {
	request, role, err := u.participant(ctx, customerID, requestID)
	if err != nil {
		return err
	}
	if role != senderCustomer {
		return ErrRequestNotFound
	}
	if request.Status.IsTerminal() {
		return entities.ErrRideLiveClosed
	}
	return u.RideTokens.Save(ctx, entities.RidePushToken{RequestID: requestID, UserID: customerID, Platform: in.Platform, Token: in.Token})
}

// pushRideLivePosition sends the position of a tracked ride to the lock screen,
// at most once per RideLiveInterval, without making the driver wait for it.
func (u *Usecase) pushRideLivePosition(ctx context.Context, trip entities.ActiveTrip, position entities.ProviderPosition, eta *int) {
	if u.RideLive == nil || u.settings.RideLiveInterval <= 0 {
		return
	}
	claimed, err := u.Locator.ClaimRideLivePush(ctx, trip.RequestID, u.settings.RideLiveInterval)
	if err != nil || !claimed {
		return
	}
	point := position.Point
	state := entities.RideLiveState{
		RequestID: trip.RequestID, Status: trip.Status, Phase: entities.RidePhaseOf(trip.Status, trip.Arrived),
		ETASecond: eta, Driver: &point, Heading: position.Heading, At: position.RecordedAt,
	}
	if trip.Target != nil {
		meters, _ := entities.StraightETA(point, *trip.Target, u.settings.RoadFactor, u.settings.AverageSpeedKmh)
		state.DistanceM = &meters
	}
	u.inBackground(ctx, "ride live position", func(ctx context.Context) error {
		return u.RideLive.Position(ctx, state)
	})
}

func (u *Usecase) refreshRideLive(ctx context.Context, requestID uuid.UUID) {
	if u.RideLive == nil {
		return
	}
	u.inBackground(ctx, "ride live refresh", func(ctx context.Context) error {
		return u.RideLive.Refresh(ctx, requestID)
	})
}

// inBackground runs a send that nobody waits for. When every slot is taken it
// is dropped: the next update carries the same information.
func (u *Usecase) inBackground(ctx context.Context, what string, send func(context.Context) error) {
	detached := context.WithoutCancel(ctx)
	select {
	case u.sends <- struct{}{}:
	default:
		logger.FromContext(detached).Warn("send dropped, too many in flight", zap.String("what", what))
		return
	}
	go func() {
		defer func() { <-u.sends }()
		sendCtx, cancel := context.WithTimeout(detached, u.settings.ChannelTimeout)
		defer cancel()
		if err := send(sendCtx); err != nil {
			logger.FromContext(detached).Warn("send failed", zap.String("what", what), zap.Error(err))
		}
	}()
}
