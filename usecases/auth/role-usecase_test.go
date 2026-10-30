package usecases_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func (f fixture) withCatalog(scope enums.OrganizationType, codes ...entities.Permission) {
	defs := make([]*entities.PermissionDefinition, 0, len(codes))
	for _, c := range codes {
		defs = append(defs, &entities.PermissionDefinition{Code: c, Scopes: []enums.OrganizationType{scope}})
	}
	f.roles.EXPECT().ListPermissions(gomock.Any(), scope).Return(defs, nil).AnyTimes()
}

func customRole(org *entities.Organization, perms ...entities.Permission) *entities.Role {
	return &entities.Role{ID: uuid.New(), OrganizationID: &org.ID, Scope: org.Type, Code: "field_support", Name: "Suporte de campo", Permissions: perms}
}

func TestCreateRole(t *testing.T) {
	r := newRBAC()

	t.Run("creates a custom role of the organization", func(t *testing.T) {
		f := newFixture(t)
		f.withCatalog(enums.OrganizationTypePlatform, entities.PermMembersRead, entities.PermMembersInvite)
		f.roles.EXPECT().FindSystemRole(gomock.Any(), enums.OrganizationTypePlatform, "field_support").Return(nil, nil)
		f.roles.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, role *entities.Role) error {
				assert.Equal(t, r.platform.ID, *role.OrganizationID)
				assert.False(t, role.IsSystem)
				assert.Equal(t, []entities.Permission{entities.PermMembersRead}, role.Permissions)
				return nil
			})
		f.expectOutbox(t, events.RoleCreated)
		f.expectMemberAudit(t, r.platform, "role.create")

		out, err := f.roleAdmin.Create(context.Background(), actorWith(r.platform, r.superAdmin), dtos.CreateRoleInput{
			Code: "field_support", Name: "Suporte de campo", Permissions: []string{"members:read", "members:read"},
		})

		require.NoError(t, err)
		assert.Equal(t, "field_support", out.Code)
	})

	cases := []struct {
		name  string
		input dtos.CreateRoleInput
		want  error
	}{
		{"unknown permission", dtos.CreateRoleInput{Code: "x_role", Name: "X", Permissions: []string{"partner.finance:read"}}, usecases.ErrUnknownPermission},
		{"permission the actor lacks", dtos.CreateRoleInput{Code: "x_role", Name: "X", Permissions: []string{"members:invite"}}, usecases.ErrPrivilegeEscalation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.withCatalog(enums.OrganizationTypePlatform, entities.PermMembersRead, entities.PermMembersInvite)

			_, err := f.roleAdmin.Create(context.Background(), actorWith(r.platform, r.operations), tc.input)

			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("the code of a system role is taken", func(t *testing.T) {
		f := newFixture(t)
		f.withCatalog(enums.OrganizationTypePlatform, entities.PermMembersRead)
		f.roles.EXPECT().FindSystemRole(gomock.Any(), enums.OrganizationTypePlatform, "operations").Return(r.operations, nil)

		_, err := f.roleAdmin.Create(context.Background(), actorWith(r.platform, r.superAdmin), dtos.CreateRoleInput{
			Code: "operations", Name: "Outra", Permissions: []string{"members:read"},
		})

		require.ErrorIs(t, err, usecases.ErrRoleCodeTaken)
	})
}

func TestUpdateRole_NewPermissionsReachEveryHolderOnTheNextRequest(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	role := customRole(r.platform, entities.PermMembersRead)
	holders := []uuid.UUID{uuid.New(), uuid.New()}
	f.withCatalog(enums.OrganizationTypePlatform, entities.PermMembersRead, entities.PermMembersInvite)
	f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{role.ID}).Return([]*entities.Role{role}, nil)
	f.roles.EXPECT().Update(gomock.Any(), role).Return(nil)
	f.roles.EXPECT().BumpMembershipsWithRole(gomock.Any(), role.ID, gomock.Any()).Return(holders, nil)
	f.expectOutbox(t, events.RoleUpdated)
	f.expectMemberAudit(t, r.platform, "role.update")
	f.accessCache.EXPECT().Delete(gomock.Any(), holders[0], holders[1]).Return(nil)

	out, err := f.roleAdmin.Update(context.Background(), actorWith(r.platform, r.superAdmin), role.ID, dtos.UpdateRoleInput{
		Permissions: []string{"members:read", "members:invite"},
	})

	require.NoError(t, err)
	assert.Equal(t, []entities.Permission{entities.PermMembersInvite, entities.PermMembersRead}, out.Permissions)
}

