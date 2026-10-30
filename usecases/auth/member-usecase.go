package usecases

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrMemberNotFound       = errors.New("member not found")
	ErrOrganizationNotFound = errors.New("organization not found")
	ErrRoleNotFound         = errors.New("role not found")
	ErrRoleNotAssignable    = errors.New("role cannot be assigned in this organization")
	ErrRolesRequired        = errors.New("at least one role is required")
	ErrPrivilegeEscalation  = errors.New("the actor does not hold every permission involved")
	ErrLastOwner            = errors.New("the organization must keep at least one active owner")
	ErrInvalidMemberState   = entities.ErrInvalidMemberState
	ErrAlreadyMember        = errors.New("the person is already a member of the organization")
	ErrIdentityInUse        = errors.New("the e-mail or phone belongs to a customer or provider account")
	ErrIdentityConflict     = errors.New("the e-mail and the phone belong to different people")
	ErrNIFAlreadyExists     = repository.ErrNIFAlreadyExists
	ErrMFAResetNotAllowed   = errors.New("the person belongs to other organizations: only AGORA can reset the authenticator")
)

const (
	auditEntityMember         = "membership"
	auditActionMemberInvite   = "member.invite"
	auditActionMemberResend   = "member.invitation-resend"
	auditActionMemberActivate = "member.activate"
	auditActionMemberUpdate   = "member.update"
	auditActionMemberRoles    = "member.roles"
	auditActionMemberSuspend  = "member.suspend"
	auditActionMemberReactive = "member.reactivate"
	auditActionMemberDeactive = "member.deactivate"
	auditActionMemberMFAReset = "member.mfa-reset"
	auditActionPartnerCreate  = "partner.create"
	auditActionAdminBootstrap = "member.bootstrap-super-admin"
	auditEntityOrganization   = "organization"
	invitationMessageTemplate = "AGORA: foi convidado para %s. Active a conta em %s (válido 48 h)."
	invitationLinkTokenQuery  = "?token="
)

type InvitationLinks struct {
	Platform string
	Partner  string
}

func (l InvitationLinks) For(scope enums.OrganizationType, token string) string {
	base := l.Platform
	if scope == enums.OrganizationTypePartner {
		base = l.Partner
	}
	return strings.TrimRight(base, "?") + invitationLinkTokenQuery + token
}

type StaffActor struct {
	Access *entities.Access
	Audit  entities.Actor
}

type MemberUsecase struct {
	userRepo       repository.IUserRepository
	orgRepo        repository.IOrganizationRepository
	roleRepo       repository.IRoleRepository
	membershipRepo repository.IMembershipRepository
	invitationRepo repository.IInvitationRepository
	mfaRepo        repository.IUserMFARepository
	auditRepo      repository.IAuditLogRepository
	outboxRepo     repository.IOutboxRepository
	txManager      repository.ITransactionManager
	opaqueTokens   services.IOpaqueTokenGenerator
	sms            services.ISMSSender
	revoker        *SessionRevoker
	access         *AccessResolver
	links          InvitationLinks
}

type MemberStores struct {
	Users       repository.IUserRepository
	Orgs        repository.IOrganizationRepository
	Roles       repository.IRoleRepository
	Memberships repository.IMembershipRepository
	Invitations repository.IInvitationRepository
	MFA         repository.IUserMFARepository
	Audit       repository.IAuditLogRepository
	Outbox      repository.IOutboxRepository
	Tx          repository.ITransactionManager
}

func NewMemberUsecase(
	stores MemberStores,
	opaqueTokens services.IOpaqueTokenGenerator,
	sms services.ISMSSender,
	revoker *SessionRevoker,
	access *AccessResolver,
	links InvitationLinks,
) *MemberUsecase {
	return &MemberUsecase{
		userRepo:       stores.Users,
		orgRepo:        stores.Orgs,
		roleRepo:       stores.Roles,
		membershipRepo: stores.Memberships,
		invitationRepo: stores.Invitations,
		mfaRepo:        stores.MFA,
		auditRepo:      stores.Audit,
		outboxRepo:     stores.Outbox,
		txManager:      stores.Tx,
		opaqueTokens:   opaqueTokens,
		sms:            sms,
		revoker:        revoker,
		access:         access,
		links:          links,
	}
}

