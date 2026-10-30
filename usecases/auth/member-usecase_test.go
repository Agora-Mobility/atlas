package usecases_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type rbac struct {
	platform   *entities.Organization
	partner    *entities.Organization
	superAdmin *entities.Role
	admin      *entities.Role
	operations *entities.Role
	owner      *entities.Role
	manager    *entities.Role
	operator   *entities.Role
	finance    *entities.Role
}

func systemRole(scope enums.OrganizationType, code string, perms ...entities.Permission) *entities.Role {
	return &entities.Role{ID: uuid.New(), Scope: scope, Code: code, Name: code, IsSystem: true, Permissions: perms}
}

func newRBAC() rbac {
	p, x := enums.OrganizationTypePlatform, enums.OrganizationTypePartner
	return rbac{
		platform: &entities.Organization{ID: uuid.New(), Type: p, Name: "AGORA"},
		partner:  &entities.Organization{ID: uuid.New(), Type: x, Name: "Parceiro A"},
		superAdmin: systemRole(p, entities.RoleCodeSuperAdmin, entities.PermMembersRead, entities.PermMembersInvite,
			entities.PermMembersSuspend, entities.PermRolesAssign, entities.PermRolesManage, entities.PermRefundsApprove),
		admin:      systemRole(p, "administrator", entities.PermMembersRead, entities.PermMembersInvite, entities.PermMembersSuspend, entities.PermRolesAssign),
		operations: systemRole(p, "operations", entities.PermMembersRead),
		owner: systemRole(x, entities.RoleCodeOwner, entities.PermMembersRead, entities.PermMembersInvite, entities.PermMembersSuspend,
			entities.PermRolesAssign, entities.PermPartnerProvidersManage, entities.PermPartnerFinanceRead),
		manager:  systemRole(x, "manager", entities.PermMembersRead, entities.PermMembersInvite, entities.PermRolesAssign, entities.PermPartnerProvidersManage),
		operator: systemRole(x, "operator", entities.PermPartnerProvidersManage),
		finance:  systemRole(x, "finance", entities.PermPartnerFinanceRead),
	}
}

func (r rbac) rolesOf(org *entities.Organization) []*entities.Role {
	if org.Type == enums.OrganizationTypePartner {
		return []*entities.Role{r.owner, r.manager, r.operator, r.finance}
	}
	return []*entities.Role{r.superAdmin, r.admin, r.operations}
}

func actorWith(org *entities.Organization, roles ...*entities.Role) usecases.StaffActor {
	userID := uuid.New()
	access := &entities.Access{
		MembershipID: uuid.New(), UserID: userID, OrganizationID: org.ID, Scope: org.Type,
		Status: enums.MembershipStatusActive, Permissions: entities.PermissionsOf(roles),
	}
	orgID := org.ID
	return usecases.StaffActor{Access: access, Audit: entities.Actor{ID: &userID, Role: enums.RoleStaff, OrganizationID: &orgID}}
}

func memberOf(org *entities.Organization, status enums.MembershipStatus, roles ...*entities.Role) *entities.Membership {
	ids := make([]uuid.UUID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return &entities.Membership{ID: uuid.New(), UserID: uuid.New(), OrganizationID: org.ID, Status: status, PermVersion: 1, RoleIDs: ids}
}

func (f fixture) withOrganization(r rbac, org *entities.Organization) {
	f.orgs.EXPECT().FindByID(gomock.Any(), org.ID).Return(org, nil).AnyTimes()
	f.roles.EXPECT().ListForOrganization(gomock.Any(), org).Return(r.rolesOf(org), nil).AnyTimes()
}

func (f fixture) expectLocked(org *entities.Organization, target *entities.Membership) {
	f.orgs.EXPECT().Lock(gomock.Any(), org.ID).Return(nil).AnyTimes()
	f.memberships.EXPECT().LockInOrganization(gomock.Any(), org.ID, target.ID).Return(target, nil)
}

func (f fixture) expectMemberView(org *entities.Organization, target *entities.Membership) {
	f.memberships.EXPECT().FindMember(gomock.Any(), org.ID, target.ID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) (*entities.Member, error) {
			return &entities.Member{Membership: *target, Name: "Ana"}, nil
		})
}

func (f fixture) expectOutbox(t *testing.T, eventType string) {
	t.Helper()
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, eventType, env.EventType)
			return nil
		})
}

func (f fixture) expectMemberAudit(t *testing.T, org *entities.Organization, action string) {
	t.Helper()
	f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
			assert.Equal(t, action, e.Action)
			assert.Equal(t, org.ID, *e.OrganizationID)
			return nil
		})
}

