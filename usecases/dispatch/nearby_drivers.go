package dispatch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	defaultNearbyDrivers = 20
	// nearbyCoordinateScale rounds positions to 4 decimals, about 11 m
	nearbyCoordinateScale = 10_000
	nearbyIDBytes         = 12
)

// NearbyDrivers are the free drivers around the pickup, one by one, for the
// cars on the map of "Escolha a viatura" and the ETA of each option. A driver
// is listed with an opaque id that changes every day, a position rounded to
// about 11 m, the service and the kind of vehicle; never with their identity.
// Drivers with an offer pending or a job are left out. Categories matched by
// coverage (no live position) list no drivers.
func (u *Usecase) NearbyDrivers(ctx context.Context, point entities.GeoPoint, category string, serviceID *uuid.UUID) (*dtos.NearbyDriversView, error) {
	categoryID, policy, err := u.nearbyPolicy(ctx, point, category)
	if err != nil {
		return nil, err
	}
	view := &dtos.NearbyDriversView{Drivers: []dtos.NearbyDriver{}, Services: []dtos.NearbyService{}}
	if policy.CandidateSource != entities.CandidateLive {
		counts, err := u.nearbyCounts(ctx, point, category, categoryID, policy)
		if err != nil {
			return nil, err
		}
		view.Available = len(counts) > 0
		return view, nil
	}
	cells, err := u.Geo.Covering(point, policy.RadiiKm[0], policy.H3Resolution)
	if err != nil {
		return nil, err
	}
	live, err := u.Locator.FindInCells(ctx, category, policy.H3Resolution, cells)
	if err != nil {
		return nil, err
	}
	nearest := nearestLive(live, point, policy.RadiiKm[0]*metersPerKm, maxCandidatesPerSearch)
	if len(nearest) == 0 {
		return view, nil
	}
	free, err := u.freeDrivers(ctx, nearest)
	if err != nil {
		return nil, err
	}
	now := u.now()
	limit := u.settings.NearbyDriversMax
	if limit <= 0 {
		limit = defaultNearbyDrivers
	}
	for _, candidate := range nearest {
		driver, ok := free[candidate.ProviderID]
		if !ok || (serviceID != nil && driver.ServiceID != *serviceID) {
			continue
		}
		view.Available = true
		meters, seconds := entities.StraightETA(candidate.Point, point, u.settings.RoadFactor, u.settings.AverageSpeedKmh)
		summarize(view, driver, seconds)
		if len(view.Drivers) >= limit {
			continue
		}
		view.Drivers = append(view.Drivers, dtos.NearbyDriver{
			ID: u.publicDriverID(candidate.ProviderID, now), Lat: roundCoordinate(candidate.Point.Lat), Lng: roundCoordinate(candidate.Point.Lng),
			Heading: roundHeading(candidate.Heading), ServiceID: driver.ServiceID, ServiceSlug: driver.ServiceSlug, VehicleKind: driver.VehicleKind,
			DistanceM: meters, ETASecond: seconds, AgeS: max(0, int(now.Sub(candidate.RecordedAt).Seconds())),
		})
	}
	return view, nil
}

// nearestLive keeps the live positions inside the radius, nearest first.
func nearestLive(live []repository.LiveCandidate, point entities.GeoPoint, radiusMeters float64, limit int) []repository.LiveCandidate {
	type placed struct {
		candidate repository.LiveCandidate
		meters    float64
	}
	inside := make([]placed, 0, len(live))
	for _, candidate := range live {
		if meters := entities.DistanceMeters(candidate.Point, point); meters <= radiusMeters {
			inside = append(inside, placed{candidate, meters})
		}
	}
	sort.Slice(inside, func(i, j int) bool { return inside[i].meters < inside[j].meters })
	if len(inside) > limit {
		inside = inside[:limit]
	}
	out := make([]repository.LiveCandidate, 0, len(inside))
	for _, p := range inside {
		out = append(out, p.candidate)
	}
	return out
}

// freeDrivers are the approved drivers among the candidates with no offer
// pending and no job.
func (u *Usecase) freeDrivers(ctx context.Context, candidates []repository.LiveCandidate) (map[uuid.UUID]entities.AvailableDriver, error) {
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ProviderID)
	}
	reserved, err := u.Locator.ReservedAmong(ctx, ids)
	if err != nil {
		return nil, err
	}
	unreserved := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, taken := reserved[id]; !taken {
			unreserved = append(unreserved, id)
		}
	}
	if len(unreserved) == 0 {
		return nil, nil
	}
	drivers, err := u.Providers.AvailableDrivers(ctx, unreserved)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]entities.AvailableDriver, len(drivers))
	for _, driver := range drivers {
		out[driver.ProviderID] = driver
	}
	return out, nil
}

func summarize(view *dtos.NearbyDriversView, driver entities.AvailableDriver, seconds int) {
	for i := range view.Services {
		if view.Services[i].ServiceID == driver.ServiceID {
			view.Services[i].Drivers++
			view.Services[i].ETASecond = min(view.Services[i].ETASecond, seconds)
			return
		}
	}
	view.Services = append(view.Services, dtos.NearbyService{ServiceID: driver.ServiceID, ServiceSlug: driver.ServiceSlug, Drivers: 1, ETASecond: seconds})
}

// publicDriverID is an HMAC of the driver and the day: the same car keeps its
// id while the customer looks at the map, and nobody can follow a driver over days.
func (u *Usecase) publicDriverID(providerID uuid.UUID, now time.Time) string {
	mac := hmac.New(sha256.New, u.settings.NearbyIDKey)
	mac.Write(providerID[:])
	mac.Write([]byte(now.UTC().Format(time.DateOnly)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:nearbyIDBytes])
}

func roundCoordinate(value float64) float64 {
	return math.Round(value*nearbyCoordinateScale) / nearbyCoordinateScale
}

func roundHeading(heading *float64) *float64 {
	if heading == nil {
		return nil
	}
	rounded := math.Round(*heading)
	return &rounded
}