func (uc *MemberUsecase) List(ctx context.Context, actor StaffActor, query dtos.MemberListQuery) (*dtos.MemberPage, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	return uc.list(ctx, org, query)
}

func (uc *MemberUsecase) ListPartnerMembers(ctx context.Context, organizationID uuid.UUID, query dtos.MemberListQuery) (*dtos.MemberPage, error) {
	org, err := uc.partnerOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	return uc.list(ctx, org, query)
}

func (uc *MemberUsecase) Get(ctx context.Context, actor StaffActor, memberID uuid.UUID) (*dtos.MemberView, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}
	return uc.memberView(ctx, org, memberID, roles)
}

func (uc *MemberUsecase) Invite(ctx context.Context, actor StaffActor, req dtos.InviteMemberInput) (*dtos.MemberView, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	return uc.invite(ctx, actor, org, req, true)
}

func (uc *MemberUsecase) InviteToPartner(ctx context.Context, actor StaffActor, organizationID uuid.UUID, req dtos.InviteMemberInput) (*dtos.MemberView, error) {
	org, err := uc.partnerOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	return uc.invite(ctx, actor, org, req, false)
}

func (uc *MemberUsecase) ResendInvitation(ctx context.Context, actor StaffActor, memberID uuid.UUID) error {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return err
	}

	var user *entities.User
	var token string
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		target, err := uc.lockMember(ctx, org, memberID)
		if err != nil {
			return err
		}
		if err := uc.ensureCanManage(actor, target, roles); err != nil {
			return err
		}
		if target.Status != enums.MembershipStatusInvited {
			return ErrInvalidMemberState
		}
		if user, err = uc.findUser(ctx, target.UserID); err != nil {
			return err
		}
		if err := uc.invitationRepo.RevokePending(ctx, target.ID, time.Now().UTC()); err != nil {
			return err
		}
		if token, err = uc.createInvitation(ctx, target, actor.Audit.ID); err != nil {
			return err
		}
		return uc.audit(ctx, actor.Audit, org, auditActionMemberResend, target.ID, nil, nil)
	})
	if err != nil {
		return err
	}
	uc.sendInvitation(ctx, org, user, token)
	return nil
}

func (uc *MemberUsecase) Update(ctx context.Context, actor StaffActor, memberID uuid.UUID, req dtos.UpdateMemberInput) (*dtos.MemberView, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		target, err := uc.lockMember(ctx, org, memberID)
		if err != nil {
			return err
		}
		if target.IsDeactivated() {
			return ErrInvalidMemberState
		}
		if target.UserID != actor.Access.UserID {
			if err := uc.ensureCanManage(actor, target, roles); err != nil {
				return err
			}
		}
		user, err := uc.findUser(ctx, target.UserID)
		if err != nil {
			return err
		}
		before := dtos.ToUserView(user)
		name, phoneNumber := user.Name, user.Phone
		if req.Name != nil {
			name = *req.Name
		}
		if req.Phone != nil {
			phoneNumber = phone.Normalize(*req.Phone)
			if phoneNumber != user.Phone {
				if err := ensureIdentityIsFree(ctx, uc.userRepo, "", phoneNumber); err != nil {
					return err
				}
			}
		}
		if err := user.UpdateProfile(name, user.Email, phoneNumber); err != nil {
			return err
		}
		if err := uc.userRepo.Update(ctx, user); err != nil {
			return err
		}
		return uc.audit(ctx, actor.Audit, org, auditActionMemberUpdate, target.ID, before, dtos.ToUserView(user))
	})
	if err != nil {
		return nil, err
	}
	return uc.memberView(ctx, org, memberID, roles)
}