func TestUpdateAndDeleteRole_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("system roles are immutable", func(t *testing.T) {
		f := newFixture(t)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{r.admin.ID}).Return([]*entities.Role{r.admin}, nil)

		_, err := f.roleAdmin.Update(context.Background(), actorWith(r.platform, r.superAdmin), r.admin.ID, dtos.UpdateRoleInput{})

		require.ErrorIs(t, err, usecases.ErrSystemRole)
	})

	t.Run("a role of another organization is not revealed", func(t *testing.T) {
		f := newFixture(t)
		foreign := customRole(r.partner, entities.PermMembersRead)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{foreign.ID}).Return([]*entities.Role{foreign}, nil)

		err := f.roleAdmin.Delete(context.Background(), actorWith(r.platform, r.superAdmin), foreign.ID)

		require.ErrorIs(t, err, usecases.ErrRoleNotFound)
	})

	t.Run("a role still assigned cannot be deleted", func(t *testing.T) {
		f := newFixture(t)
		role := customRole(r.platform, entities.PermMembersRead)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{role.ID}).Return([]*entities.Role{role}, nil)
		f.roles.EXPECT().IsAssigned(gomock.Any(), role.ID).Return(true, nil)

		err := f.roleAdmin.Delete(context.Background(), actorWith(r.platform, r.superAdmin), role.ID)

		require.ErrorIs(t, err, usecases.ErrRoleInUse)
	})

	t.Run("nobody edits a role with permissions they lack", func(t *testing.T) {
		f := newFixture(t)
		role := customRole(r.platform, entities.PermMembersInvite)
		f.roles.EXPECT().FindByIDs(gomock.Any(), []uuid.UUID{role.ID}).Return([]*entities.Role{role}, nil)

		err := f.roleAdmin.Delete(context.Background(), actorWith(r.platform, r.operations), role.ID)

		require.ErrorIs(t, err, usecases.ErrPrivilegeEscalation)
	})
}

func TestResetMFA(t *testing.T) {
	r := newRBAC()

	t.Run("removes the authenticator and ends every session", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusActive, r.manager)
		f.memberships.EXPECT().LockInOrganization(gomock.Any(), r.partner.ID, target.ID).Return(target, nil)
		f.memberships.EXPECT().HasMembershipOutside(gomock.Any(), target.UserID, r.partner.ID).Return(false, nil)
		f.mfa.EXPECT().Delete(gomock.Any(), target.UserID).Return(nil)
		f.expectAllSessionsRevoked(t, target.UserID)
		f.expectOutbox(t, events.MemberMFAReset)
		f.expectMemberAudit(t, r.partner, "member.mfa-reset")

		require.NoError(t, f.members.ResetMFA(context.Background(), actorWith(r.partner, r.owner), target.ID))
	})

	t.Run("a partner cannot reset someone who also works elsewhere", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.partner)
		target := memberOf(r.partner, enums.MembershipStatusActive, r.manager)
		f.memberships.EXPECT().LockInOrganization(gomock.Any(), r.partner.ID, target.ID).Return(target, nil)
		f.memberships.EXPECT().HasMembershipOutside(gomock.Any(), target.UserID, r.partner.ID).Return(true, nil)

		err := f.members.ResetMFA(context.Background(), actorWith(r.partner, r.owner), target.ID)

		require.ErrorIs(t, err, usecases.ErrMFAResetNotAllowed)
	})

	t.Run("own authenticator", func(t *testing.T) {
		f := newFixture(t)
		f.withOrganization(r, r.platform)
		actor := actorWith(r.platform, r.superAdmin)
		self := memberOf(r.platform, enums.MembershipStatusActive, r.superAdmin)
		self.UserID = actor.Access.UserID
		f.memberships.EXPECT().LockInOrganization(gomock.Any(), r.platform.ID, self.ID).Return(self, nil)

		require.ErrorIs(t, f.members.ResetMFA(context.Background(), actor, self.ID), usecases.ErrCannotManageSelf)
	})
}
