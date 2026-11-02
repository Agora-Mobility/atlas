package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func (u *Usecase) GetOffer(ctx context.Context, providerID, offerID uuid.UUID) (*dtos.OfferView, error) {
	offer, err := u.Offers.Find(ctx, offerID)
	if err != nil {
		return nil, err
	}
	if offer == nil || offer.ProviderID != providerID {
		return nil, ErrOfferNotFound
	}
	return u.offerView(ctx, offer, u.now())
}

func (u *Usecase) MarkDelivered(ctx context.Context, providerID, offerID uuid.UUID, channel string) error {
	if channel != entities.ChannelSSE && channel != entities.ChannelFCM && channel != entities.ChannelSMS {
		return ErrInvalidChannel
	}
	return u.withLockedOffer(ctx, providerID, offerID, func(ctx context.Context, request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, now time.Time) error {
		first, err := offer.MarkDelivered(channel, policy, now)
		if errors.Is(err, entities.ErrOfferClosed) || !first {
			return nil
		}
		if err != nil {
			return err
		}
		if err := u.Offers.Update(ctx, offer); err != nil {
			return err
		}
		ack := now.Sub(offer.SentAt).Seconds()
		businessmetrics.OfferAckSeconds.WithLabelValues(request.CategorySlug, channel).Observe(ack)
		businessmetrics.FirstOfferDelivered.WithLabelValues(request.CategorySlug).Observe(now.Sub(request.CreatedAt).Seconds())
		event := u.offerEvent(events.OfferDelivered, request, offer, now)
		event.Channel = channel
		event.AckSeconds = &ack
		return u.publish(ctx, event)
	})
}

func (u *Usecase) Accept(ctx context.Context, providerID, offerID uuid.UUID, version int) (*dtos.AcceptResult, error) {
	var request *entities.ServiceRequest
	var policy *entities.DispatchPolicy
	var released []reservation
	err := u.withLockedOffer(ctx, providerID, offerID, func(ctx context.Context, r *entities.ServiceRequest, offer *entities.RequestOffer, p *entities.DispatchPolicy, now time.Time) error {
		request, policy = r, p
		if p.OfferMode == entities.OfferModeQuote {
			return ErrQuoteRequired
		}
		if r.Version != version {
			return ErrRequestConflict
		}
		if err := u.requireEligible(ctx, r, providerID); err != nil {
			return err
		}
		if p.ReserveProvider {
			active, err := u.Requests.ActiveOfProvider(ctx, providerID)
			if err != nil {
				return err
			}
			if active != nil {
				return ErrProviderBusy
			}
		}
		if err := offer.Respond(entities.OfferAccepted, now); err != nil {
			return err
		}
		change, err := r.Transition(entities.RequestAccepted, entities.RequestActor{Type: entities.RequestActorProvider, ID: &providerID}, "", now)
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRequestConflict
		}
		if err != nil {
			return err
		}
		r.ProviderID = &providerID
		setReminder(r, now)
		if err := u.recordTransition(ctx, r, change, p); err != nil {
			return err
		}
		if err := u.Offers.Update(ctx, offer); err != nil {
			return err
		}
		businessmetrics.OfferOutcomes.WithLabelValues(r.CategorySlug, string(entities.OfferAccepted)).Inc()
		released, err = u.closeOffers(ctx, r, &offer.ID, "another_provider_accepted", now)
		released = append(released, reservation{providerID: providerID, offerID: offer.ID})
		return err
	})
	if err != nil {
		return nil, err
	}
	u.afterAssignment(ctx, request, policy, released)
	view, err := u.providerView(ctx, request)
	if err != nil {
		return nil, err
	}
	return &dtos.AcceptResult{Request: *view, Route: u.pickupRoute(ctx, providerID, request)}, nil
}

func (u *Usecase) requireEligible(ctx context.Context, request *entities.ServiceRequest, providerID uuid.UUID) error {
	eligible, err := u.Providers.FilterEligible(ctx, request.ServiceID, []uuid.UUID{providerID})
	if err != nil {
		return err
	}
	if len(eligible) == 0 {
		return ErrProviderNotApproved
	}
	capable, err := u.filterByVehicleType(ctx, request, eligible)
	if err != nil {
		return err
	}
	if len(capable) == 0 {
		return ErrProviderNotEligible
	}
	return nil
}

