package containers

import (
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/repository"
	bookingsrest "github.com/Agora-Mobility/atlas/infra/rest/bookings"
	"github.com/Agora-Mobility/atlas/usecases/bookings"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

type BookingsModule struct {
	Router  *bookingsrest.Router
	Usecase *bookings.Usecase
}

func newBookingsModule(c *Container, storage services.IObjectStorage) *BookingsModule {
	config := repository.NewDispatchConfigRepository(c.Pool)
	geo := h3index.New()
	uc := bookings.NewUsecase(bookings.Deps{
		Bookings: repository.NewBookingRepository(c.Pool),
		Requests: repository.NewRequestRepository(c.Pool),
		Fleet:    repository.NewFleetRepository(c.Pool),
		Garage:   repository.NewGarageRepository(c.Pool),
		Config:   config,
		Pricing:  repository.NewPricingRepository(c.Pool),
		Payments: repository.NewPaymentRepository(c.Pool),
		Cancels:  repository.NewCancellationRepository(c.Pool),
		Vouchers: repository.NewVoucherRepository(c.Pool),
		Settings: repository.NewReferenceRepository(c.Pool),
		Live:     repository.NewLiveRepository(c.Pool),
		Devices:  repository.NewDeviceRepository(c.Pool),
		Outbox:   c.Outbox,
		Audit:    repository.NewAuditLogRepository(c.Pool),
		Tx:       c.TxManager,
		Calls:    newCallBridge(c.Config.Calls),
		Geo:      geo,
		Zones:    dispatch.NewZoneCache(config, geo),
		Refunds:  c.Payments.Usecase,
		Storage:  storage,
		Photos:   c.Experience.Usecase,
	})
	c.Payments.Usecase.Prepaid = prepaidRoutes{bookings: uc, removals: c.Dispatch.Usecase}
	c.Dispatch.Usecase.Refunds = c.Payments.Usecase
	return &BookingsModule{
		Usecase: uc,
		Router:  bookingsrest.NewRouter(bookingsrest.NewController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, middleware.NewIdempotency(c.Redis, c.Config.Dispatch.IdempotencyTTL)),
	}
}
