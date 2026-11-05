package routing

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
)

type Settings struct {
	Timeout          time.Duration
	FailureThreshold int
	Cooldown         time.Duration
	RoadFactor       float64
	AverageSpeedKmh  float64
}

type Resilient struct {
	primary  services.IRoutingProvider
	settings Settings
	now      func() time.Time

	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

var _ services.IRoutingProvider = (*Resilient)(nil)

func NewResilient(primary services.IRoutingProvider, settings Settings) *Resilient {
	return &Resilient{primary: primary, settings: settings, now: time.Now}
}

func (r *Resilient) Route(ctx context.Context, from, to entities.GeoPoint, via ...entities.GeoPoint) (services.Route, error) {
	if r.primary != nil && r.closed() {
		callCtx, cancel := context.WithTimeout(ctx, r.settings.Timeout)
		defer cancel()
		route, err := r.primary.Route(callCtx, from, to, via...)
		r.record(ctx, err)
		if err == nil {
			businessmetrics.RoutingCalls.WithLabelValues("directions", "ok").Inc()
			return route, nil
		}
		businessmetrics.RoutingCalls.WithLabelValues("directions", "error").Inc()
	}
	return r.estimate(from, to, via), nil
}

func (r *Resilient) ETAs(ctx context.Context, origins []entities.GeoPoint, to entities.GeoPoint) ([]time.Duration, error) {
	if r.primary != nil && r.closed() {
		callCtx, cancel := context.WithTimeout(ctx, r.settings.Timeout)
		defer cancel()
		etas, err := r.primary.ETAs(callCtx, origins, to)
		r.record(ctx, err)
		if err == nil {
			businessmetrics.RoutingCalls.WithLabelValues("matrix", "ok").Inc()
			return etas, nil
		}
		businessmetrics.RoutingCalls.WithLabelValues("matrix", "error").Inc()
	}
	etas := make([]time.Duration, 0, len(origins))
	for _, origin := range origins {
		etas = append(etas, r.estimate(origin, to, nil).Duration)
	}
	return etas, nil
}

func (r *Resilient) estimate(from, to entities.GeoPoint, via []entities.GeoPoint) services.Route {
	straightLine := 0.0
	previous := from
	for _, point := range append(append([]entities.GeoPoint{}, via...), to) {
		straightLine += entities.DistanceMeters(previous, point)
		previous = point
	}
	roadMeters := straightLine * r.settings.RoadFactor
	metersPerSecond := r.settings.AverageSpeedKmh * 1000 / 3600
	return services.Route{
		DistanceMeters: int(roadMeters),
		Duration:       time.Duration(roadMeters / metersPerSecond * float64(time.Second)),
		Source:         services.RouteSourceFallback,
	}
}

func (r *Resilient) closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.now().Before(r.openUntil)
}

func (r *Resilient) record(ctx context.Context, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.failures = 0
		return
	}
	r.failures++
	if r.failures >= r.settings.FailureThreshold {
		r.openUntil = r.now().Add(r.settings.Cooldown)
		r.failures = 0
		logger.FromContext(ctx).Warn("routing circuit opened, using road factor estimates", zap.Error(err))
	}
}