func TestReplaceRoles_PrivilegeEscalation(t *testing.T) {
	r := newRBAC()

	t.Run("CA01.1 a manager cannot grant the finance role", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusActive, r.operator)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.finance.ID}).Return([]*entities.Role{r.finance}, nil)
		f.expectLocked(r.partner, target)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.manager), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.finance.ID}})

		require.ErrorIs(t, err, usecases.ErrPrivilegeEscalation)
		assert.Equal(t, []uuid.UUID{r.operator.ID}, target.RoleIDs, "roles unchanged")
	})

	t.Run("CA01.3 an administrator cannot grant super admin", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.platform)
		target := memberOf(r.platform, enums.MembershipStatusActive, r.operations)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.superAdmin.ID}).Return([]*entities.Role{r.superAdmin}, nil)
		f.expectLocked(r.platform, target)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.platform, r.admin), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.superAdmin.ID}})

		require.ErrorIs(t, err, usecases.ErrPrivilegeEscalation)
	})

	t.Run("nobody manages a member with more permissions", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.platform)
		target := memberOf(r.platform, enums.MembershipStatusActive, r.superAdmin)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.operations.ID}).Return([]*entities.Role{r.operations}, nil)
		f.expectLocked(r.platform, target)
		f.memberships.EXPECT().CountOtherActiveWithRole(gomock.Any(), r.platform.ID, r.superAdmin.ID, target.ID).Return(int64(1), nil)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.platform, r.admin), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.operations.ID}})

		require.ErrorIs(t, err, usecases.ErrPrivilegeEscalation)
	})
}

func TestReplaceRoles_ReplacesBumpsVersionInvalidatesCacheAndAudits(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.withOrganization(r, r.partner)
	target := memberOf(r.partner, enums.MembershipStatusActive, r.operator)
	f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.manager.ID}).Return([]*entities.Role{r.manager}, nil)
	f.expectLocked(r.partner, target)
	f.memberships.EXPECT().ReplaceRoles(gomock.Any(), target).Return(nil)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, events.MemberRolesChanged, env.EventType)
			var payload events.MemberRolesChangedV1
			require.NoError(t, env.DecodePayload(&payload))
			assert.Equal(t, []string{"operator"}, payload.Before)
			assert.Equal(t, []string{"manager"}, payload.After)
			assert.Equal(t, int64(2), payload.PermVersion)
			return nil
		})
	f.expectMemberAudit(t, r.partner, "member.roles")
	f.accessCache.EXPECT().Delete(gomock.Any(), target.ID).Return(nil)
	f.expectMemberView(r.partner, target)

	out, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.manager.ID}})

	require.NoError(t, err)
	assert.Equal(t, int64(2), target.PermVersion, "CA07.2 perm_version goes up exactly once")
	require.Len(t, out.Roles, 1)
	assert.Equal(t, "manager", out.Roles[0].Code)
}

func TestReplaceRoles_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("CA02.1 a role of another scope", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.operations.ID}).Return([]*entities.Role{r.operations}, nil)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), uuid.New(), dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.operations.ID}})

		require.ErrorIs(t, err, usecases.ErrRoleNotAssignable)
	})

	t.Run("CA02.2 a custom role of another organization is not revealed", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		otherOrg := uuid.New()
		custom := &entities.Role{ID: uuid.New(), OrganizationID: &otherOrg, Scope: enums.OrganizationTypePartner, Code: "custom"}
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{custom.ID}).Return([]*entities.Role{custom}, nil)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), uuid.New(), dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{custom.ID}})

		require.ErrorIs(t, err, usecases.ErrRoleNotFound)
	})

	t.Run("CA03.1 own roles", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		actor := actorWith(r.partner, r.owner)
		self := memberOf(r.partner, enums.MembershipStatusActive, r.owner)
		self.UserID = actor.Access.UserID
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.manager}, nil)
		f.expectLocked(r.partner, self)

		_, err := f.members.ReplaceRoles(context.Background(), actor, self.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.manager.ID}})

		require.ErrorIs(t, err, usecases.ErrCannotManageSelf)
	})

	t.Run("CA04.2 the last active owner keeps the role", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusActive, r.owner)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.manager}, nil)
		f.expectLocked(r.partner, target)
		f.memberships.EXPECT().CountOtherActiveWithRole(gomock.Any(), r.partner.ID, r.owner.ID, target.ID).Return(int64(0), nil)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.manager.ID}})

		require.ErrorIs(t, err, usecases.ErrLastOwner)
	})

	t.Run("CA17.1 a deactivated member", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusDeactivated, r.operator)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.manager}, nil)
		f.expectLocked(r.partner, target)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), target.ID, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.manager.ID}})

		require.ErrorIs(t, err, usecases.ErrInvalidMemberState)
	})

	t.Run("CA14.1 a member of another organization answers not found", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.manager}, nil)
		f.orgs.EXPECT().Lock(gomock.Any(), r.partner.ID).Return(nil)
		foreign := uuid.New()
		f.memberships.EXPECT().LockInOrganization(gomock.Any(), r.partner.ID, foreign).Return(nil, nil)

		_, err := f.members.ReplaceRoles(context.Background(), actorWith(r.partner, r.owner), foreign, dtos.ReplaceRolesInput{RoleIDs: []uuid.UUID{r.manager.ID}})

		require.ErrorIs(t, err, usecases.ErrMemberNotFound)
	})
}

