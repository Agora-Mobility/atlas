package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type MemberController struct {
	members       *usecases.MemberUsecase
	organizations *usecases.OrganizationUsecase
}

func NewMemberController(members *usecases.MemberUsecase, organizations *usecases.OrganizationUsecase) *MemberController {
	return &MemberController{members: members, organizations: organizations}
}

// MyAccess godoc
//
//	@Summary		Effective permissions of the signed-in member
//	@Description	For the UI only: every endpoint checks its own permission.
//	@Tags			Members
//	@Produce		json
//	@Success		200	{object}	dtos.MyAccessView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Not a staff session"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/permissions [get]
func (c *MemberController) MyAccess(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	resp, err := c.organizations.MyAccess(ctx.Request.Context(), actor)
	respond(ctx, http.StatusOK, resp, err)
}

// ListRoles godoc
//
//	@Summary		List the roles of the organization
//	@Description	Requires roles:read. System roles of the scope and the roles of the organization.
//	@Tags			Members
//	@Produce		json
//	@Success		200	{object}	dtos.RoleList
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/roles [get]
func (c *MemberController) ListRoles(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	resp, err := c.organizations.ListRoles(ctx.Request.Context(), actor)
	respond(ctx, http.StatusOK, resp, err)
}

// ListPermissions godoc
//
//	@Summary		Permission catalog of the scope
//	@Description	Requires roles:read.
//	@Tags			Members
//	@Produce		json
//	@Success		200	{object}	dtos.PermissionList
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/permissions [get]
func (c *MemberController) ListPermissions(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	resp, err := c.organizations.ListPermissions(ctx.Request.Context(), actor)
	respond(ctx, http.StatusOK, resp, err)
}

// List godoc
//
//	@Summary		List the members of the organization
//	@Description	Requires members:read. The organization is always the one of the session.
//	@Tags			Members
//	@Produce		json
//	@Param			status	query		string	false	"Status"	Enums(invited, active, suspended, deactivated)
//	@Param			role_id	query		string	false	"Role id"
//	@Param			search	query		string	false	"Part of the name, e-mail or phone"
//	@Param			page	query		int		false	"Page (from 1)"
//	@Param			size	query		int		false	"Page size (max 100)"
//	@Success		200		{object}	dtos.MemberPage
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid filters"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members [get]
func (c *MemberController) List(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var query dtos.MemberListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.List(ctx.Request.Context(), actor, query)
	respond(ctx, http.StatusOK, resp, err)
}

// Get godoc
//
//	@Summary		Get a member
//	@Description	Requires members:read. A member of another organization answers 404.
//	@Tags			Members
//	@Produce		json
//	@Param			id	path		string	true	"Member (membership) id"
//	@Success		200	{object}	dtos.MemberView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"Member not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id} [get]
func (c *MemberController) Get(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	resp, err := c.members.Get(ctx.Request.Context(), actor, memberID)
	respond(ctx, http.StatusOK, resp, err)
}

// Invite godoc
//
//	@Summary		Invite a member
//	@Description	Requires members:invite. The invitation goes by SMS, lasts 48 h and is accepted at POST /api/v1/invitations/accept. You can only grant roles whose permissions you hold.
//	@Tags			Members
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.InviteMemberInput	true	"Invitation"
//	@Success		201		{object}	dtos.MemberView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or privilege escalation"
//	@Failure		404		{object}	shared.ErrorResponse	"Role not found"
//	@Failure		409		{object}	shared.ErrorResponse	"Already a member, or the identity belongs to a customer or provider"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or role of another scope"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/invitations [post]
func (c *MemberController) Invite(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var req dtos.InviteMemberInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.Invite(ctx.Request.Context(), actor, req)
	respond(ctx, http.StatusCreated, resp, err)
}

