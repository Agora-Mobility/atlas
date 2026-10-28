package containers

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/auth/controllers"
	authrouter "github.com/Agora-Mobility/atlas/infra/rest/auth/router"
	"github.com/Agora-Mobility/atlas/infra/security/opaque"
	"github.com/Agora-Mobility/atlas/infra/security/otp"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

type AuthModule struct {
	Controller *controllers.AuthController
	Router     *authrouter.Router
}

func newAuthModule(c *Container) *AuthModule {
	cfg := c.Config
	userRepo := repository.NewUserRepository(c.Pool)
	refreshTokenRepo := repository.NewRefreshTokenRepository(c.Pool)
	hasher := password.NewArgon2Hasher(password.DefaultParams)
	opaqueTokens := opaque.TokenGenerator{}
	otpService := otp.NewRedisOTPService(c.Redis, cfg.App.Name+":", otp.Settings{
		TTL:            cfg.OTP.TTL,
		MaxAttempts:    cfg.OTP.MaxAttempts,
		ResendCooldown: cfg.OTP.ResendCooldown,
	})

	codeSender := usecases.NewVerificationCodeSender(otpService, c.SMS, cfg.OTP.TTL)
	sessions := usecases.NewSessionIssuer(userRepo, refreshTokenRepo, c.TxManager, c.JWT, opaqueTokens, cfg.JWT.RefreshTTL)

	controller := controllers.NewAuthController(
		usecases.NewAuthUsecase(userRepo, refreshTokenRepo, c.Outbox, c.TxManager, hasher, codeSender, sessions),
		usecases.NewRefreshUsecase(refreshTokenRepo, userRepo, c.TxManager, opaqueTokens, sessions, cfg.JWT.RefreshTTL),
		usecases.NewPhoneVerificationUsecase(userRepo, c.Outbox, c.TxManager, otpService, codeSender, sessions),
		usecases.NewRecoveryUsecase(userRepo, refreshTokenRepo, c.Outbox, c.TxManager, hasher, otpService, codeSender),
	)

	return &AuthModule{
		Controller: controller,
		Router: authrouter.NewRouter(
			controller,
			c.Authenticator,
			c.ClientAuthorizer,
			authLimiter(c, "register"),
			authLimiter(c, "login"),
			authLimiter(c, "recovery"),
		),
	}
}

func authLimiter(c *Container, scope string) gin.HandlerFunc {
	return middleware.ScopedRateLimiter(c.Redis, scope, c.Config.HTTP.AuthRateLimit, c.Config.HTTP.AuthRateWindow)
}