func TestSuspend_EndsSessionsAtOnce(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.withOrganization(r, r.partner)
	target := memberOf(r.partner, enums.MembershipStatusActive, r.owner)
	f.expectLocked(r.partner, target)
	f.memberships.EXPECT().CountOtherActiveWithRole(gomock.Any(), r.partner.ID, r.owner.ID, target.ID).Return(int64(1), nil)
	f.memberships.EXPECT().Update(gomock.Any(), target).Return(nil)
	f.invitations.EXPECT().RevokePending(gomock.Any(), target.ID, gomock.Any()).Return(nil)
	f.expectAllSessionsRevoked(t, target.UserID)
	f.expectOutbox(t, events.MemberSuspended)
	f.expectMemberAudit(t, r.partner, "member.suspend")
	f.accessCache.EXPECT().Delete(gomock.Any(), target.ID).Return(nil)
	f.expectMemberView(r.partner, target)

	out, err := f.members.Suspend(context.Background(), actorWith(r.partner, r.owner), target.ID)

	require.NoError(t, err, "CA04.2 one of two owners can suspend the other")
	assert.Equal(t, enums.MembershipStatusSuspended, out.Status)
	assert.Equal(t, int64(2), target.PermVersion)
}

func TestStatusChanges_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("CA04.1 the last super admin cannot be suspended", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.platform)
		target := memberOf(r.platform, enums.MembershipStatusActive, r.superAdmin)
		f.expectLocked(r.platform, target)
		f.memberships.EXPECT().CountOtherActiveWithRole(gomock.Any(), r.platform.ID, r.superAdmin.ID, target.ID).Return(int64(0), nil)

		_, err := f.members.Deactivate(context.Background(), actorWith(r.platform, r.admin), target.ID)

		require.ErrorIs(t, err, usecases.ErrLastOwner)
	})

	t.Run("CA03.2 nobody suspends themselves", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.platform)
		actor := actorWith(r.platform, r.superAdmin)
		self := memberOf(r.platform, enums.MembershipStatusActive, r.superAdmin)
		self.UserID = actor.Access.UserID
		f.expectLocked(r.platform, self)

		_, err := f.members.Suspend(context.Background(), actor, self.ID)

		require.ErrorIs(t, err, usecases.ErrCannotManageSelf)
	})

	t.Run("reactivating an active member is an invalid transition", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusActive, r.operator)
		f.expectLocked(r.partner, target)

		_, err := f.members.Reactivate(context.Background(), actorWith(r.partner, r.owner), target.ID)

		require.ErrorIs(t, err, usecases.ErrInvalidMemberState)
	})

	t.Run("CA17.1 a deactivated member cannot be reactivated", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusDeactivated, r.operator)
		f.expectLocked(r.partner, target)

		_, err := f.members.Reactivate(context.Background(), actorWith(r.partner, r.owner), target.ID)

		require.ErrorIs(t, err, usecases.ErrInvalidMemberState)
	})
}

func (f fixture) expectNoIdentity(email, phoneNumber string) {
	f.users.EXPECT().FindByEmail(gomock.Any(), email).Return(nil, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phoneNumber).Return(nil, nil)
}

