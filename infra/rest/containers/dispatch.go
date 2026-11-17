package containers

import (
	"context"
	"fmt"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/entities"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/calls"
	"github.com/Agora-Mobility/atlas/infra/geo/googlemaps"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/geo/routing"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/notification/push"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/realtime"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/dispatch/controllers"
	dispatchrouter "github.com/Agora-Mobility/atlas/infra/rest/dispatch/router"
	"github.com/Agora-Mobility/atlas/infra/sse"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
	rtusecase "github.com/Agora-Mobility/atlas/usecases/realtime"
)

const kmhToMps = 1000.0 / 3600.0

type DispatchModule struct {
	Router    *dispatchrouter.Router
	Usecase   *dispatch.Usecase
	Projector *rtusecase.Projector
	Notifier  *notifications.RequestNotifier
	Locator   domainrepository.IProviderLocator

	projectorWorkers    int
	notificationWorkers int
}

func newCallBridge(cfg config.CallsConfig) services.ICallBridge {
	if !cfg.Enabled() {
		return calls.Unavailable{}
	}
	return calls.NewBridge(cfg.BaseURL, cfg.APIKey, cfg.Timeout)
}

func newDispatchModule(c *Container, pushSender services.IPushSender, routingProvider services.IRoutingProvider) *DispatchModule {
	cfg := c.Config
	geo := h3index.New()
	mailbox := realtime.NewMailbox(c.Redis, cfg.Realtime.MailboxEntries, cfg.Realtime.MailboxTTL)
	devices := repository.NewDeviceRepository(c.Pool)
	locator := repository.NewProviderLocator(c.Redis, geo)
	notifier := notifications.NewRequestNotifier(devices, repository.NewRequestRepository(c.Pool), pushSender, c.SMS, cfg.Dispatch.OfferDeepLink, cfg.Dispatch.OperatorPhone)
	uc := dispatch.NewUsecase(dispatch.Deps{
		Config:    repository.NewDispatchConfigRepository(c.Pool),
		Pricing:   repository.NewPricingRepository(c.Pool),
		Demand:    repository.NewDemandRepository(c.Pool),
		Signals:   repository.NewDemandSignals(c.Redis),
		Providers: repository.NewProviderRepository(c.Pool),
		Requests:  repository.NewRequestRepository(c.Pool),
		Offers:    repository.NewOfferRepository(c.Pool),
		Payments:  repository.NewPaymentRepository(c.Pool),
		Cancels:   repository.NewCancellationRepository(c.Pool),
		Ratings:   repository.NewRatingRepository(c.Pool),
		Vouchers:  repository.NewVoucherRepository(c.Pool),
		Live:      repository.NewLiveRepository(c.Pool),
		Reference: repository.NewReferenceRepository(c.Pool),
		Calls:     newCallBridge(cfg.Calls),
		Removals:  repository.NewRemovalRepository(c.Pool),
		Devices:   devices,
		Locator:   locator,
		Outbox:    c.Outbox,
		Tx:        c.TxManager,
		Audit:     repository.NewAuditLogRepository(c.Pool),
		Geo:       geo,
		Routing:   routingProvider,
		Feed:      mailbox,
		Channels:  notifier,
		Heatmap:   repository.NewHeatmapRepository(c.Pool),
		Cache:     c.Cache,
	}, dispatch.Settings{
		WatcherInterval: cfg.Dispatch.WatcherInterval, WatcherBatch: cfg.Dispatch.WatcherBatch, FareEstimateTTL: cfg.Dispatch.FareEstimateTTL,
		NearbyMinCount: cfg.Dispatch.NearbyMinCount, HistoryRetention: cfg.Dispatch.HistoryRetention, ChannelTimeout: cfg.Dispatch.ChannelTimeout, ShareBaseURL: cfg.Dispatch.ShareBaseURL, Currency: cfg.AppyPay.LedgerCurrency,
		Limits:     entities.PlausibilityLimits{MaxAccuracyM: cfg.Dispatch.MaxAccuracyM, MaxSpeedMps: cfg.Dispatch.MaxSpeedKmh * kmhToMps},
		RoadFactor: cfg.GoogleMaps.RoadFactor, AverageSpeedKmh: cfg.GoogleMaps.AverageSpeedKmh,
		DemandAreaResolution: cfg.Dispatch.DemandAreaResolution, DemandQuoteWindow: cfg.Dispatch.DemandQuoteWindow, DemandFactorTTL: cfg.Dispatch.DemandFactorTTL,
		DemandHistoryRetention: cfg.Dispatch.DemandHistoryRetention, RouteReviewAboveBps: cfg.Dispatch.RouteReviewAboveBps(),
		Heatmap: HeatmapSettings(cfg.Heatmap),
	})
	notifier.UseSharer(uc)
	notifier.UseAccount(repository.NewAccountRepository(c.Pool))
	streamer := sse.NewStreamer(c.SSE, mailbox, mailbox, sse.Settings{
		Heartbeat: cfg.Realtime.Heartbeat, PollInterval: cfg.Realtime.PollInterval, DeadAfter: cfg.Realtime.DeadAfter,
	})
	router := dispatchrouter.NewRouter(
		controllers.NewDispatchController(uc),
		controllers.NewStreamController(streamer, uc),
		c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer,
		middleware.NewIdempotency(c.Redis, cfg.Dispatch.IdempotencyTTL),
	)
	return &DispatchModule{
		Router: router, Usecase: uc, Projector: rtusecase.NewProjector(mailbox), Notifier: notifier, Locator: locator,
		projectorWorkers: cfg.Realtime.ProjectorWorkers, notificationWorkers: cfg.Dispatch.NotificationWorkers,
	}
}

