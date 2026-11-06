package containers

import (
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/fleet/controllers"
	fleetrouter "github.com/Agora-Mobility/atlas/infra/rest/fleet/router"
	"github.com/Agora-Mobility/atlas/usecases/fleet"
)

type FleetModule struct {
	Router *fleetrouter.Router
}

func newFleetModule(c *Container) *FleetModule {
	uc := fleet.NewUsecase(fleet.Deps{
		Users:         repository.NewUserRepository(c.Pool),
		Providers:     repository.NewProviderRepository(c.Pool),
		Documents:     repository.NewProviderDocumentRepository(c.Pool),
		Requests:      repository.NewRequestRepository(c.Pool),
		Organizations: repository.NewOrganizationRepository(c.Pool),
		Fleet:         repository.NewFleetRepository(c.Pool),
		Locator:       c.Dispatch.Locator,
		Outbox:        c.Outbox,
		Audit:         repository.NewAuditLogRepository(c.Pool),
		Tx:            c.TxManager,
		Codes:         c.Auth.CodeSender,
		Profiles:      c.Providers.Usecase,
	})
	return &FleetModule{Router: fleetrouter.NewRouter(controllers.NewFleetController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer)}
}