func inviteInput(roles ...*entities.Role) dtos.InviteMemberInput {
	ids := make([]uuid.UUID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return dtos.InviteMemberInput{Name: "Operador", Email: "Op@Agora.ao", Phone: "923000010", RoleIDs: ids}
}

func TestInvite_CreatesInvitedMemberAndSendsTheLinkBySMSAndEmail(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.withOrganization(r, r.partner)
	f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.operator.ID}).Return([]*entities.Role{r.operator}, nil)
	f.expectNoIdentity("op@agora.ao", "+244923000010")
	var user *entities.User
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u *entities.User) error { user = u; return nil })
	var membership *entities.Membership
	f.memberships.EXPECT().FindByUserAndOrganization(gomock.Any(), gomock.Any(), r.partner.ID).Return(nil, nil)
	f.memberships.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *entities.Membership) error { membership = m; return nil })
	f.opaqueTokens.EXPECT().Generate().Return("raw-invite", "hash-invite", nil)
	f.invitations.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, i *entities.Invitation) error {
			assert.Equal(t, "hash-invite", i.TokenHash, "CA08.5 only the hash is stored")
			assert.WithinDuration(t, time.Now().Add(48*time.Hour), i.ExpiresAt, time.Minute)
			return nil
		})
	f.expectOutbox(t, events.MemberInvited)
	f.expectMemberAudit(t, r.partner, "member.invite")
	f.sms.EXPECT().Send(gomock.Any(), "+244923000010", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, message string) error {
			assert.Contains(t, message, "https://athena.agora.ao/convite?token=raw-invite")
			assert.Contains(t, message, "Parceiro A")
			return nil
		})
	f.email.EXPECT().Send(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, email services.Email) error {
			assert.Equal(t, "op@agora.ao", email.To)
			assert.Equal(t, "Convite para Parceiro A na AGORA", email.Subject)
			assert.Contains(t, email.HTML, `href="https://athena.agora.ao/convite?token=raw-invite"`)
			assert.Contains(t, email.Text, "https://athena.agora.ao/convite?token=raw-invite")
			assert.True(t, strings.HasPrefix(email.IdempotencyKey, "invitation/"))
			return nil
		})
	f.memberships.EXPECT().FindMember(gomock.Any(), r.partner.ID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (*entities.Member, error) {
			return &entities.Member{Membership: *membership, Name: user.Name}, nil
		})

	out, err := f.members.Invite(context.Background(), actorWith(r.partner, r.manager), inviteInput(r.operator))

	require.NoError(t, err, "CA01.2 a manager can invite an operator")
	assert.Equal(t, enums.MembershipStatusInvited, out.Status)
	assert.Equal(t, enums.RoleStaff, user.Role)
	assert.False(t, user.HasPassword())
	assert.Equal(t, r.partner.ID, membership.OrganizationID, "CAE.2 always the organization of the actor")
}

func TestInvite_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("privilege escalation", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.finance}, nil)

		_, err := f.members.Invite(context.Background(), actorWith(r.partner, r.manager), inviteInput(r.finance))

		require.ErrorIs(t, err, usecases.ErrPrivilegeEscalation)
	})

	t.Run("customer account", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.operator}, nil)
		f.users.EXPECT().FindByEmail(gomock.Any(), "op@agora.ao").Return(nil, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), "+244923000010").Return(newUser(t, enums.RoleCustomer, true), nil)

		_, err := f.members.Invite(context.Background(), actorWith(r.partner, r.owner), inviteInput(r.operator))

		require.ErrorIs(t, err, usecases.ErrIdentityInUse)
	})

	t.Run("already a member", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		staff := newUser(t, enums.RoleStaff, true)
		f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.operator}, nil)
		f.users.EXPECT().FindByEmail(gomock.Any(), "op@agora.ao").Return(staff, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), "+244923000010").Return(nil, nil)
		f.memberships.EXPECT().FindByUserAndOrganization(gomock.Any(), staff.ID, r.partner.ID).
			Return(memberOf(r.partner, enums.MembershipStatusActive), nil)

		_, err := f.members.Invite(context.Background(), actorWith(r.partner, r.owner), inviteInput(r.operator))

		require.ErrorIs(t, err, usecases.ErrAlreadyMember)
	})
}