func (uc *MemberUsecase) ReplaceRoles(ctx context.Context, actor StaffActor, memberID uuid.UUID, req dtos.ReplaceRolesInput) (*dtos.MemberView, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}
	requested, err := uc.assignableRoles(ctx, org, req.RoleIDs)
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.orgRepo.Lock(ctx, org.ID); err != nil {
			return err
		}
		target, err := uc.lockMember(ctx, org, memberID)
		if err != nil {
			return err
		}
		if target.UserID == actor.Access.UserID {
			return ErrCannotManageSelf
		}
		if target.IsDeactivated() {
			return ErrInvalidMemberState
		}
		if err := uc.ensureKeepsOwner(ctx, org, target, roles, rolesIDs(requested)); err != nil {
			return err
		}
		if err := uc.ensureCanManage(actor, target, roles); err != nil {
			return err
		}
		if !actor.Access.HasAll(entities.PermissionsOf(requested)) {
			return ErrPrivilegeEscalation
		}

		before := roleCodes(target.RoleIDs, roles)
		if err := target.ReplaceRoles(rolesIDs(requested)); err != nil {
			return err
		}
		if err := uc.membershipRepo.ReplaceRoles(ctx, target); err != nil {
			return err
		}
		after := roleCodes(target.RoleIDs, roles)
		if err := uc.publish(ctx, events.MemberRolesChangedV1{MemberChange: memberChange(target, actor), Before: before, After: after}); err != nil {
			return err
		}
		return uc.audit(ctx, actor.Audit, org, auditActionMemberRoles, target.ID, before, after)
	})
	if err != nil {
		return nil, err
	}
	uc.access.Invalidate(ctx, memberID)
	return uc.memberView(ctx, org, memberID, roles)
}

type statusChange struct {
	action     string
	transition func(*entities.Membership) error
	endsAccess bool
	event      func(events.MemberChange) events.Event
}

var (
	suspension = statusChange{auditActionMemberSuspend, (*entities.Membership).Suspend, true,
		func(c events.MemberChange) events.Event { return events.MemberSuspendedV1{MemberChange: c} }}
	reactivation = statusChange{auditActionMemberReactive, (*entities.Membership).Reactivate, false,
		func(c events.MemberChange) events.Event { return events.MemberReactivatedV1{MemberChange: c} }}
	deactivation = statusChange{auditActionMemberDeactive, (*entities.Membership).Deactivate, true,
		func(c events.MemberChange) events.Event { return events.MemberDeactivatedV1{MemberChange: c} }}
)

func (uc *MemberUsecase) Suspend(ctx context.Context, actor StaffActor, memberID uuid.UUID) (*dtos.MemberView, error) {
	return uc.changeStatus(ctx, actor, memberID, suspension)
}

func (uc *MemberUsecase) Reactivate(ctx context.Context, actor StaffActor, memberID uuid.UUID) (*dtos.MemberView, error) {
	return uc.changeStatus(ctx, actor, memberID, reactivation)
}

func (uc *MemberUsecase) Deactivate(ctx context.Context, actor StaffActor, memberID uuid.UUID) (*dtos.MemberView, error) {
	return uc.changeStatus(ctx, actor, memberID, deactivation)
}

func (uc *MemberUsecase) ResetMFA(ctx context.Context, actor StaffActor, memberID uuid.UUID) error {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return err
	}

	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		target, err := uc.lockMember(ctx, org, memberID)
		if err != nil {
			return err
		}
		if target.UserID == actor.Access.UserID {
			return ErrCannotManageSelf
		}
		if target.IsDeactivated() {
			return ErrInvalidMemberState
		}
		if err := uc.ensureCanManage(actor, target, roles); err != nil {
			return err
		}
		if actor.Access.Scope != enums.OrganizationTypePlatform {
			outside, err := uc.membershipRepo.HasMembershipOutside(ctx, target.UserID, org.ID)
			if err != nil {
				return err
			}
			if outside {
				return ErrMFAResetNotAllowed
			}
		}
		if err := uc.mfaRepo.Delete(ctx, target.UserID); err != nil {
			return err
		}
		if err := uc.revoker.RevokeUser(ctx, target.UserID); err != nil {
			return err
		}
		if err := uc.publish(ctx, events.MemberMFAResetV1{MemberChange: memberChange(target, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor.Audit, org, auditActionMemberMFAReset, target.ID, nil, nil)
	})
}