func (u *Usecase) afterAssignment(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, released []reservation) {
	u.releaseAll(ctx, released)
	if err := u.Locator.SetTrip(ctx, *request.ProviderID, tripFor(request, policy)); err != nil {
		logger.FromContext(ctx).Warn("active trip not stored", zap.Error(err))
	}
	businessmetrics.RequestOutcomes.WithLabelValues(request.CategorySlug, string(request.Status)).Inc()
}

func setReminder(request *entities.ServiceRequest, now time.Time) {
	if request.ScheduledFor == nil {
		return
	}
	at := request.ScheduledFor.Add(-reminderLead)
	if at.After(now) {
		request.ReminderAt = &at
	}
}

func tripFor(request *entities.ServiceRequest, policy *entities.DispatchPolicy) entities.ActiveTrip {
	target := &request.Pickup.Point
	if request.Status == entities.RequestInProgress {
		target = nil
		if request.Destination != nil {
			target = &request.Destination.Point
		}
	}
	return entities.ActiveTrip{
		RequestID: request.ID, CustomerID: request.CustomerID, Status: request.Status, Target: target,
		Tracked: policy.Tracks(trackedStatus(request.Status)),
	}
}

func (u *Usecase) pickupRoute(ctx context.Context, providerID uuid.UUID, request *entities.ServiceRequest) *dtos.RouteView {
	position, err := u.Locator.LastPosition(ctx, providerID)
	if err != nil || position == nil {
		return nil
	}
	route, err := u.Routing.Route(ctx, position.Point, request.Pickup.Point)
	if err != nil {
		return nil
	}
	return &dtos.RouteView{DistanceM: route.DistanceMeters, DurationS: int(route.Duration.Seconds()), Geometry: route.Geometry, Source: string(route.Source)}
}

func (u *Usecase) Reject(ctx context.Context, providerID, offerID uuid.UUID, in dtos.RejectOfferInput) error {
	var release *reservation
	again := false
	var requestID uuid.UUID
	err := u.withLockedOffer(ctx, providerID, offerID, func(ctx context.Context, request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, now time.Time) error {
		requestID = request.ID
		note, err := u.reasonNote(ctx, in.ReasonID, entities.RejectionScopeOfferReject, request.CategoryID, in.Note)
		if err != nil {
			return err
		}
		if err := offer.Respond(entities.OfferRejected, now); err != nil {
			return err
		}
		offer.RejectReason, offer.RejectNote = &in.ReasonID, note
		if err := u.Offers.Update(ctx, offer); err != nil {
			return err
		}
		release = &reservation{providerID: providerID, offerID: offer.ID}
		businessmetrics.OfferOutcomes.WithLabelValues(request.CategorySlug, string(entities.OfferRejected)).Inc()
		if err := u.publish(ctx, u.offerEvent(events.OfferRejected, request, offer, now)); err != nil {
			return err
		}
		if request.Status != entities.RequestOffered {
			return nil
		}
		again, err = u.reconcile(ctx, request, policy, now)
		return err
	})
	if err != nil {
		return err
	}
	u.releaseAll(ctx, []reservation{*release})
	if again {
		if err := u.Advance(ctx, requestID); err != nil {
			logger.FromContext(ctx).Warn("search after rejection failed, the watcher will retry", zap.Error(err))
		}
	}
	return nil
}