// ResendInvitation godoc
//
//	@Summary		Resend the invitation
//	@Description	Requires members:invite. The previous invitation stops working.
//	@Tags			Members
//	@Param			id	path	string	true	"Member (membership) id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"Member not found"
//	@Failure		409	{object}	shared.ErrorResponse	"The member is not invited"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id}/invitations/resend [post]
func (c *MemberController) ResendInvitation(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	if err := c.members.ResendInvitation(ctx.Request.Context(), actor, memberID); err != nil {
		handleAccessError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

// Update godoc
//
//	@Summary		Change the name or phone of a member
//	@Description	Requires members:update.
//	@Tags			Members
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Member (membership) id"
//	@Param			body	body		dtos.UpdateMemberInput	true	"Fields to change"
//	@Success		200		{object}	dtos.MemberView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or privilege escalation"
//	@Failure		404		{object}	shared.ErrorResponse	"Member not found"
//	@Failure		409		{object}	shared.ErrorResponse	"Phone in use or member deactivated"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id} [patch]
func (c *MemberController) Update(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	var req dtos.UpdateMemberInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.Update(ctx.Request.Context(), actor, memberID, req)
	respond(ctx, http.StatusOK, resp, err)
}

// ReplaceRoles godoc
//
//	@Summary		Replace the roles of a member
//	@Description	Requires roles:assign. The list replaces the current roles and takes effect on the next request. You cannot change your own roles, grant permissions you do not hold, or remove the last active owner.
//	@Tags			Members
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Member (membership) id"
//	@Param			body	body		dtos.ReplaceRolesInput	true	"Roles"
//	@Success		200		{object}	dtos.MemberView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission, privilege escalation or own account"
//	@Failure		404		{object}	shared.ErrorResponse	"Member or role not found"
//	@Failure		409		{object}	shared.ErrorResponse	"Last owner or member deactivated"
//	@Failure		422		{object}	shared.ErrorResponse	"Role of another scope"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id}/roles [put]
func (c *MemberController) ReplaceRoles(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	var req dtos.ReplaceRolesInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.ReplaceRoles(ctx.Request.Context(), actor, memberID, req)
	respond(ctx, http.StatusOK, resp, err)
}

// Suspend godoc
//
//	@Summary		Suspend a member
//	@Description	Requires members:suspend. Ends every session of the person at once.
//	@Tags			Members
//	@Produce		json
//	@Param			id	path		string	true	"Member (membership) id"
//	@Success		200	{object}	dtos.MemberView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission, privilege escalation or own account"
//	@Failure		404	{object}	shared.ErrorResponse	"Member not found"
//	@Failure		409	{object}	shared.ErrorResponse	"Last owner or invalid state"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id}/suspend [post]
func (c *MemberController) Suspend(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	resp, err := c.members.Suspend(ctx.Request.Context(), actor, memberID)
	respond(ctx, http.StatusOK, resp, err)
}

// Reactivate godoc
//
//	@Summary		Reactivate a suspended member
//	@Description	Requires members:suspend.
//	@Tags			Members
//	@Produce		json
//	@Param			id	path		string	true	"Member (membership) id"
//	@Success		200	{object}	dtos.MemberView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission, privilege escalation or own account"
//	@Failure		404	{object}	shared.ErrorResponse	"Member not found"
//	@Failure		409	{object}	shared.ErrorResponse	"Invalid state"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id}/reactivate [post]
func (c *MemberController) Reactivate(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	resp, err := c.members.Reactivate(ctx.Request.Context(), actor, memberID)
	respond(ctx, http.StatusOK, resp, err)
}

// Deactivate godoc
//
//	@Summary		Deactivate a member
//	@Description	Requires members:deactivate. Final: the membership cannot be reactivated. Ends sessions and pending invitations.
//	@Tags			Members
//	@Produce		json
//	@Param			id	path		string	true	"Member (membership) id"
//	@Success		200	{object}	dtos.MemberView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission, privilege escalation or own account"
//	@Failure		404	{object}	shared.ErrorResponse	"Member not found"
//	@Failure		409	{object}	shared.ErrorResponse	"Last owner or already deactivated"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/members/{id}/deactivate [post]
func (c *MemberController) Deactivate(ctx *gin.Context) {
	actor, memberID, ok := memberRequest(ctx)
	if !ok {
		return
	}
	resp, err := c.members.Deactivate(ctx.Request.Context(), actor, memberID)
	respond(ctx, http.StatusOK, resp, err)
}

type PartnerController struct {
	members       *usecases.MemberUsecase
	organizations *usecases.OrganizationUsecase
}

func NewPartnerController(members *usecases.MemberUsecase, organizations *usecases.OrganizationUsecase) *PartnerController {
	return &PartnerController{members: members, organizations: organizations}
}

// Create godoc
//
//	@Summary		Create a partner organization
//	@Description	Requires partners:manage (AGORA only). The NIF is optional: 10 digits, or the BI number for a sole trader; spaces are ignored. Invite its first owner at POST /api/v1/partners/{orgId}/members/invitations.
//	@Tags			Partners
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CreatePartnerInput	true	"Partner"
//	@Success		201		{object}	dtos.OrganizationView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		409		{object}	shared.ErrorResponse	"NIF already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or NIF"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners [post]
func (c *PartnerController) Create(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var req dtos.CreatePartnerInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.organizations.CreatePartner(ctx.Request.Context(), actor, req)
	respond(ctx, http.StatusCreated, resp, err)
}

// Update godoc
//
//	@Summary		Update a partner organization
//	@Description	Requires partners:manage (AGORA only). Changes only the fields sent; an empty nif removes it. Spaces in the NIF are ignored.
//	@Tags			Partners
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string					true	"Partner organization id"
//	@Param			body	body		dtos.UpdatePartnerInput	true	"Fields to change"
//	@Success		200		{object}	dtos.OrganizationView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Partner not found"
//	@Failure		409		{object}	shared.ErrorResponse	"NIF already registered"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or NIF"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners/{orgId} [patch]
func (c *PartnerController) Update(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	orgID, ok := organizationIDParam(ctx)
	if !ok {
		return
	}
	var req dtos.UpdatePartnerInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.organizations.UpdatePartner(ctx.Request.Context(), actor, orgID, req)
	respond(ctx, http.StatusOK, resp, err)
}

// Get godoc
//
//	@Summary		Get a partner organization
//	@Description	Requires partners:read (AGORA only).
//	@Tags			Partners
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner organization id"
//	@Success		200		{object}	dtos.OrganizationView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Partner not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners/{orgId} [get]
func (c *PartnerController) Get(ctx *gin.Context) {
	orgID, ok := organizationIDParam(ctx)
	if !ok {
		return
	}
	resp, err := c.organizations.GetPartner(ctx.Request.Context(), orgID)
	respond(ctx, http.StatusOK, resp, err)
}

// List godoc
//
//	@Summary		List partner organizations
//	@Description	Requires partners:read (AGORA only).
//	@Tags			Partners
//	@Produce		json
//	@Param			search	query		string	false	"Part of the name or NIF"
//	@Param			page	query		int		false	"Page (from 1)"
//	@Param			size	query		int		false	"Page size (max 100)"
//	@Success		200		{object}	dtos.OrganizationPage
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners [get]
func (c *PartnerController) List(ctx *gin.Context) {
	var query dtos.PartnerListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.organizations.ListPartners(ctx.Request.Context(), query)
	respond(ctx, http.StatusOK, resp, err)
}

// ListRoles godoc
//
//	@Summary		List the roles of a partner
//	@Description	Requires partners:read (AGORA only). Use them to invite the members of the partner.
//	@Tags			Partners
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner organization id"
//	@Success		200		{object}	dtos.RoleList
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Partner not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners/{orgId}/roles [get]
func (c *PartnerController) ListRoles(ctx *gin.Context) {
	orgID, ok := organizationIDParam(ctx)
	if !ok {
		return
	}
	resp, err := c.organizations.ListPartnerRoles(ctx.Request.Context(), orgID)
	respond(ctx, http.StatusOK, resp, err)
}

// ListMembers godoc
//
//	@Summary		List the members of a partner
//	@Description	Requires partners:read (AGORA only).
//	@Tags			Partners
//	@Produce		json
//	@Param			orgId	path		string	true	"Partner organization id"
//	@Param			status	query		string	false	"Status"	Enums(invited, active, suspended, deactivated)
//	@Param			role_id	query		string	false	"Role id"
//	@Param			search	query		string	false	"Part of the name, e-mail or phone"
//	@Param			page	query		int		false	"Page (from 1)"
//	@Param			size	query		int		false	"Page size (max 100)"
//	@Success		200		{object}	dtos.MemberPage
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Partner not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners/{orgId}/members [get]
func (c *PartnerController) ListMembers(ctx *gin.Context) {
	orgID, ok := organizationIDParam(ctx)
	if !ok {
		return
	}
	var query dtos.MemberListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.ListPartnerMembers(ctx.Request.Context(), orgID, query)
	respond(ctx, http.StatusOK, resp, err)
}

// InviteMember godoc
//
//	@Summary		Invite a member to a partner
//	@Description	Requires partners:manage (AGORA only), for example to invite the first owner. Roles must be of the partner scope.
//	@Tags			Partners
//	@Accept			json
//	@Produce		json
//	@Param			orgId	path		string					true	"Partner organization id"
//	@Param			body	body		dtos.InviteMemberInput	true	"Invitation"
//	@Success		201		{object}	dtos.MemberView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404		{object}	shared.ErrorResponse	"Partner or role not found"
//	@Failure		409		{object}	shared.ErrorResponse	"Already a member, or the identity belongs to a customer or provider"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or role of another scope"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/partners/{orgId}/members/invitations [post]
func (c *PartnerController) InviteMember(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	orgID, ok := organizationIDParam(ctx)
	if !ok {
		return
	}
	var req dtos.InviteMemberInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.members.InviteToPartner(ctx.Request.Context(), actor, orgID, req)
	respond(ctx, http.StatusCreated, resp, err)
}

type InvitationController struct {
	invitations *usecases.InvitationUsecase
}

func NewInvitationController(invitations *usecases.InvitationUsecase) *InvitationController {
	return &InvitationController{invitations: invitations}
}

// RequestCode godoc
//
//	@Summary		Send the activation code of an invitation
//	@Description	Sends an SMS code to the phone of the invited person. Use the client of the invitation (agora-backoffice or agora-partner-panel).
//	@Tags			Invitations
//	@Accept			json
//	@Param			body	body	dtos.InvitationTokenInput	true	"Invitation token"
//	@Success		202		"Accepted"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid invitation"
//	@Failure		429		{object}	shared.ErrorResponse	"Code sent recently"
//	@Security		ClientId
//	@Router			/api/v1/invitations/verification-code [post]
func (c *InvitationController) RequestCode(ctx *gin.Context) {
	client, ok := requestClient(ctx)
	if !ok {
		return
	}
	var req dtos.InvitationTokenInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	if err := c.invitations.RequestCode(ctx.Request.Context(), client, req); err != nil {
		handleAccessError(ctx, err)
		return
	}
	ctx.Status(http.StatusAccepted)
}

// Accept godoc
//
//	@Summary		Accept an invitation
//	@Description	Sets the password (or confirms the current one when the person already has an account) and activates the membership. Sign in afterwards.
//	@Tags			Invitations
//	@Accept			json
//	@Param			body	body	dtos.AcceptInvitationInput	true	"Token, password and SMS code"
//	@Success		204		"No Content"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid invitation, password or code"
//	@Security		ClientId
//	@Router			/api/v1/invitations/accept [post]
func (c *InvitationController) Accept(ctx *gin.Context) {
	client, ok := requestClient(ctx)
	if !ok {
		return
	}
	var req dtos.AcceptInvitationInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	if err := c.invitations.Accept(ctx.Request.Context(), client, req, ctx.ClientIP(), ctx.Request.UserAgent()); err != nil {
		handleAccessError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

type RoleController struct {
	roles *usecases.RoleUsecase
}

func NewRoleController(roles *usecases.RoleUsecase) *RoleController {
	return &RoleController{roles: roles}
}

// Get godoc
//
//	@Summary		Get a role
//	@Description	Requires roles:read. A role of another organization answers 404.
//	@Tags			Roles
//	@Produce		json
//	@Param			id	path		string	true	"Role id"
//	@Success		200	{object}	dtos.RoleView
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission"
//	@Failure		404	{object}	shared.ErrorResponse	"Role not found"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/roles/{id} [get]
func (c *RoleController) Get(ctx *gin.Context) {
	actor, roleID, ok := roleRequest(ctx)
	if !ok {
		return
	}
	resp, err := c.roles.Get(ctx.Request.Context(), actor, roleID)
	respond(ctx, http.StatusOK, resp, err)
}

// Create godoc
//
//	@Summary		Create a custom role
//	@Description	Requires roles:manage. The role belongs to the organization of the session; it can only hold permissions of its scope that you hold yourself.
//	@Tags			Roles
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CreateRoleInput	true	"Role"
//	@Success		201		{object}	dtos.RoleView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or privilege escalation"
//	@Failure		409		{object}	shared.ErrorResponse	"Code already used"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or unknown permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/roles [post]
func (c *RoleController) Create(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	var req dtos.CreateRoleInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.roles.Create(ctx.Request.Context(), actor, req)
	respond(ctx, http.StatusCreated, resp, err)
}

// Update godoc
//
//	@Summary		Change a custom role
//	@Description	Requires roles:manage. New permissions apply to every member with the role on their next request.
//	@Tags			Roles
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Role id"
//	@Param			body	body		dtos.UpdateRoleInput	true	"Fields to change"
//	@Success		200		{object}	dtos.RoleView
//	@Failure		401		{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403		{object}	shared.ErrorResponse	"Missing permission or privilege escalation"
//	@Failure		404		{object}	shared.ErrorResponse	"Role not found"
//	@Failure		409		{object}	shared.ErrorResponse	"System role"
//	@Failure		422		{object}	shared.ErrorResponse	"Invalid fields or unknown permission"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/roles/{id} [patch]
func (c *RoleController) Update(ctx *gin.Context) {
	actor, roleID, ok := roleRequest(ctx)
	if !ok {
		return
	}
	var req dtos.UpdateRoleInput
	if err := binding.JSON(ctx, &req); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	resp, err := c.roles.Update(ctx.Request.Context(), actor, roleID, req)
	respond(ctx, http.StatusOK, resp, err)
}

// Delete godoc
//
//	@Summary		Delete a custom role
//	@Description	Requires roles:manage. Only roles no member holds.
//	@Tags			Roles
//	@Param			id	path	string	true	"Role id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	shared.ErrorResponse	"Unauthorized or session ended"
//	@Failure		403	{object}	shared.ErrorResponse	"Missing permission or privilege escalation"
//	@Failure		404	{object}	shared.ErrorResponse	"Role not found"
//	@Failure		409	{object}	shared.ErrorResponse	"System role or role in use"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/roles/{id} [delete]
func (c *RoleController) Delete(ctx *gin.Context) {
	actor, roleID, ok := roleRequest(ctx)
	if !ok {
		return
	}
	if err := c.roles.Delete(ctx.Request.Context(), actor, roleID); err != nil {
		handleAccessError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func roleRequest(ctx *gin.Context) (usecases.StaffActor, uuid.UUID, bool) {
	actor, ok := staffActor(ctx)
	if !ok {
		return usecases.StaffActor{}, uuid.Nil, false
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "ROLE_NOT_FOUND", "Role not found.")
		return usecases.StaffActor{}, uuid.Nil, false
	}
	return actor, id, true
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleAccessError(ctx, err)
		return
	}
	ctx.JSON(status, body)
}

func memberRequest(ctx *gin.Context) (usecases.StaffActor, uuid.UUID, bool) {
	actor, ok := staffActor(ctx)
	if !ok {
		return usecases.StaffActor{}, uuid.Nil, false
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, "MEMBER_NOT_FOUND", "Member not found.")
		return usecases.StaffActor{}, uuid.Nil, false
	}
	return actor, id, true
}

func organizationIDParam(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("orgId"))
	if err != nil {
		shared.NotFound(ctx, "ORGANIZATION_NOT_FOUND", "Organization not found.")
		return uuid.Nil, false
	}
	return id, true
}