func (uc *MemberUsecase) changeStatus(ctx context.Context, actor StaffActor, memberID uuid.UUID, change statusChange) (*dtos.MemberView, error) {
	org, err := uc.organization(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.orgRepo.Lock(ctx, org.ID); err != nil {
			return err
		}
		target, err := uc.lockMember(ctx, org, memberID)
		if err != nil {
			return err
		}
		if target.UserID == actor.Access.UserID {
			return ErrCannotManageSelf
		}
		before := target.Status
		if change.endsAccess && target.IsActive() {
			if err := uc.ensureKeepsOwner(ctx, org, target, roles, nil); err != nil {
				return err
			}
		}
		if err := uc.ensureCanManage(actor, target, roles); err != nil {
			return err
		}
		if err := change.transition(target); err != nil {
			return err
		}
		if err := uc.membershipRepo.Update(ctx, target); err != nil {
			return err
		}
		if change.endsAccess {
			if err := uc.invitationRepo.RevokePending(ctx, target.ID, time.Now().UTC()); err != nil {
				return err
			}
			if err := uc.revoker.RevokeUser(ctx, target.UserID); err != nil {
				return err
			}
		}
		if err := uc.publish(ctx, change.event(memberChange(target, actor))); err != nil {
			return err
		}
		return uc.audit(ctx, actor.Audit, org, change.action, target.ID, before, target.Status)
	})
	if err != nil {
		return nil, err
	}
	uc.access.Invalidate(ctx, memberID)
	return uc.memberView(ctx, org, memberID, roles)
}

func (uc *MemberUsecase) invite(ctx context.Context, actor StaffActor, org *entities.Organization, req dtos.InviteMemberInput, checkEscalation bool) (*dtos.MemberView, error) {
	requested, err := uc.assignableRoles(ctx, org, req.RoleIDs)
	if err != nil {
		return nil, err
	}
	if checkEscalation && !actor.Access.HasAll(entities.PermissionsOf(requested)) {
		return nil, ErrPrivilegeEscalation
	}
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}
	email := normalizeEmail(req.Email)
	phoneNumber := phone.Normalize(req.Phone)

	var user *entities.User
	var membership *entities.Membership
	var token string
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if user, err = uc.staffIdentity(ctx, req.Name, email, phoneNumber); err != nil {
			return err
		}
		existing, err := uc.membershipRepo.FindByUserAndOrganization(ctx, user.ID, org.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.IsDeactivated() {
				return ErrInvalidMemberState
			}
			return ErrAlreadyMember
		}
		if membership, err = entities.NewInvitedMembership(user.ID, org.ID, actor.Audit.ID, rolesIDs(requested)); err != nil {
			return err
		}
		if err := uc.membershipRepo.Create(ctx, membership); err != nil {
			return err
		}
		if token, err = uc.createInvitation(ctx, membership, actor.Audit.ID); err != nil {
			return err
		}
		codes := roleCodes(membership.RoleIDs, rolesByID(requested))
		if err := uc.publish(ctx, events.MemberInvitedV1{MemberChange: memberChange(membership, actor), Roles: codes}); err != nil {
			return err
		}
		after := map[string]any{"user": dtos.ToUserView(user), "roles": codes, "status": membership.Status}
		return uc.audit(ctx, actor.Audit, org, auditActionMemberInvite, membership.ID, nil, after)
	})
	if err != nil {
		return nil, err
	}
	uc.sendInvitation(ctx, org, user, token)
	return uc.memberView(ctx, org, membership.ID, roles)
}