func TestInvite_CA12_2_MemberOfAnotherOrganizationIsInvitedLikeANewPerson(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.withOrganization(r, r.partner)
	staff := newUser(t, enums.RoleStaff, true)
	f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.operator}, nil)
	f.users.EXPECT().FindByEmail(gomock.Any(), "op@agora.ao").Return(staff, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), "+244923000010").Return(nil, nil)
	f.memberships.EXPECT().FindByUserAndOrganization(gomock.Any(), staff.ID, r.partner.ID).Return(nil, nil)
	f.memberships.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.opaqueTokens.EXPECT().Generate().Return("raw-invite", "hash-invite", nil)
	f.invitations.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.expectOutbox(t, events.MemberInvited)
	f.expectMemberAudit(t, r.partner, "member.invite")
	f.sms.EXPECT().Send(gomock.Any(), staff.Phone, gomock.Any()).Return(nil)
	f.email.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)
	f.memberships.EXPECT().FindMember(gomock.Any(), r.partner.ID, gomock.Any()).Return(&entities.Member{}, nil)

	_, err := f.members.Invite(context.Background(), actorWith(r.partner, r.owner), inviteInput(r.operator))

	require.NoError(t, err)
}

func TestInviteToPartner_AgoraInvitesTheFirstOwner(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.withOrganization(r, r.partner)
	f.roles.EXPECT().FindByIDs(gomock.Any(), gomock.Any()).Return([]*entities.Role{r.owner}, nil)
	f.expectNoIdentity("op@agora.ao", "+244923000010")
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.memberships.EXPECT().FindByUserAndOrganization(gomock.Any(), gomock.Any(), r.partner.ID).Return(nil, nil)
	f.memberships.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m *entities.Membership) error {
			assert.Equal(t, r.partner.ID, m.OrganizationID)
			return nil
		})
	f.opaqueTokens.EXPECT().Generate().Return("raw-invite", "hash-invite", nil)
	f.invitations.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.expectOutbox(t, events.MemberInvited)
	f.expectMemberAudit(t, r.partner, "member.invite")
	f.sms.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.email.EXPECT().Send(gomock.Any(), gomock.Any()).Return(services.ErrEmailUnavailable)
	f.memberships.EXPECT().FindMember(gomock.Any(), r.partner.ID, gomock.Any()).Return(&entities.Member{}, nil)

	_, err := f.members.InviteToPartner(context.Background(), actorWith(r.platform, r.superAdmin), r.partner.ID, inviteInput(r.owner))

	require.NoError(t, err)
}

func TestInviteToPartner_OnlyPartnerOrganizations(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	f.orgs.EXPECT().FindByID(gomock.Any(), r.platform.ID).Return(r.platform, nil)

	_, err := f.members.InviteToPartner(context.Background(), actorWith(r.platform, r.superAdmin), r.platform.ID, inviteInput(r.owner))

	require.ErrorIs(t, err, usecases.ErrOrganizationNotFound)
}

type invitationCase struct {
	invitation *entities.Invitation
	membership *entities.Membership
	user       *entities.User
}

func (f fixture) expectPendingInvitation(t *testing.T, org *entities.Organization, lock bool) invitationCase {
	t.Helper()
	user, err := entities.NewInvitedStaffUser("Ana", "ana@agora.ao", phone)
	require.NoError(t, err)
	membership := &entities.Membership{ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID, Status: enums.MembershipStatusInvited, PermVersion: 1}
	invitation, err := entities.NewInvitation(membership.ID, "hash-invite", nil)
	require.NoError(t, err)

	f.opaqueTokens.EXPECT().Hash("raw-invite").Return("hash-invite")
	f.invitations.EXPECT().Scope(gomock.Any(), "hash-invite").Return(&domainrepository.InvitationScope{OrganizationID: org.ID, UserID: user.ID}, nil)
	if lock {
		f.invitations.EXPECT().LockByTokenHash(gomock.Any(), "hash-invite").Return(invitation, nil)
	} else {
		f.invitations.EXPECT().FindByTokenHash(gomock.Any(), "hash-invite").Return(invitation, nil)
	}
	f.memberships.EXPECT().LockByID(gomock.Any(), membership.ID).Return(membership, nil)
	f.orgs.EXPECT().FindByID(gomock.Any(), org.ID).Return(org, nil)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	return invitationCase{invitation: invitation, membership: membership, user: user}
}

func TestAcceptInvitation_SetsPasswordAndActivates(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	c := f.expectPendingInvitation(t, r.partner, true)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeInvitation, c.invitation.ID.String(), "123456").Return(nil)
	f.hasher.EXPECT().Hash(strongPass).Return(storedHash, nil)
	f.users.EXPECT().Update(gomock.Any(), c.user).Return(nil)
	f.memberships.EXPECT().Update(gomock.Any(), c.membership).Return(nil)
	f.invitations.EXPECT().Accept(gomock.Any(), c.invitation.ID, gomock.Any()).Return(nil)
	f.expectOutbox(t, events.MemberActivated)
	f.expectMemberAudit(t, r.partner, "member.activate")
	f.accessCache.EXPECT().Delete(gomock.Any(), c.membership.ID).Return(nil)

	err := f.invitationFlow.Accept(context.Background(), partnerPanel, dtos.AcceptInvitationInput{Token: "raw-invite", Password: strongPass, Code: "123456"}, "", "")

	require.NoError(t, err)
	assert.Equal(t, enums.MembershipStatusActive, c.membership.Status)
	assert.True(t, c.user.HasPassword())
	assert.True(t, c.user.IsVerified)
}

