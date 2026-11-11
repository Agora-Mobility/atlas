package dispatch

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
)

const (
	housekeepingEvery   = time.Minute
	lostProviderBatch   = 200
	lostProviderPadding = 2 * time.Minute
)

func (u *Usecase) RunWatcher(ctx context.Context) {
	log := logger.FromContext(ctx)
	log.Info("offer watcher started")
	ticker := time.NewTicker(u.settings.WatcherInterval)
	defer ticker.Stop()
	housekeeping := time.NewTicker(housekeepingEvery)
	defer housekeeping.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("offer watcher stopped")
			return
		case <-ticker.C:
			if err := u.Tick(ctx); err != nil && ctx.Err() == nil {
				log.Warn("offer watcher tick failed", zap.Error(err))
			}
		case <-housekeeping.C:
			if err := u.Housekeep(ctx); err != nil && ctx.Err() == nil {
				log.Warn("dispatch housekeeping failed", zap.Error(err))
			}
			if err := u.RefreshDemand(ctx); err != nil && ctx.Err() == nil {
				log.Warn("demand refresh failed", zap.Error(err))
			}
		}
	}
}

func (u *Usecase) Tick(ctx context.Context) error {
	now := u.now()
	var offers, requests, reminders []uuid.UUID
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if offers, err = u.Offers.Due(ctx, now, u.settings.WatcherBatch); err != nil {
			return err
		}
		if requests, err = u.Requests.DueRequests(ctx, now, u.settings.WatcherBatch); err != nil {
			return err
		}
		reminders, err = u.Requests.DueReminders(ctx, now, u.settings.WatcherBatch)
		return err
	})
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)
	for _, id := range offers {
		if err := u.runOfferStep(ctx, id); err != nil {
			log.Warn("offer step failed", zap.String("offer_id", id.String()), zap.Error(err))
		}
	}
	for _, id := range requests {
		if err := u.Advance(ctx, id); err != nil {
			log.Warn("request search failed", zap.String("request_id", id.String()), zap.Error(err))
		}
	}
	for _, id := range reminders {
		if err := u.sendReminder(ctx, id); err != nil {
			log.Warn("reminder failed", zap.String("request_id", id.String()), zap.Error(err))
		}
	}
	return nil
}

func (u *Usecase) runOfferStep(ctx context.Context, offerID uuid.UUID) error {
	preview, err := u.Offers.Find(ctx, offerID)
	if err != nil || preview == nil {
		return err
	}
	var notice *OfferNotice
	var channel string
	var release *reservation
	again := false
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		notice, channel, release, again = nil, "", nil, false
		request, err := u.Requests.TryLock(ctx, preview.RequestID)
		if err != nil || request == nil {
			return err
		}
		offer, err := u.Offers.FindForUpdate(ctx, offerID)
		if err != nil || offer == nil {
			return err
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		now := u.now()
		step, nextChannel := offer.DueStep(policy, now)
		switch step {
		case entities.OfferStepFallback:
			channel = nextChannel
			remaining := offer.RemainingSeconds(now)
			notice = &OfferNotice{
				OfferID: offer.ID, RequestID: request.ID, ProviderID: offer.ProviderID, Category: request.CategorySlug,
				ExpiresIn: remaining, Reference: request.Pickup.Reference, DistanceM: offer.DistanceM,
			}
			offer.AdvanceFallback(policy)
			return u.Offers.Update(ctx, offer)
		case entities.OfferStepGiveUp:
			release = &reservation{providerID: offer.ProviderID, offerID: offer.ID}
			again, err = u.endOffer(ctx, request, offer, policy, entities.OfferNoResponse, "no_ack", now)
			return err
		case entities.OfferStepExpire:
			release = &reservation{providerID: offer.ProviderID, offerID: offer.ID}
			again, err = u.endOffer(ctx, request, offer, policy, entities.OfferExpired, "deadline", now)
			return err
		default:
			if offer.IsOpen() {
				offer.Reschedule(policy)
			} else {
				offer.NextActionAt = nil
			}
			return u.Offers.Update(ctx, offer)
		}
	})
	if err != nil {
		return err
	}
	if release != nil {
		u.releaseAll(ctx, []reservation{*release})
	}
	if notice != nil {
		u.dispatchChannel(ctx, channel, *notice)
	}
	if again {
		return u.Advance(ctx, preview.RequestID)
	}
	return nil
}

