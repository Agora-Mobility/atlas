package auth_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/auth/controllers"
)

type Router struct {
	controller       *controllers.AuthController
	authenticator    *middleware.Authenticator
	clientAuthorizer *middleware.ClientAuthorizer
	registerLimiter  gin.HandlerFunc
	loginLimiter     gin.HandlerFunc
	recoveryLimiter  gin.HandlerFunc
}

func NewRouter(
	controller *controllers.AuthController,
	authenticator *middleware.Authenticator,
	clientAuthorizer *middleware.ClientAuthorizer,
	registerLimiter gin.HandlerFunc,
	loginLimiter gin.HandlerFunc,
	recoveryLimiter gin.HandlerFunc,
) *Router {
	return &Router{
		controller:       controller,
		authenticator:    authenticator,
		clientAuthorizer: clientAuthorizer,
		registerLimiter:  registerLimiter,
		loginLimiter:     loginLimiter,
		recoveryLimiter:  recoveryLimiter,
	}
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()
	authenticated := r.authenticator.RequireAuth()

	authGroup := api.Group("/auth", clients)
	authGroup.POST("/register", r.registerLimiter, r.controller.Register)
	authGroup.POST("/login", r.loginLimiter, r.controller.Login)
	authGroup.POST("/refresh", r.controller.Refresh)

	phone := authGroup.Group("/phone", r.recoveryLimiter)
	phone.POST("/verification-code", r.controller.RequestPhoneVerification)
	phone.POST("/verify", r.controller.ConfirmPhoneVerification)

	recovery := authGroup.Group("/recovery", r.recoveryLimiter)
	recovery.POST("/request", r.controller.RequestRecovery)
	recovery.POST("/confirm", r.controller.ConfirmRecovery)

	authGroup.POST("/logout", authenticated, r.controller.Logout)
	authGroup.POST("/logout-all", authenticated, r.controller.LogoutAll)

	users := api.Group("/users", clients, authenticated)
	users.GET("/me", r.controller.Me)
	users.PUT("/password", r.controller.ChangePassword)
}
