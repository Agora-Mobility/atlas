package dispatch

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
)

type candidate struct {
	provider  entities.EligibleProvider
	distanceM float64
}

func (u *Usecase) Advance(ctx context.Context, requestID uuid.UUID) error {
	var reserved []reservation
	var alert *events.OperatorAlertedV1
	again := false
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		reserved = nil
		request, err := u.Requests.TryLock(ctx, requestID)
		if err != nil || request == nil {
			return err
		}
		now := u.now()
		if request.NextActionAt == nil || request.NextActionAt.After(now) {
			return nil
		}
		policy, err := u.policyOf(ctx, request)
		if err != nil {
			return err
		}
		switch request.Status {
		case entities.RequestScheduled:
			change, err := request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "", now)
			if err != nil {
				return err
			}
			request.BeginSearch(policy, now)
			reserved, alert, err = u.searchRound(ctx, request, policy, change, now)
			return err
		case entities.RequestSearching:
			reserved, alert, err = u.searchRound(ctx, request, policy, nil, now)
			return err
		case entities.RequestOffered:
			again, err = u.reconcile(ctx, request, policy, now)
			return err
		case entities.RequestAwaitingChoice:
			return u.closeUnchosenQuotes(ctx, request, policy, now)
		default:
			request.NextActionAt = nil
			return u.Requests.Update(ctx, request, request.Touch(now))
		}
	})
	if err != nil {
		u.releaseAll(ctx, reserved)
		return err
	}
	if alert != nil {
		logger.FromContext(ctx).Warn("request needs an operator", zap.String("request_id", requestID.String()), zap.String("reason", alert.Reason))
	}
	if again {
		return u.Advance(ctx, requestID)
	}
	return nil
}

type reservation struct {
	providerID uuid.UUID
	offerID    uuid.UUID
}

func (u *Usecase) releaseAll(ctx context.Context, reserved []reservation) {
	for _, r := range reserved {
		if err := u.Locator.Release(ctx, r.providerID, r.offerID); err != nil {
			logger.FromContext(ctx).Warn("provider reservation not released", zap.Error(err))
		}
	}
}

func (u *Usecase) searchRound(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, pending *entities.RequestStatusChange, now time.Time) ([]reservation, *events.OperatorAlertedV1, error) {
	expected := request.Version
	if pending != nil {
		expected = pending.ExpectedVersion
		if err := u.announceTransition(ctx, request, pending, policy); err != nil {
			return nil, nil, err
		}
	}
	if request.SearchDeadline != nil && !now.Before(*request.SearchDeadline) {
		alert, err := u.exhausted(ctx, request, policy, expected, now)
		return nil, alert, err
	}

	for {
		found, err := u.findCandidates(ctx, request, policy, policy.RadiusAt(request.SearchRound))
		if err != nil {
			return nil, nil, err
		}
		reserved, created, err := u.sendOffers(ctx, request, policy, found, now)
		if err != nil {
			return reserved, nil, err
		}
		if created > 0 {
			change, err := request.Transition(entities.RequestOffered, entities.SystemRequestActor(), "", now)
			if err != nil {
				return reserved, nil, err
			}
			change.ExpectedVersion = expected
			safety := now.Add(time.Duration(policy.OfferTTLSeconds)*time.Second + offerSafetyMargin)
			request.NextActionAt = &safety
			return reserved, nil, u.recordTransition(ctx, request, change, policy)
		}
		if policy.IsLastRound(request.SearchRound) {
			break
		}
		request.SearchRound++
	}

	next := now.Add(searchRetryInterval(policy))
	if request.SearchDeadline != nil && next.After(*request.SearchDeadline) {
		next = *request.SearchDeadline
	}
	request.NextActionAt = &next
	request.UpdatedAt = now
	return nil, nil, u.Requests.Update(ctx, request, expected)
}

func searchRetryInterval(policy *entities.DispatchPolicy) time.Duration {
	interval := time.Duration(policy.SearchTTLSeconds) * time.Second / searchRetryDivisor
	if interval < searchRetryFloor {
		return searchRetryFloor
	}
	return interval
}