func (u *Usecase) endOffer(ctx context.Context, request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, status entities.OfferStatus, reason string, now time.Time) (bool, error) {
	offer.Close(status, now)
	if err := u.Offers.Update(ctx, offer); err != nil {
		return false, err
	}
	businessmetrics.OfferOutcomes.WithLabelValues(request.CategorySlug, string(status)).Inc()
	event := u.offerEvent(events.OfferExpired, request, offer, now)
	event.Reason = reason
	if err := u.publish(ctx, event); err != nil {
		return false, err
	}
	if request.Status != entities.RequestOffered {
		return false, nil
	}
	return u.reconcile(ctx, request, policy, now)
}

func (u *Usecase) dispatchChannel(ctx context.Context, channel string, notice OfferNotice) {
	detached := context.WithoutCancel(ctx)
	u.sends <- struct{}{}
	go func() {
		defer func() { <-u.sends }()
		sendCtx, cancel := context.WithTimeout(detached, u.settings.ChannelTimeout)
		defer cancel()
		result := "ok"
		if err := u.Channels.SendOffer(sendCtx, channel, notice); err != nil {
			result = "error"
			logger.FromContext(detached).Warn("offer fallback failed", zap.String("channel", channel), zap.String("offer_id", notice.OfferID.String()), zap.Error(err))
		}
		businessmetrics.OfferChannelSends.WithLabelValues(notice.Category, channel, result).Inc()
	}()
}

func (u *Usecase) sendReminder(ctx context.Context, requestID uuid.UUID) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.TryLock(ctx, requestID)
		if err != nil || request == nil {
			return err
		}
		now := u.now()
		if request.ReminderAt == nil || request.ReminderAt.After(now) {
			return nil
		}
		request.ReminderAt = nil
		request.UpdatedAt = now
		if err := u.Requests.Update(ctx, request, request.Version); err != nil {
			return err
		}
		if request.Status.IsTerminal() {
			return nil
		}
		return u.publish(ctx, events.ReminderDueV1{
			RequestID: request.ID, CustomerID: request.CustomerID, ProviderID: request.ProviderID, Category: request.CategorySlug,
			ScheduledFor: request.ScheduledFor, OccurredAt: now,
		})
	})
}

func (u *Usecase) Housekeep(ctx context.Context) error {
	if u.settings.HistoryRetention > 0 {
		if _, err := u.Providers.PurgeLocationSamples(ctx, u.now().Add(-u.settings.HistoryRetention)); err != nil {
			return err
		}
	}
	return u.alertLostProviders(ctx)
}

func (u *Usecase) alertLostProviders(ctx context.Context) error {
	requests, err := u.Requests.ListWithProviderOnTheWay(ctx, lostProviderBatch)
	if err != nil {
		return err
	}
	now := u.now()
	for _, request := range requests {
		if request.AcceptedAt == nil || now.Sub(*request.AcceptedAt) < lostProviderPadding {
			continue
		}
		position, err := u.Locator.LastPosition(ctx, *request.ProviderID)
		if err != nil {
			return err
		}
		if position != nil {
			continue
		}
		claimed, err := u.Locator.ClaimLostAlert(ctx, request.ID, lostAlertEvery)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
			return u.publish(ctx, events.OperatorAlertedV1{
				RequestID: request.ID, CustomerID: request.CustomerID, Category: request.CategorySlug, Reason: "provider_lost", OccurredAt: now,
			})
		})
		if err != nil {
			return err
		}
	}
	return nil
}
