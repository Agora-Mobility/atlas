package containers

import (
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/repository"
	experiencerest "github.com/Agora-Mobility/atlas/infra/rest/experience"
	"github.com/Agora-Mobility/atlas/usecases/experience"
)

type ExperienceModule struct {
	Router  *experiencerest.Router
	Usecase *experience.Usecase
}

func newExperienceModule(c *Container) *ExperienceModule {
	uc := experience.NewUsecase(experience.Deps{
		Reference: repository.NewReferenceRepository(c.Pool),
		Ratings:   repository.NewRatingRepository(c.Pool),
		Vouchers:  repository.NewVoucherRepository(c.Pool),
		Support:   repository.NewSupportRepository(c.Pool),
		Garage:    repository.NewGarageRepository(c.Pool),
		Account:   repository.NewAccountRepository(c.Pool),
		Ledger:    repository.NewLedgerRepository(c.Pool),
		Workshops: repository.NewWorkshopDirectory(c.Pool),
		Storage:   newObjectStorage(c.Config.R2),
		Requests:  repository.NewRequestRepository(c.Pool),
		Providers: repository.NewProviderRepository(c.Pool),
		Audit:     repository.NewAuditLogRepository(c.Pool),
		Tx:        c.TxManager,
	})
	uc.Inbox = uc
	c.Payments.Usecase.Rewards = uc
	return &ExperienceModule{
		Usecase: uc,
		Router:  experiencerest.NewRouter(experiencerest.NewController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, middleware.NewIdempotency(c.Redis, c.Config.Dispatch.IdempotencyTTL)),
	}
}
