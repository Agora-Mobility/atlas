package containers

import (
	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/payment/appypay"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/payments/controllers"
	paymentsrouter "github.com/Agora-Mobility/atlas/infra/rest/payments/router"
	"github.com/Agora-Mobility/atlas/usecases/payments"
)

type PaymentsModule struct {
	Router  *paymentsrouter.Router
	Usecase *payments.Usecase
}

func newPaymentGateway(cfg config.AppyPayConfig) services.IPaymentGateway {
	if !cfg.Enabled() {
		return appypay.Unavailable{}
	}
	return appypay.New(appypay.Settings{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, BaseURL: cfg.BaseURL, TokenURL: cfg.TokenURL, Resource: cfg.Resource,
		GPOMethod: cfg.GPOMethod, REFMethod: cfg.REFMethod, Timeout: cfg.Timeout,
	})
}

func newPaymentsModule(c *Container) *PaymentsModule {
	cfg := c.Config.AppyPay
	uc := payments.NewUsecase(payments.Deps{
		Payments: repository.NewPaymentRepository(c.Pool),
		Requests: repository.NewRequestRepository(c.Pool),
		Users:    repository.NewUserRepository(c.Pool),
		Outbox:   c.Outbox,
		Audit:    repository.NewAuditLogRepository(c.Pool),
		Tx:       c.TxManager,
		Gateway:  newPaymentGateway(cfg),
		Ledger:   repository.NewLedgerRepository(c.Pool),
		Settings: repository.NewReferenceRepository(c.Pool),
	}, payments.Settings{
		Currency:  cfg.LedgerCurrency,
		GPOWindow: cfg.GPOWindow, ReferenceTTL: cfg.ReferenceTTL, ReconcileInterval: cfg.ReconcileInterval, ReconcileBatch: cfg.ReconcileBatch,
		GPOCheckInterval: cfg.GPOCheckInterval, ReferenceCheckInterval: cfg.ReferenceCheckInterval,
	})
	return &PaymentsModule{Usecase: uc, Router: paymentsrouter.NewRouter(controllers.NewPaymentController(uc), c.Authenticator, c.Auth.Authorizer, c.ClientAuthorizer, middleware.NewIdempotency(c.Redis, c.Config.Dispatch.IdempotencyTTL))}
}