func (uc *MemberUsecase) staffIdentity(ctx context.Context, name, email, phoneNumber string) (*entities.User, error) {
	byEmail, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	byPhone, err := uc.userRepo.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if byEmail != nil && byPhone != nil && byEmail.ID != byPhone.ID {
		return nil, ErrIdentityConflict
	}
	existing := byEmail
	if existing == nil {
		existing = byPhone
	}
	if existing != nil {
		if !existing.IsStaff() {
			return nil, ErrIdentityInUse
		}
		return existing, nil
	}

	user, err := entities.NewInvitedStaffUser(name, email, phoneNumber)
	if err != nil {
		return nil, err
	}
	if err := uc.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (uc *MemberUsecase) createInvitation(ctx context.Context, membership *entities.Membership, createdBy *uuid.UUID) (string, error) {
	token, hash, err := uc.opaqueTokens.Generate()
	if err != nil {
		return "", err
	}
	invitation, err := entities.NewInvitation(membership.ID, hash, createdBy)
	if err != nil {
		return "", err
	}
	if err := uc.invitationRepo.Create(ctx, invitation); err != nil {
		return "", err
	}
	return token, nil
}

func (uc *MemberUsecase) sendInvitation(ctx context.Context, org *entities.Organization, user *entities.User, token string) {
	if user.Phone == "" {
		logger.FromContext(ctx).Error("invitation not sent: the member has no phone")
		return
	}
	message := fmt.Sprintf(invitationMessageTemplate, org.Name, uc.links.For(org.Type, token))
	if err := uc.sms.Send(ctx, user.Phone, message); err != nil {
		logger.FromContext(ctx).Error("invitation not sent: sms provider failed", zap.Error(err))
	}
}

func (uc *MemberUsecase) list(ctx context.Context, org *entities.Organization, query dtos.MemberListQuery) (*dtos.MemberPage, error) {
	roles, err := uc.rolesOf(ctx, org)
	if err != nil {
		return nil, err
	}
	params := pagination.Params{Page: query.Page, Size: query.Size}
	members, total, err := uc.membershipRepo.ListMembers(ctx, org.ID, repository.MemberFilter{
		Status: query.Status, RoleID: query.RoleID, Search: query.Search,
	}, params)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.MemberView, 0, len(members))
	for _, m := range members {
		views = append(views, toMemberView(m, roles))
	}
	page := pagination.NewPage(views, params, total)
	return &dtos.MemberPage{Items: page.Items, Page: page.Page, Size: page.Size, Total: page.Total, TotalPages: page.TotalPages}, nil
}

func (uc *MemberUsecase) memberView(ctx context.Context, org *entities.Organization, memberID uuid.UUID, roles map[uuid.UUID]*entities.Role) (*dtos.MemberView, error) {
	member, err := uc.membershipRepo.FindMember(ctx, org.ID, memberID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, ErrMemberNotFound
	}
	view := toMemberView(member, roles)
	return &view, nil
}

func (uc *MemberUsecase) lockMember(ctx context.Context, org *entities.Organization, memberID uuid.UUID) (*entities.Membership, error) {
	target, err := uc.membershipRepo.LockInOrganization(ctx, org.ID, memberID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrMemberNotFound
	}
	return target, nil
}

func (uc *MemberUsecase) ensureCanManage(actor StaffActor, target *entities.Membership, roles map[uuid.UUID]*entities.Role) error {
	if actor.Access.OrganizationID != target.OrganizationID {
		return nil
	}
	if !actor.Access.HasAll(entities.PermissionsOf(rolesByIDs(target.RoleIDs, roles))) {
		return ErrPrivilegeEscalation
	}
	return nil
}

func (uc *MemberUsecase) ensureKeepsOwner(ctx context.Context, org *entities.Organization, target *entities.Membership, roles map[uuid.UUID]*entities.Role, nextRoleIDs []uuid.UUID) error {
	if !target.IsActive() {
		return nil
	}
	for _, roleID := range target.RoleIDs {
		role, ok := roles[roleID]
		if !ok || !role.IsOwnerRole() || slices.Contains(nextRoleIDs, roleID) {
			continue
		}
		others, err := uc.membershipRepo.CountOtherActiveWithRole(ctx, org.ID, roleID, target.ID)
		if err != nil {
			return err
		}
		if others == 0 {
			return ErrLastOwner
		}
	}
	return nil
}

