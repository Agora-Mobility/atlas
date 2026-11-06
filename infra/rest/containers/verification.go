package containers

import (
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/repository"
	verificationrest "github.com/Agora-Mobility/atlas/infra/rest/verification"
	"github.com/Agora-Mobility/atlas/infra/verification"
	usecase "github.com/Agora-Mobility/atlas/usecases/verification"
)

type VerificationModule struct {
	Router  *verificationrest.Router
	Usecase *usecase.Usecase
	workers int
}

func newVerificationModule(c *Container, storage services.IObjectStorage) *VerificationModule {
	cfg := c.Config.Verify
	fake := verification.FakeSettings{QualityReasons: cfg.FakeQualityReasons, FailOCR: cfg.FakeOCRFail, FailLiveness: cfg.FakeLivenessFail}
	uc := usecase.NewUsecase(usecase.Deps{
		Users:         repository.NewUserRepository(c.Pool),
		Providers:     repository.NewProviderRepository(c.Pool),
		Documents:     repository.NewProviderDocumentRepository(c.Pool),
		Verifications: repository.NewVerificationRepository(c.Pool),
		Outbox:        c.Outbox,
		Audit:         repository.NewAuditLogRepository(c.Pool),
		Tx:            c.TxManager,
		Storage:       storage,
		Quality:       verification.NewQualityChecker(cfg.QualityProvider, fake),
		OCR:           verification.NewOCRProvider(cfg.OCRProvider, fake),
		Liveness:      verification.NewLivenessProvider(cfg.LivenessProvider, fake),
		Settings: usecase.Settings{
			MaxFileBytes: cfg.MaxFileBytes, ReadURLTTL: cfg.ReadURLTTL, MinProviderAge: cfg.MinProviderAge, MaxAttempts: cfg.MaxAttempts,
			CriminalRecordMaxDays: cfg.CriminalRecordMaxDays, CertificateMaxMonths: cfg.CertificateMaxMonths, ExpiryNoticeDays: cfg.ExpiryNoticeDays,
			PendingUploadTTL: cfg.PendingUploadTTL, ReminderDelay: cfg.ReminderDelay, HousekeepingInterval: cfg.HousekeepingInterval, Currency: c.Config.AppyPay.LedgerCurrency,
		},
	})
	limiters := verificationrest.Limiters{
		Uploads: middleware.ScopedRateLimiter(c.Redis, "verification-uploads", cfg.UploadRateLimit, cfg.RateWindow),
		Checks:  middleware.ScopedRateLimiter(c.Redis, "verification-checks", cfg.CheckRateLimit, cfg.RateWindow),
	}
	return &VerificationModule{
		Usecase: uc,
		workers: cfg.Workers,
		Router: verificationrest.NewRouter(verificationrest.NewController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer,
			middleware.NewIdempotency(c.Redis, c.Config.Dispatch.IdempotencyTTL), limiters),
	}
}

func (m *VerificationModule) Consumers() []queue.ConsumerConfig {
	return []queue.ConsumerConfig{
		{
			Queue:       "providers.document-processing",
			BindingKeys: []string{events.RoutingKey(events.ProviderDocumentUploaded, 1)},
			Handler:     m.Usecase.OnDocumentUploaded,
			Workers:     m.workers,
		},
		{
			Queue:       "providers.identity-reading",
			BindingKeys: []string{events.RoutingKey(events.ProviderIdentityReadRequested, 1)},
			Handler:     m.Usecase.OnIdentityReadRequested,
			Workers:     m.workers,
		},
	}
}