func (u *Usecase) SubmitQuote(ctx context.Context, providerID, offerID uuid.UUID, in dtos.QuoteInput) error {
	return u.withLockedOffer(ctx, providerID, offerID, func(ctx context.Context, request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, now time.Time) error {
		if policy.OfferMode != entities.OfferModeQuote {
			return ErrQuoteNotAllowed
		}
		if err := u.requireEligible(ctx, request, providerID); err != nil {
			return err
		}
		if request.Status != entities.RequestOffered && request.Status != entities.RequestAwaitingChoice {
			return ErrRequestConflict
		}
		validUntil := now.Add(time.Duration(policy.SearchTTLSeconds) * time.Second)
		if err := offer.SubmitQuote(in.AmountCents, in.Note, validUntil, now); err != nil {
			return err
		}
		if err := u.Offers.Update(ctx, offer); err != nil {
			return err
		}
		if request.Status == entities.RequestOffered {
			change, err := request.Transition(entities.RequestAwaitingChoice, entities.SystemRequestActor(), "", now)
			if err != nil {
				return err
			}
			request.NextActionAt = &validUntil
			if err := u.recordTransition(ctx, request, change, policy); err != nil {
				return err
			}
		}
		businessmetrics.OfferOutcomes.WithLabelValues(request.CategorySlug, string(entities.OfferQuoted)).Inc()
		return u.publish(ctx, events.QuoteSubmittedV1{
			OfferID: offer.ID, RequestID: request.ID, ProviderID: providerID, CustomerID: request.CustomerID, Category: request.CategorySlug,
			Version: request.Version, AmountCents: in.AmountCents, Currency: request.Fare.Currency, OccurredAt: now,
		})
	})
}

func (u *Usecase) SelectQuote(ctx context.Context, customerID, requestID uuid.UUID, in dtos.SelectQuoteInput) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	var policy *entities.DispatchPolicy
	var released []reservation
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestAwaitingChoice || request.Version != in.Version {
			return ErrRequestConflict
		}
		offer, err := u.Offers.FindForUpdate(ctx, in.OfferID)
		if err != nil {
			return err
		}
		if offer == nil || offer.RequestID != requestID {
			return ErrOfferNotFound
		}
		now := u.now()
		if offer.Status != entities.OfferQuoted || offer.QuoteCents == nil || !now.Before(offer.ExpiresAt) {
			return ErrQuoteExpired
		}
		policy, err = u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		change, err := request.Transition(entities.RequestAccepted, entities.RequestActor{Type: entities.RequestActorCustomer, ID: &customerID}, "", now)
		if err != nil {
			return err
		}
		request.ProviderID = &offer.ProviderID
		total := *offer.QuoteCents
		commission, err := u.quoteCommission(ctx, request, total)
		if err != nil {
			return err
		}
		request.Fare.Cents, request.Fare.CommissionCents = &total, &commission
		setReminder(request, now)
		if err := u.recordTransition(ctx, request, change, policy); err != nil {
			return err
		}
		offer.Close(entities.OfferAccepted, now)
		if err := u.Offers.Update(ctx, offer); err != nil {
			return err
		}
		businessmetrics.OfferOutcomes.WithLabelValues(request.CategorySlug, string(entities.OfferAccepted)).Inc()
		released, err = u.closeOffers(ctx, request, &offer.ID, "another_quote_selected", now)
		return err
	})
	if err != nil {
		return nil, err
	}
	u.afterAssignment(ctx, request, policy, released)
	return u.customerView(ctx, request)
}

func (u *Usecase) quoteCommission(ctx context.Context, request *entities.ServiceRequest, total int64) (int64, error) {
	tariff, err := u.Pricing.CurrentTariff(ctx, request.ServiceID, request.ZoneID)
	if err != nil || tariff == nil {
		return 0, err
	}
	return tariff.CommissionFor(total), nil
}

func (u *Usecase) withLockedOffer(ctx context.Context, providerID, offerID uuid.UUID, fn func(ctx context.Context, request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, now time.Time) error) error {
	preview, err := u.Offers.Find(ctx, offerID)
	if err != nil {
		return err
	}
	if preview == nil || preview.ProviderID != providerID {
		return ErrOfferNotFound
	}
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, preview.RequestID)
		if err != nil {
			return err
		}
		if request == nil {
			return ErrOfferNotFound
		}
		offer, err := u.Offers.FindForUpdate(ctx, offerID)
		if err != nil {
			return err
		}
		if offer == nil {
			return ErrOfferNotFound
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		return fn(ctx, request, offer, policy, u.now())
	})
}

func (u *Usecase) StartRoute(ctx context.Context, providerID, requestID uuid.UUID) (*dtos.RequestView, error) {
	return u.providerStep(ctx, providerID, requestID, entities.RequestProviderEnRoute, nil)
}