func (u *Usecase) exhausted(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, expected int, now time.Time) (*events.OperatorAlertedV1, error) {
	switch policy.OnExhausted {
	case entities.ExhaustedExtendOrQueue:
		if !request.SearchExtended {
			request.SearchExtended = true
			request.BeginSearch(policy, now)
			request.UpdatedAt = now
			return nil, u.Requests.Update(ctx, request, expected)
		}
		return u.queueForOperator(ctx, request, expected, "no_provider_after_extension", now)
	case entities.ExhaustedAlertOperator, entities.ExhaustedOperatorQueue:
		return u.queueForOperator(ctx, request, expected, "no_provider", now)
	default:
		change, err := request.Transition(entities.RequestExpired, entities.SystemRequestActor(), "no_provider", now)
		if err != nil {
			return nil, err
		}
		change.ExpectedVersion = expected
		return nil, u.recordTransition(ctx, request, change, policy)
	}
}

func (u *Usecase) queueForOperator(ctx context.Context, request *entities.ServiceRequest, expected int, reason string, now time.Time) (*events.OperatorAlertedV1, error) {
	request.SendToOperator(reason, now)
	if err := u.Requests.Update(ctx, request, expected); err != nil {
		return nil, err
	}
	alert := events.OperatorAlertedV1{RequestID: request.ID, CustomerID: request.CustomerID, Category: request.CategorySlug, Reason: reason, OccurredAt: now}
	return &alert, u.publish(ctx, alert)
}