func (uc *MemberUsecase) assignableRoles(ctx context.Context, org *entities.Organization, roleIDs []uuid.UUID) ([]*entities.Role, error) {
	ids := distinctIDs(roleIDs)
	if len(ids) == 0 {
		return nil, ErrRolesRequired
	}
	found, err := uc.roleRepo.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	visible := make([]*entities.Role, 0, len(found))
	for _, role := range found {
		if role.VisibleTo(org) {
			visible = append(visible, role)
		}
	}
	if len(visible) != len(ids) {
		return nil, ErrRoleNotFound
	}
	for _, role := range visible {
		if role.Scope != org.Type {
			return nil, ErrRoleNotAssignable
		}
	}
	return visible, nil
}

func (uc *MemberUsecase) rolesOf(ctx context.Context, org *entities.Organization) (map[uuid.UUID]*entities.Role, error) {
	list, err := uc.roleRepo.ListForOrganization(ctx, org)
	if err != nil {
		return nil, err
	}
	roles := make(map[uuid.UUID]*entities.Role, len(list))
	for _, r := range list {
		roles[r.ID] = r
	}
	return roles, nil
}

func (uc *MemberUsecase) organization(ctx context.Context, id uuid.UUID) (*entities.Organization, error) {
	org, err := uc.orgRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrOrganizationNotFound
	}
	return org, nil
}

func (uc *MemberUsecase) partnerOrganization(ctx context.Context, id uuid.UUID) (*entities.Organization, error) {
	org, err := uc.organization(ctx, id)
	if err != nil {
		return nil, err
	}
	if !org.IsPartner() {
		return nil, ErrOrganizationNotFound
	}
	return org, nil
}

func (uc *MemberUsecase) findUser(ctx context.Context, id uuid.UUID) (*entities.User, error) {
	user, err := uc.userRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrMemberNotFound
	}
	return user, nil
}

func (uc *MemberUsecase) audit(ctx context.Context, actor entities.Actor, org *entities.Organization, action string, membershipID uuid.UUID, before, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, auditEntityMember, membershipID.String(), before, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.auditRepo.Record(ctx, entry.InOrganization(org.ID))
}

func (uc *MemberUsecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.outboxRepo.Save(ctx, envelope)
}

func memberChange(m *entities.Membership, actor StaffActor) events.MemberChange {
	return events.NewMemberChange(m.ID, m.OrganizationID, m.UserID, actor.Audit.ID, m.PermVersion)
}

func rolesByID(roles []*entities.Role) map[uuid.UUID]*entities.Role {
	out := make(map[uuid.UUID]*entities.Role, len(roles))
	for _, r := range roles {
		out[r.ID] = r
	}
	return out
}

func toMemberView(m *entities.Member, roles map[uuid.UUID]*entities.Role) dtos.MemberView {
	summaries := make([]dtos.RoleSummary, 0, len(m.RoleIDs))
	for _, role := range rolesByIDs(m.RoleIDs, roles) {
		summaries = append(summaries, dtos.RoleSummary{ID: role.ID, Code: role.Code, Name: role.Name})
	}
	return dtos.MemberView{
		ID:          m.ID,
		UserID:      m.UserID,
		Name:        m.Name,
		Email:       m.Email,
		Phone:       m.Phone,
		Status:      m.Status,
		Roles:       summaries,
		ActivatedAt: m.ActivatedAt,
		LastLoginAt: m.LastLoginAt,
		CreatedAt:   m.CreatedAt,
	}
}

func rolesByIDs(ids []uuid.UUID, roles map[uuid.UUID]*entities.Role) []*entities.Role {
	out := make([]*entities.Role, 0, len(ids))
	for _, id := range ids {
		if role, ok := roles[id]; ok {
			out = append(out, role)
		}
	}
	return out
}

func rolesIDs(roles []*entities.Role) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return ids
}

func roleCodes(ids []uuid.UUID, roles map[uuid.UUID]*entities.Role) []string {
	codes := make([]string, 0, len(ids))
	for _, role := range rolesByIDs(ids, roles) {
		codes = append(codes, role.Code)
	}
	slices.Sort(codes)
	return codes
}
