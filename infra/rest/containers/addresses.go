package containers

import (
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/addresses/controllers"
	addressesrouter "github.com/Agora-Mobility/atlas/infra/rest/addresses/router"
	"github.com/Agora-Mobility/atlas/usecases/addresses"
)

type AddressesModule struct {
	Router *addressesrouter.Router
}

func newAddressesModule(c *Container) *AddressesModule {
	uc := addresses.NewUsecase(repository.NewAddressRepository(c.Pool))
	return &AddressesModule{Router: addressesrouter.NewRouter(controllers.NewAddressController(uc), c.Authenticator, c.ClientAuthorizer)}
}