func (u *Usecase) Arrive(ctx context.Context, providerID, requestID uuid.UUID) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsProvider(providerID) {
			return ErrRequestNotFound
		}
		now := u.now()
		if err := request.MarkArrived(now); err != nil {
			return err
		}
		return u.Requests.Update(ctx, request, request.Touch(now))
	})
	if err != nil {
		return nil, err
	}
	return u.providerView(ctx, request)
}

func (u *Usecase) StartJob(ctx context.Context, providerID, requestID uuid.UUID) (*dtos.RequestView, error) {
	return u.providerStep(ctx, providerID, requestID, entities.RequestInProgress, u.chargeWaiting)
}

func (u *Usecase) CompleteJob(ctx context.Context, providerID, requestID uuid.UUID, in dtos.CompleteInput) (*dtos.RequestView, error) {
	if err := u.checkCompletionCode(ctx, providerID, requestID, in.Code); err != nil {
		return nil, err
	}
	view, err := u.providerStep(ctx, providerID, requestID, entities.RequestCompleted, nil)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (u *Usecase) chargeWaiting(ctx context.Context, request *entities.ServiceRequest, actorID uuid.UUID, now time.Time) error {
	if request.ArrivedAt == nil || request.Fare.EstimateID == nil || request.Fare.Cents == nil {
		return nil
	}
	estimate, err := u.Pricing.FindEstimate(ctx, *request.Fare.EstimateID)
	if err != nil || estimate == nil {
		return err
	}
	tariff, err := u.Pricing.FindTariff(ctx, estimate.TariffID)
	if err != nil || tariff == nil {
		return err
	}
	fee := tariff.WaitingFeeFor(now.Sub(*request.ArrivedAt))
	if fee == 0 {
		return nil
	}
	request.AddToFare(fee)
	return u.Requests.AddFareAdjustment(ctx, entities.FareAdjustment{RequestID: request.ID, Reason: entities.AdjustmentWaiting, AmountCents: fee, ActorID: &actorID, CreatedAt: now})
}

type stepHook func(ctx context.Context, request *entities.ServiceRequest, actorID uuid.UUID, now time.Time) error

func (u *Usecase) providerStep(ctx context.Context, providerID, requestID uuid.UUID, to entities.RequestStatus, hook stepHook) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	var policy *entities.DispatchPolicy
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		request, err = u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsProvider(providerID) {
			return ErrRequestNotFound
		}
		policy, err = u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		now := u.now()
		change, err := request.Transition(to, entities.RequestActor{Type: entities.RequestActorProvider, ID: &providerID}, "", now)
		if err != nil {
			return err
		}
		if hook != nil {
			if err := hook(ctx, request, providerID, now); err != nil {
				return err
			}
		}
		if err := u.recordTransition(ctx, request, change, policy); err != nil {
			return err
		}
		if to == entities.RequestCompleted {
			if u.Refunds != nil {
				if err := u.Refunds.OnPrepaidCompleted(ctx, request, now); err != nil {
					return err
				}
			}
			return u.recordCompletion(ctx, request, providerID, now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	u.syncTrip(ctx, request, policy)
	businessmetrics.RequestOutcomes.WithLabelValues(request.CategorySlug, string(request.Status)).Inc()
	return u.providerView(ctx, request)
}

func (u *Usecase) recordCompletion(ctx context.Context, request *entities.ServiceRequest, providerID uuid.UUID, now time.Time) error {
	if err := u.Providers.TouchJobCompleted(ctx, providerID, now); err != nil {
		return err
	}
	if request.StartedAt == nil || request.DestinationH3 == nil {
		return nil
	}
	zone, err := u.Config.FindZone(ctx, request.ZoneID)
	if err != nil {
		return err
	}
	location := time.UTC
	if zone != nil {
		location = zone.Location()
	}
	return u.Pricing.RecordETA(ctx, request.PickupH3R8, *request.DestinationH3, now.In(location).Hour(), now.Sub(*request.StartedAt))
}

func (u *Usecase) syncTrip(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy) {
	if request.ProviderID == nil {
		return
	}
	if request.Status.IsTerminal() {
		u.clearTrip(ctx, request)
		return
	}
	if err := u.Locator.SetTrip(ctx, *request.ProviderID, tripFor(request, policy)); err != nil {
		logger.FromContext(ctx).Warn("active trip not stored", zap.Error(err))
	}
}
