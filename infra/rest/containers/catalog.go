package containers

import (
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/catalog/controllers"
	catalogrouter "github.com/Agora-Mobility/atlas/infra/rest/catalog/router"
	"github.com/Agora-Mobility/atlas/usecases/catalog"
)

type CatalogModule struct {
	Router *catalogrouter.Router
}

func newCatalogModule(c *Container) *CatalogModule {
	uc := catalog.NewUsecase(
		repository.NewCatalogRepository(c.Pool),
		repository.NewAuditLogRepository(c.Pool),
		c.Outbox,
		c.TxManager,
		c.Cache,
	)
	return &CatalogModule{
		Router: catalogrouter.NewRouter(controllers.NewCatalogController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer),
	}
}
