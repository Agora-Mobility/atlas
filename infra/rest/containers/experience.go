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
		Requests:  repository.NewRequestRepository(c.Pool),
		Providers: repository.NewProviderRepository(c.Pool),
		Audit:     repository.NewAuditLogRepository(c.Pool),
		Tx:        c.TxManager,
	})
	return &ExperienceModule{
		Usecase: uc,
		Router:  experiencerest.NewRouter(experiencerest.NewController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, middleware.NewIdempotency(c.Redis, c.Config.Dispatch.IdempotencyTTL)),
	}
}