func (u *Usecase) findCandidates(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, radiusKm float64) ([]candidate, error) {
	started := time.Now()
	defer func() {
		businessmetrics.CandidateSearchSeconds.WithLabelValues(string(policy.CandidateSource)).Observe(time.Since(started).Seconds())
	}()

	previous, err := u.Offers.ListOfRequest(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	excluded := make(map[uuid.UUID]struct{}, len(previous))
	for _, offer := range previous {
		if offer.BoostBps >= request.Fare.BoostBps {
			excluded[offer.ProviderID] = struct{}{}
		}
	}

	var found []candidate
	if policy.CandidateSource == entities.CandidateLive {
		found, err = u.liveCandidates(ctx, request, policy, radiusKm, excluded)
	} else {
		found, err = u.coverageCandidates(ctx, request, excluded)
	}
	if err != nil {
		return nil, err
	}
	u.rank(found, policy, u.now())
	if policy.RankByETA {
		found = u.rankByETA(ctx, request, found)
	}
	return found, nil
}

func (u *Usecase) liveCandidates(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, radiusKm float64, excluded map[uuid.UUID]struct{}) ([]candidate, error) {
	cell, err := u.Geo.Cell(request.Pickup.Point, policy.H3Resolution)
	if err != nil {
		return nil, err
	}
	cells, err := u.Geo.Disk(cell, u.Geo.RingsFor(radiusKm, policy.H3Resolution))
	if err != nil {
		return nil, err
	}
	businessmetrics.CandidateSearchCells.WithLabelValues(string(policy.CandidateSource)).Observe(float64(len(cells)))
	live, err := u.Locator.FindInCells(ctx, request.CategorySlug, policy.H3Resolution, cells)
	if err != nil {
		return nil, err
	}

	nearest, distances := nearestWithin(live, request.Pickup.Point, radiusKm*metersPerKm, excluded)
	reserved, err := u.Locator.ReservedAmong(ctx, nearest)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(nearest))
	for _, id := range nearest {
		if _, skip := reserved[id]; !skip {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	eligible, err := u.Providers.FilterEligible(ctx, request.ServiceID, ids)
	if err != nil {
		return nil, err
	}
	busy, err := u.busySet(ctx, policy, eligible)
	if err != nil {
		return nil, err
	}
	found := make([]candidate, 0, len(eligible))
	for _, provider := range eligible {
		if _, isBusy := busy[provider.UserID]; !isBusy {
			found = append(found, candidate{provider: provider, distanceM: distances[provider.UserID]})
		}
	}
	return found, nil
}

func nearestWithin(live []repository.LiveCandidate, pickup entities.GeoPoint, radiusMeters float64, excluded map[uuid.UUID]struct{}) ([]uuid.UUID, map[uuid.UUID]float64) {
	distances := make(map[uuid.UUID]float64, len(live))
	ids := make([]uuid.UUID, 0, len(live))
	for _, c := range live {
		if _, skip := excluded[c.ProviderID]; skip {
			continue
		}
		distance := entities.DistanceMeters(c.Point, pickup)
		if distance <= radiusMeters {
			distances[c.ProviderID] = distance
			ids = append(ids, c.ProviderID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return distances[ids[i]] < distances[ids[j]] })
	if len(ids) > maxCandidatesPerSearch {
		ids = ids[:maxCandidatesPerSearch]
	}
	return ids, distances
}

func (u *Usecase) coverageCandidates(ctx context.Context, request *entities.ServiceRequest, excluded map[uuid.UUID]struct{}) ([]candidate, error) {
	businessmetrics.CandidateSearchCells.WithLabelValues(string(entities.CandidateCoverage)).Observe(1)
	cell, err := u.Geo.Cell(request.Pickup.Point, zoneCellResolution)
	if err != nil {
		return nil, err
	}
	covering, err := u.Providers.CoveringCell(ctx, request.CategoryID, cell)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(covering))
	for _, id := range covering {
		if _, skip := excluded[id]; !skip {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	eligible, err := u.Providers.FilterEligible(ctx, request.ServiceID, ids)
	if err != nil {
		return nil, err
	}
	found := make([]candidate, 0, len(eligible))
	for _, provider := range eligible {
		if provider.Base == nil || provider.ServiceRadiusKm == nil {
			continue
		}
		distance := entities.DistanceMeters(*provider.Base, request.Pickup.Point)
		if distance <= *provider.ServiceRadiusKm*metersPerKm {
			found = append(found, candidate{provider: provider, distanceM: distance})
		}
	}
	return found, nil
}

func (u *Usecase) busySet(ctx context.Context, policy *entities.DispatchPolicy, eligible []entities.EligibleProvider) (map[uuid.UUID]struct{}, error) {
	busy := map[uuid.UUID]struct{}{}
	if !policy.ReserveProvider || len(eligible) == 0 {
		return busy, nil
	}
	ids := make([]uuid.UUID, 0, len(eligible))
	for _, provider := range eligible {
		ids = append(ids, provider.UserID)
	}
	withJob, err := u.Providers.WithActiveJob(ctx, ids, u.now().Add(time.Duration(policy.SearchLeadSeconds)*time.Second))
	if err != nil {
		return nil, err
	}
	for _, id := range withJob {
		busy[id] = struct{}{}
	}
	return busy, nil
}

func (u *Usecase) rank(found []candidate, policy *entities.DispatchPolicy, now time.Time) {
	sort.SliceStable(found, func(i, j int) bool {
		if policy.Ranking == entities.RankingDistanceIdle {
			bandI, bandJ := int(found[i].distanceM/idleBandMeters), int(found[j].distanceM/idleBandMeters)
			if bandI != bandJ {
				return bandI < bandJ
			}
			return idleTime(found[i].provider, now) > idleTime(found[j].provider, now)
		}
		return found[i].distanceM < found[j].distanceM
	})
}

func idleTime(provider entities.EligibleProvider, now time.Time) time.Duration {
	if provider.LastJobCompletedAt != nil {
		return now.Sub(*provider.LastJobCompletedAt)
	}
	return now.Sub(provider.CreatedAt)
}

func (u *Usecase) rankByETA(ctx context.Context, request *entities.ServiceRequest, found []candidate) []candidate {
	if len(found) < 2 {
		return found
	}
	top := found
	if len(top) > maxCandidateBatchForETA {
		top = top[:maxCandidateBatchForETA]
	}
	positions := make([]entities.GeoPoint, 0, len(top))
	kept := make([]candidate, 0, len(top))
	for _, c := range top {
		position, err := u.Locator.LastPosition(ctx, c.provider.UserID)
		if err != nil || position == nil {
			continue
		}
		positions = append(positions, position.Point)
		kept = append(kept, c)
	}
	etas, err := u.Routing.ETAs(ctx, positions, request.Pickup.Point)
	if err != nil || len(etas) != len(kept) {
		return found
	}
	index := make([]int, len(kept))
	for i := range index {
		index[i] = i
	}
	sort.SliceStable(index, func(a, b int) bool { return etas[index[a]] < etas[index[b]] })
	ordered := make([]candidate, 0, len(found))
	for _, i := range index {
		ordered = append(ordered, kept[i])
	}
	return append(ordered, found[len(top):]...)
}

func (u *Usecase) sendOffers(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, found []candidate, now time.Time) ([]reservation, int, error) {
	reserved := make([]reservation, 0, policy.BatchSize)
	created := 0
	for _, c := range found {
		if created == policy.BatchSize {
			break
		}
		offerID, err := newUUID()
		if err != nil {
			return reserved, created, err
		}
		if policy.ReserveProvider {
			ok, err := u.Locator.Reserve(ctx, c.provider.UserID, offerID, time.Duration(policy.OfferTTLSeconds)*time.Second)
			if err != nil {
				return reserved, created, err
			}
			if !ok {
				continue
			}
			reserved = append(reserved, reservation{providerID: c.provider.UserID, offerID: offerID})
		}
		offer := entities.NewRequestOffer(offerID, request.ID, c.provider.UserID, policy, int(c.distanceM), request.Fare.ProviderNetCents(), request.Fare.BoostBps, now)
		if err := u.Offers.Create(ctx, offer); err != nil {
			if errors.Is(err, repository.ErrOfferAlreadySent) {
				continue
			}
			return reserved, created, err
		}
		if err := u.publish(ctx, u.newOfferEvent(request, offer, policy, now)); err != nil {
			return reserved, created, err
		}
		created++
	}
	return reserved, created, nil
}

func (u *Usecase) newOfferEvent(request *entities.ServiceRequest, offer *entities.RequestOffer, policy *entities.DispatchPolicy, now time.Time) events.RequestOfferV1 {
	event := u.offerEvent(events.OfferCreated, request, offer, now)
	event.Version = request.Version + 1
	event.Pickup = &events.RequestPoint{Lat: request.Pickup.Point.Lat, Lng: request.Pickup.Point.Lng, Reference: request.Pickup.Reference}
	if request.Destination != nil {
		event.Destination = &events.RequestPoint{Lat: request.Destination.Point.Lat, Lng: request.Destination.Point.Lng, Reference: request.Destination.Reference}
	}
	event.DistanceM = offer.DistanceM
	event.NetCents = offer.NetCents
	event.Currency = request.Fare.Currency
	event.Channels = policy.InitialChannels
	return event
}

func (u *Usecase) reconcile(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, now time.Time) (bool, error) {
	offers, err := u.Offers.ListOfRequest(ctx, request.ID)
	if err != nil {
		return false, err
	}
	for _, offer := range offers {
		if offer.IsOpen() || offer.Status == entities.OfferQuoted {
			safety := offer.ExpiresAt.Add(offerSafetyMargin)
			if safety.Before(now) {
				safety = now.Add(offerSafetyMargin)
			}
			request.NextActionAt = &safety
			return false, u.Requests.Update(ctx, request, request.Version)
		}
	}
	change, err := request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "offers_ended", now)
	if err != nil {
		return false, err
	}
	request.NextActionAt = &now
	return true, u.recordTransition(ctx, request, change, policy)
}

func (u *Usecase) closeUnchosenQuotes(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, now time.Time) error {
	change, err := request.Transition(entities.RequestCancelled, entities.SystemRequestActor(), "quote_choice_timeout", now)
	if err != nil {
		return err
	}
	if err := u.recordTransition(ctx, request, change, policy); err != nil {
		return err
	}
	_, err = u.closeOffers(ctx, request, nil, "quote_choice_timeout", now)
	return err
}

func (u *Usecase) closeOffers(ctx context.Context, request *entities.ServiceRequest, keep *uuid.UUID, reason string, now time.Time) ([]reservation, error) {
	offers, err := u.Offers.ListOfRequestForUpdate(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	released := make([]reservation, 0, len(offers))
	for _, offer := range offers {
		if keep != nil && offer.ID == *keep {
			continue
		}
		if !offer.IsOpen() && offer.Status != entities.OfferQuoted {
			continue
		}
		offer.Close(entities.OfferCancelled, now)
		if err := u.Offers.Update(ctx, offer); err != nil {
			return released, err
		}
		event := u.offerEvent(events.OfferCancelled, request, offer, now)
		event.Reason = reason
		if err := u.publish(ctx, event); err != nil {
			return released, err
		}
		businessmetrics.OfferOutcomes.WithLabelValues(request.CategorySlug, string(entities.OfferCancelled)).Inc()
		released = append(released, reservation{providerID: offer.ProviderID, offerID: offer.ID})
	}
	return released, nil
}