func TestAcceptInvitation_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("CA08.4 without a valid code nothing is saved", func(t *testing.T) {
		f := newFixture(t)
		c := f.expectPendingInvitation(t, r.partner, true)
		f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeInvitation, c.invitation.ID.String(), "000000").Return(services.ErrOTPInvalid)

		err := f.invitationFlow.Accept(context.Background(), partnerPanel, dtos.AcceptInvitationInput{Token: "raw-invite", Password: strongPass, Code: "000000"}, "", "")

		require.ErrorIs(t, err, usecases.ErrInvalidOTPCode)
		assert.Equal(t, enums.MembershipStatusInvited, c.membership.Status)
		assert.False(t, c.user.HasPassword())
	})

	t.Run("an invitation of a partner used in the backoffice", func(t *testing.T) {
		f := newFixture(t)
		user, _ := entities.NewInvitedStaffUser("Ana", "ana@agora.ao", phone)
		membership := &entities.Membership{ID: uuid.New(), UserID: user.ID, OrganizationID: r.partner.ID, Status: enums.MembershipStatusInvited}
		invitation, _ := entities.NewInvitation(membership.ID, "hash-invite", nil)
		f.opaqueTokens.EXPECT().Hash("raw-invite").Return("hash-invite")
		f.invitations.EXPECT().Scope(gomock.Any(), "hash-invite").Return(&domainrepository.InvitationScope{OrganizationID: r.partner.ID, UserID: user.ID}, nil)
		f.invitations.EXPECT().LockByTokenHash(gomock.Any(), "hash-invite").Return(invitation, nil)
		f.memberships.EXPECT().LockByID(gomock.Any(), membership.ID).Return(membership, nil)
		f.orgs.EXPECT().FindByID(gomock.Any(), r.partner.ID).Return(r.partner, nil)

		err := f.invitationFlow.Accept(context.Background(), backoffice, dtos.AcceptInvitationInput{Token: "raw-invite", Password: strongPass, Code: "123456"}, "", "")

		require.ErrorIs(t, err, usecases.ErrInvalidInvitation)
	})

	for name, change := range map[string]func(*entities.Invitation){
		"CA08.1 expired":  func(i *entities.Invitation) { i.ExpiresAt = time.Now().Add(-time.Minute) },
		"CA08.2 used":     func(i *entities.Invitation) { now := time.Now(); i.AcceptedAt = &now },
		"CA08.3 replaced": func(i *entities.Invitation) { now := time.Now(); i.RevokedAt = &now },
		"unknown token":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.opaqueTokens.EXPECT().Hash("raw-invite").Return("hash-invite")
			if change == nil {
				f.invitations.EXPECT().Scope(gomock.Any(), "hash-invite").Return(nil, nil)
			} else {
				invitation, _ := entities.NewInvitation(uuid.New(), "hash-invite", nil)
				change(invitation)
				f.invitations.EXPECT().Scope(gomock.Any(), "hash-invite").Return(&domainrepository.InvitationScope{OrganizationID: r.partner.ID, UserID: uuid.New()}, nil)
				f.invitations.EXPECT().LockByTokenHash(gomock.Any(), "hash-invite").Return(invitation, nil)
			}

			err := f.invitationFlow.Accept(context.Background(), partnerPanel, dtos.AcceptInvitationInput{Token: "raw-invite", Password: strongPass, Code: "123456"}, "", "")

			require.ErrorIs(t, err, usecases.ErrInvalidInvitation, "RN12 the same answer for every case")
		})
	}
}

func TestRequestInvitationCode_SendsAnSMSBoundToTheInvitation(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	c := f.expectPendingInvitation(t, r.platform, false)
	f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeInvitation, c.invitation.ID.String()).Return("123456", nil)
	f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, message string) error {
			assert.True(t, strings.Contains(message, "123456"))
			return nil
		})

	require.NoError(t, f.invitationFlow.RequestCode(context.Background(), backoffice, dtos.InvitationTokenInput{Token: "raw-invite"}))
}
