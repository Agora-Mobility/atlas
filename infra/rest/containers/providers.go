package containers

import (
	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/providers/controllers"
	providersrouter "github.com/Agora-Mobility/atlas/infra/rest/providers/router"
	"github.com/Agora-Mobility/atlas/infra/storage/r2"
	"github.com/Agora-Mobility/atlas/usecases/providers"
)

type ProvidersModule struct {
	Router  *providersrouter.Router
	Usecase *providers.Usecase
}

func newObjectStorage(cfg config.R2Config) services.IObjectStorage {
	if !cfg.Enabled() {
		return r2.Unavailable{}
	}
	return r2.New(r2.Settings{
		Endpoint: cfg.ResolvedEndpoint(), AccessKeyID: cfg.AccessKeyID,
		SecretKey: cfg.SecretAccessKey, Bucket: cfg.Bucket, PresignTTL: cfg.PresignTTL,
	})
}

func newProvidersModule(c *Container, storage services.IObjectStorage) *ProvidersModule {
	uc := providers.NewUsecase(providers.Deps{
		Users:         repository.NewUserRepository(c.Pool),
		Providers:     repository.NewProviderRepository(c.Pool),
		Documents:     repository.NewProviderDocumentRepository(c.Pool),
		Requests:      repository.NewRequestRepository(c.Pool),
		Locator:       c.Dispatch.Locator,
		Outbox:        c.Outbox,
		Audit:         repository.NewAuditLogRepository(c.Pool),
		Tx:            c.TxManager,
		Codes:         c.Auth.CodeSender,
		Storage:       storage,
		Approvals:     c.Verification.Usecase,
		Settings:      providers.Settings{MaxFileBytes: c.Config.Verify.MaxFileBytes},
		Business:      c.Business,
		ActivityStats: repository.NewActivityRepository(c.Pool),
	})
	return &ProvidersModule{
		Usecase: uc,
		Router:  providersrouter.NewRouter(controllers.NewProviderController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, authLimiter(c, "provider-register")),
	}
}