func (m *DispatchModule) Consumers() []queue.ConsumerConfig {
	return []queue.ConsumerConfig{
		{Queue: "realtime.projector", BindingKeys: rtusecase.BindingKeys(), Handler: m.Projector.Handle, Workers: m.projectorWorkers},
		{Queue: "notifications.requests", BindingKeys: notifications.RequestBindingKeys(), Handler: m.Notifier.OnEvent, Workers: m.notificationWorkers},
	}
}

func newRoutingProvider(cfg config.GoogleMapsConfig) services.IRoutingProvider {
	settings := routing.Settings{
		Timeout: cfg.Timeout, FailureThreshold: cfg.BreakerFailures, Cooldown: cfg.BreakerCooldown,
		RoadFactor: cfg.RoadFactor, AverageSpeedKmh: cfg.AverageSpeedKmh,
	}
	if !cfg.Enabled() {
		return routing.NewResilient(nil, settings)
	}
	return routing.NewResilient(googlemaps.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Timeout), settings)
}

func newPushSender(ctx context.Context, cfg config.FCMConfig) (services.IPushSender, error) {
	if !cfg.Enabled() {
		return push.LogSender{}, nil
	}
	sender, err := push.NewFCMSender(ctx, []byte(cfg.CredentialsJSON), cfg.ProjectID, cfg.BaseURL, cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("fcm: %w", err)
	}
	return sender, nil
}

func HeatmapSettings(cfg config.HeatmapConfig) dispatch.HeatmapSettings {
	zooms := make([]dispatch.ZoomStep, 0, len(cfg.Zooms))
	for _, zoom := range cfg.Zooms {
		zooms = append(zooms, dispatch.ZoomStep{MinZoom: zoom.MinZoom, Resolution: zoom.Resolution})
	}
	return dispatch.HeatmapSettings{
		Interval: cfg.Interval, Window: cfg.Window, LookbackWindows: cfg.LookbackWindows, Rollups: cfg.Rollups,
		SelfWeight: cfg.SelfWeight, NeighborWeight: cfg.NeighborWeight, Zooms: zooms, MaxCells: cfg.MaxCells,
		MaxPeriod: cfg.MaxPeriod, CacheTTL: cfg.CacheTTL, Retention: cfg.Retention,
	}
}
