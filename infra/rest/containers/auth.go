package containers

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/auth/controllers"
	authrouter "github.com/Agora-Mobility/atlas/infra/rest/auth/router"
	"github.com/Agora-Mobility/atlas/infra/security/accesscache"
	"github.com/Agora-Mobility/atlas/infra/security/opaque"
	"github.com/Agora-Mobility/atlas/infra/security/otp"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/infra/security/recoveryticket"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

type AuthModule struct {
	Controller *controllers.AuthController
	Router     *authrouter.Router
	// Authorizer checks the permissions of staff routes in every module.
	Authorizer *middleware.Authorizer
	CodeSender *usecases.VerificationCodeSender
}

func newAuthModule(c *Container) (*AuthModule, error) {
	cfg := c.Config
	namespace := cfg.App.Name + ":"

	userRepo := repository.NewUserRepository(c.Pool)
	refreshTokenRepo := repository.NewRefreshTokenRepository(c.Pool)
	auditRepo := repository.NewAuditLogRepository(c.Pool)
	blockReasonRepo := repository.NewBlockReasonRepository(c.Pool)
	orgRepo := repository.NewOrganizationRepository(c.Pool)
	roleRepo := repository.NewRoleRepository(c.Pool)
	membershipRepo := repository.NewMembershipRepository(c.Pool)
	invitationRepo := repository.NewInvitationRepository(c.Pool)
	hasher := password.NewArgon2Hasher(password.DefaultParams)
	opaqueTokens := opaque.TokenGenerator{}
	otpService := otp.NewRedisOTPService(c.Redis, namespace, otp.Settings{
		TTL:            cfg.OTP.TTL,
		MaxAttempts:    cfg.OTP.MaxAttempts,
		ResendCooldown: cfg.OTP.ResendCooldown,
	})

	accessResolver := usecases.NewAccessResolver(membershipRepo, accesscache.NewRedisCache(c.Redis, namespace, cfg.Access.PermissionCacheTTL))
	codeSender := usecases.NewVerificationCodeSender(otpService, c.SMS, cfg.OTP.TTL)
	sessions := usecases.NewSessionIssuer(userRepo, refreshTokenRepo, c.TxManager, c.JWT, opaqueTokens, cfg.JWT.RefreshTTL)
	revoker := usecases.NewSessionRevoker(refreshTokenRepo, c.SessionRevocations, cfg.JWT.AccessTTL)
	staffSignIn := usecases.NewStaffSignIn(userRepo, membershipRepo, sessions)

	controller := controllers.NewAuthController(
		usecases.NewAuthUsecase(userRepo, c.Outbox, c.TxManager, hasher, codeSender, sessions, staffSignIn, revoker),
		usecases.NewRefreshUsecase(refreshTokenRepo, userRepo, accessResolver, c.TxManager, opaqueTokens, sessions, revoker, cfg.JWT.RefreshTTL),
		usecases.NewPhoneVerificationUsecase(userRepo, c.Outbox, c.TxManager, otpService, codeSender, sessions),
		usecases.NewRecoveryUsecase(userRepo, c.Outbox, c.TxManager, hasher, otpService, codeSender, revoker,
			recoveryticket.NewRedisRecoveryTicketService(c.Redis, namespace, cfg.OTP.TTL), c.Email, cfg.OTP.ResendCooldown),
	)
	members := usecases.NewMemberUsecase(usecases.MemberStores{
		Users: userRepo, Orgs: orgRepo, Roles: roleRepo, Memberships: membershipRepo, Invitations: invitationRepo,
		Audit: auditRepo, Outbox: c.Outbox, Tx: c.TxManager,
	}, opaqueTokens, c.SMS, c.Email, revoker, accessResolver, usecases.InvitationLinks{
		Platform: cfg.Access.InvitationBackoffice,
		Partner:  cfg.Access.InvitationPartner,
	})
	organizations := usecases.NewOrganizationUsecase(orgRepo, roleRepo, membershipRepo, auditRepo, c.Outbox, c.TxManager)
	authorizer := middleware.NewAuthorizer(accessResolver)

	return &AuthModule{
		Controller: controller,
		Authorizer: authorizer,
		CodeSender: codeSender,
		Router: authrouter.NewRouter(authrouter.Controllers{
			Auth: controller,
			Profile: controllers.NewProfileController(
				usecases.NewProfileUsecase(userRepo, c.Outbox, c.TxManager, hasher, otpService, codeSender, revoker),
			),
			UserAdmin: controllers.NewUserAdminController(
				usecases.NewUserAdminUsecase(userRepo, c.Outbox, auditRepo, blockReasonRepo, c.TxManager, hasher, revoker),
			),
			Members:  controllers.NewMemberController(members, organizations),
			Partners: controllers.NewPartnerController(members, organizations),
			Invitations: controllers.NewInvitationController(usecases.NewInvitationUsecase(
				userRepo, orgRepo, membershipRepo, invitationRepo, auditRepo, c.Outbox, c.TxManager, hasher, opaqueTokens, otpService, codeSender, accessResolver,
			)),
			Roles: controllers.NewRoleController(usecases.NewRoleUsecase(roleRepo, auditRepo, c.Outbox, c.TxManager, accessResolver)),
		}, c.Authenticator, authorizer, c.ClientAuthorizer, authrouter.Limiters{
			Register: authLimiter(c, "register"),
			Login:    authLimiter(c, "login"),
			Phone:    authLimiter(c, "phone"),
			Recovery: authLimiter(c, "recovery"),
		}),
	}, nil
}

func authLimiter(c *Container, scope string) gin.HandlerFunc {
	return middleware.ScopedRateLimiter(c.Redis, scope, c.Config.HTTP.AuthRateLimit, c.Config.HTTP.AuthRateWindow)
}
