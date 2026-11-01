package containers

import (
	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/onboarding/controllers"
	onboardingrouter "github.com/Agora-Mobility/atlas/infra/rest/onboarding/router"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/infra/storage/r2"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/onboarding"
)

type OnboardingModule struct {
	Router *onboardingrouter.Router
}

func newObjectStorage(cfg config.R2Config) services.IObjectStorage {
	if !cfg.Enabled() {
		return r2.Unavailable{}
	}
	return r2.New(r2.Settings{
		Endpoint: cfg.ResolvedEndpoint(), PresignEndpoint: cfg.PresignEndpoint, AccessKeyID: cfg.AccessKeyID,
		SecretKey: cfg.SecretAccessKey, Bucket: cfg.Bucket, PresignTTL: cfg.PresignTTL,
	})
}

func newOnboardingModule(c *Container, storage services.IObjectStorage) *OnboardingModule {
	uc := onboarding.NewUsecase(onboarding.Deps{
		Users:      repository.NewUserRepository(c.Pool),
		Profiles:   repository.NewProviderRepository(c.Pool),
		Onboarding: repository.NewOnboardingRepository(c.Pool),
		Requests:   repository.NewRequestRepository(c.Pool),
		Locator:    c.Dispatch.Locator,
		Outbox:     c.Outbox,
		Audit:      repository.NewAuditLogRepository(c.Pool),
		Tx:         c.TxManager,
		Hasher:     password.NewArgon2Hasher(password.DefaultParams),
		Codes:      c.Auth.CodeSender,
		Storage:    storage,
		Passwords:  usecases.ValidatePassword,
	})
	return &OnboardingModule{
		Router: onboardingrouter.NewRouter(controllers.NewOnboardingController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, authLimiter(c, "provider-register")),
	}
}
