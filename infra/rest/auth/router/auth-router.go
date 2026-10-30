package auth_router

import (
	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/auth/controllers"
)

// Limiters are the rate limits of the auth routes, each in its own scope.
type Limiters struct {
	Register gin.HandlerFunc
	Login    gin.HandlerFunc
	Phone    gin.HandlerFunc
	Recovery gin.HandlerFunc
}

type Controllers struct {
	Auth        *controllers.AuthController
	Profile     *controllers.ProfileController
	UserAdmin   *controllers.UserAdminController
	Members     *controllers.MemberController
	Partners    *controllers.PartnerController
	Invitations *controllers.InvitationController
	MFA         *controllers.MFAController
	Roles       *controllers.RoleController
}

type Router struct {
	controller       *controllers.AuthController
	profile          *controllers.ProfileController
	userAdmin        *controllers.UserAdminController
	members          *controllers.MemberController
	partners         *controllers.PartnerController
	invitations      *controllers.InvitationController
	mfa              *controllers.MFAController
	roles            *controllers.RoleController
	authenticator    *middleware.Authenticator
	authorizer       *middleware.Authorizer
	clientAuthorizer *middleware.ClientAuthorizer
	limiters         Limiters
}

func NewRouter(
	c Controllers,
	authenticator *middleware.Authenticator,
	authorizer *middleware.Authorizer,
	clientAuthorizer *middleware.ClientAuthorizer,
	limiters Limiters,
) *Router {
	return &Router{
		controller:       c.Auth,
		profile:          c.Profile,
		userAdmin:        c.UserAdmin,
		members:          c.Members,
		partners:         c.Partners,
		invitations:      c.Invitations,
		mfa:              c.MFA,
		roles:            c.Roles,
		authenticator:    authenticator,
		authorizer:       authorizer,
		clientAuthorizer: clientAuthorizer,
		limiters:         limiters,
	}
}

func (r *Router) Register(api *gin.RouterGroup) {
	clients := r.clientAuthorizer.RequireClient()
	authenticated := r.authenticator.RequireAuth()

	authGroup := api.Group("/auth", clients)
	authGroup.POST("/register", r.limiters.Register, r.controller.Register)
	authGroup.POST("/login", r.limiters.Login, r.controller.Login)
	authGroup.POST("/refresh", r.controller.Refresh)

	phone := authGroup.Group("/phone", r.limiters.Phone)
	phone.POST("/verification-code", r.controller.RequestPhoneVerification)
	phone.POST("/verify", r.controller.ConfirmPhoneVerification)

	recovery := authGroup.Group("/recovery", r.limiters.Recovery)
	recovery.POST("/request", r.controller.RequestRecovery)
	recovery.POST("/confirm", r.controller.ConfirmRecovery)

	authGroup.POST("/logout", authenticated, r.controller.Logout)
	authGroup.POST("/logout-all", authenticated, r.controller.LogoutAll)

	users := api.Group("/users", clients, authenticated)
	users.GET("/me", r.controller.Me)
	users.PATCH("/me", r.profile.UpdateProfile)
	users.DELETE("/me", r.profile.DeleteAccount)
	users.POST("/me/phone/verification-code", r.limiters.Phone, r.profile.RequestPhoneChange)
	users.PUT("/me/phone", r.limiters.Phone, r.profile.ConfirmPhoneChange)
	users.POST("/password", r.controller.SetPassword)
	users.PUT("/password", r.controller.ChangePassword)

	users.GET("/me/permissions", r.authorizer.RequireMembership(), r.members.MyAccess)
	users.POST("/me/mfa/recovery-codes", r.limiters.Login, r.authorizer.RequireMembership(), r.mfa.RegenerateRecoveryCodes)

	mfa := authGroup.Group("/mfa", r.limiters.Login)
	mfa.POST("/setup", r.mfa.Setup)
	mfa.POST("/verify", r.mfa.Verify)
	mfa.POST("/recover", r.mfa.Recover)

	invitations := api.Group("/invitations", clients, r.limiters.Phone)
	invitations.POST("/verification-code", r.invitations.RequestCode)
	invitations.POST("/accept", r.invitations.Accept)

	can := r.authorizer.RequirePermission
	staff := api.Group("", clients, authenticated)
	staff.GET("/roles", can(entities.PermRolesRead), r.members.ListRoles)
	staff.POST("/roles", can(entities.PermRolesManage), r.roles.Create)
	staff.GET("/roles/:id", can(entities.PermRolesRead), r.roles.Get)
	staff.PATCH("/roles/:id", can(entities.PermRolesManage), r.roles.Update)
	staff.DELETE("/roles/:id", can(entities.PermRolesManage), r.roles.Delete)
	staff.GET("/permissions", can(entities.PermRolesRead), r.members.ListPermissions)

	members := staff.Group("/members")
	members.GET("", can(entities.PermMembersRead), r.members.List)
	members.POST("/invitations", can(entities.PermMembersInvite), r.members.Invite)
	members.GET("/:id", can(entities.PermMembersRead), r.members.Get)
	members.PATCH("/:id", can(entities.PermMembersUpdate), r.members.Update)
	members.PUT("/:id/roles", can(entities.PermRolesAssign), r.members.ReplaceRoles)
	members.POST("/:id/suspend", can(entities.PermMembersSuspend), r.members.Suspend)
	members.POST("/:id/reactivate", can(entities.PermMembersSuspend), r.members.Reactivate)
	members.POST("/:id/deactivate", can(entities.PermMembersDeactivate), r.members.Deactivate)
	members.POST("/:id/invitations/resend", can(entities.PermMembersInvite), r.members.ResendInvitation)
	members.POST("/:id/mfa/reset", can(entities.PermMembersMFAReset), r.members.ResetMFA)

	partners := staff.Group("/partners")
	partners.GET("", can(entities.PermPartnersRead), r.partners.List)
	partners.POST("", can(entities.PermPartnersManage), r.partners.Create)
	partners.GET("/:orgId/roles", can(entities.PermPartnersRead), r.partners.ListRoles)
	partners.GET("/:orgId/members", can(entities.PermPartnersRead), r.partners.ListMembers)
	partners.POST("/:orgId/members/invitations", can(entities.PermPartnersManage), r.partners.InviteMember)

	staff.GET("/admin/block-reasons", can(entities.PermUsersBlock), r.userAdmin.ListBlockReasons)
	admin := staff.Group("/admin/users")
	admin.GET("", can(entities.PermUsersRead), r.userAdmin.List)
	admin.GET("/:id", can(entities.PermUsersRead), r.userAdmin.Get)
	admin.POST("/:id/block", can(entities.PermUsersBlock), r.userAdmin.Block)
	admin.POST("/:id/unblock", can(entities.PermUsersBlock), r.userAdmin.Unblock)
	admin.PUT("/:id/password", can(entities.PermUsersResetPassword), r.userAdmin.ResetPassword)
}
