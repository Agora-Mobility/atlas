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
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func staffActor() entities.Actor {
	id := uuid.New()
	return entities.Actor{ID: &id, Role: enums.RoleStaff, IPAddress: "10.0.0.1", UserAgent: "backoffice"}
}

func (f fixture) expectAudit(t *testing.T, actor entities.Actor, action string, target uuid.UUID) {
	t.Helper()
	f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
			assert.Equal(t, action, e.Action)
			assert.Equal(t, "user", e.EntityType)
			assert.Equal(t, target.String(), e.EntityID)
			assert.Equal(t, actor, e.Actor)
			assert.NotContains(t, string(e.Before)+string(e.After), storedHash, "audit snapshots must not carry the password hash")
			return nil
		})
}

func (f fixture) expectEvent(t *testing.T, eventType string) {
	t.Helper()
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, eventType, env.EventType)
			return nil
		})
}

func blockReason(code string) *entities.BlockReason {
	return &entities.BlockReason{ID: uuid.New(), Code: code, Label: code, IsActive: true}
}

func TestBlock_RecordsReasonsEndsEverySessionAndIsAudited(t *testing.T) {
	f := newFixture(t)
	actor := staffActor()
	user := newUser(t, enums.RoleCustomer, true)
	fraud, duplicate := blockReason("fraud"), blockReason("duplicate_account")
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.blockReasons.EXPECT().ListActiveByIDs(gomock.Any(), []uuid.UUID{fraud.ID, duplicate.ID}).
		Return([]*entities.BlockReason{fraud, duplicate}, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.blockReasons.EXPECT().RecordBlock(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, b *entities.UserBlock) error {
			assert.Equal(t, user.ID, b.UserID)
			assert.Equal(t, actor.ID, b.BlockedBy)
			assert.Equal(t, []uuid.UUID{fraud.ID, duplicate.ID}, b.ReasonIDs())
			assert.Equal(t, "Reportado", b.Note)
			return nil
		})
	f.expectAllSessionsRevoked(t, user.ID)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, events.UserBlocked, env.EventType)
			assert.Contains(t, string(env.Payload), `"reasons":["fraud","duplicate_account"]`)
			return nil
		})
	f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
			assert.Equal(t, "user.block", e.Action)
			assert.Equal(t, "fraud, duplicate_account: Reportado", e.Reason)
			return nil
		})

	out, err := f.userAdmin.Block(context.Background(), actor, user.ID, dtos.BlockUserInput{
		ReasonIDs: []uuid.UUID{fraud.ID, duplicate.ID, fraud.ID}, Note: "  Reportado ",
	})

	require.NoError(t, err)
	assert.Equal(t, enums.UserStatusBlocked, out.Status)
}

func TestBlock_UnknownOrInactiveReasonIsRejected(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	fraud := blockReason("fraud")
	unknown := uuid.New()
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.blockReasons.EXPECT().ListActiveByIDs(gomock.Any(), []uuid.UUID{fraud.ID, unknown}).
		Return([]*entities.BlockReason{fraud}, nil)

	_, err := f.userAdmin.Block(context.Background(), staffActor(), user.ID, dtos.BlockUserInput{
		ReasonIDs: []uuid.UUID{fraud.ID, unknown},
	})

	require.ErrorIs(t, err, usecases.ErrUnknownReason)
	assert.Equal(t, enums.UserStatusActive, user.Status)
}

func TestListBlockReasons(t *testing.T) {
	f := newFixture(t)
	fraud := blockReason("fraud")
	f.blockReasons.EXPECT().ListActive(gomock.Any()).Return([]*entities.BlockReason{fraud}, nil)

	out, err := f.userAdmin.ListBlockReasons(context.Background())

	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, dtos.BlockReasonView{ID: fraud.ID, Code: "fraud", Label: "fraud"}, out.Items[0])
}

func TestBlock_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		target  enums.Role
		self    bool
		reasons []uuid.UUID
		lookup  bool
		want    error
	}{
		{"no reason", enums.RoleCustomer, false, nil, false, usecases.ErrReasonRequired},
		{"only nil reasons", enums.RoleCustomer, false, []uuid.UUID{uuid.Nil}, false, usecases.ErrReasonRequired},
		{"staff are managed as members", enums.RoleStaff, false, []uuid.UUID{uuid.New()}, true, usecases.ErrUserNotFound},
		{"own account", enums.RoleStaff, true, []uuid.UUID{uuid.New()}, false, usecases.ErrCannotManageSelf},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			actor := staffActor()
			user := newUser(t, tc.target, true)
			if tc.self {
				user.ID = *actor.ID
			}
			if tc.lookup {
				f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
			}

			_, err := f.userAdmin.Block(context.Background(), actor, user.ID, dtos.BlockUserInput{ReasonIDs: tc.reasons})

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestBlock_AlreadyBlockedChangesNothing(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleProvider, true)
	user.Block()
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	out, err := f.userAdmin.Block(context.Background(), staffActor(), user.ID, dtos.BlockUserInput{ReasonIDs: []uuid.UUID{uuid.New()}})

	require.NoError(t, err)
	assert.Equal(t, enums.UserStatusBlocked, out.Status)
}

func TestUnblock_ActivatesAndIsAudited(t *testing.T) {
	f := newFixture(t)
	actor := staffActor()
	user := newUser(t, enums.RoleProvider, true)
	user.Block()
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.expectEvent(t, events.UserUnblocked)
	f.expectAudit(t, actor, "user.unblock", user.ID)

	out, err := f.userAdmin.Unblock(context.Background(), actor, user.ID, dtos.UnblockUserInput{})

	require.NoError(t, err)
	assert.Equal(t, enums.UserStatusActive, out.Status)
}

func TestResetPassword_ReplacesHashEndsSessionsAndIsAudited(t *testing.T) {
	f := newFixture(t)
	actor := staffActor()
	user := newUser(t, enums.RoleProvider, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Hash("N0vo!segredo").Return("$argon2id$new", nil)
	f.expectAudit(t, actor, "user.reset-password", user.ID)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.expectAllSessionsRevoked(t, user.ID)

	err := f.userAdmin.ResetPassword(context.Background(), actor, user.ID, dtos.ResetPasswordInput{NewPassword: "N0vo!segredo"})

	require.NoError(t, err)
	assert.Equal(t, "$argon2id$new", user.Password)
}

func TestList_OnlyAppAccountsByDefault(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().List(gomock.Any(),
		repository.UserFilter{Roles: []enums.Role{enums.RoleCustomer, enums.RoleProvider}},
		pagination.Params{},
	).Return(nil, int64(0), nil)

	page, err := f.userAdmin.List(context.Background(), dtos.UserListQuery{})

	require.NoError(t, err)
	assert.Empty(t, page.Items)
}

func TestGet_StaffAreNotAppAccounts(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)

	_, err := f.userAdmin.Get(context.Background(), staff.ID)

	require.ErrorIs(t, err, usecases.ErrUserNotFound)
}

func TestList_PassesFiltersAndPages(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleProvider, true)
	f.users.EXPECT().List(gomock.Any(),
		repository.UserFilter{Roles: []enums.Role{enums.RoleProvider}, Status: enums.UserStatusActive, Search: "ana"},
		pagination.Params{Page: 2, Size: 1},
	).Return([]*entities.User{user}, int64(3), nil)

	page, err := f.userAdmin.List(context.Background(), dtos.UserListQuery{
		Role: enums.RoleProvider, Status: enums.UserStatusActive, Search: "ana", Page: 2, Size: 1,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(3), page.TotalPages)
	require.Len(t, page.Items, 1)
	assert.Equal(t, user.ID, page.Items[0].ID)
}

func TestAdminBootstrap(t *testing.T) {
	platform := &entities.Organization{ID: uuid.New(), Type: enums.OrganizationTypePlatform, Name: "AGORA"}
	superAdmin := &entities.Role{ID: uuid.New(), Scope: enums.OrganizationTypePlatform, Code: entities.RoleCodeSuperAdmin, IsSystem: true}

	t.Run("creates the first super admin as the system", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.EXPECT().Platform(gomock.Any()).Return(platform, nil)
		f.roles.EXPECT().FindSystemRole(gomock.Any(), enums.OrganizationTypePlatform, entities.RoleCodeSuperAdmin).Return(superAdmin, nil)
		f.memberships.EXPECT().ExistsActiveWithRole(gomock.Any(), platform.ID, superAdmin.ID).Return(false, nil)
		f.users.EXPECT().FindByEmail(gomock.Any(), "admin@agora.ao").Return(nil, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)
		f.hasher.EXPECT().Hash("uma-frase-longa").Return(storedHash, nil)
		var created *entities.User
		f.users.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, u *entities.User) error { created = u; return nil })
		f.memberships.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, m *entities.Membership) error {
				assert.Equal(t, created.ID, m.UserID)
				assert.Equal(t, platform.ID, m.OrganizationID)
				assert.Equal(t, enums.MembershipStatusActive, m.Status)
				assert.Equal(t, []uuid.UUID{superAdmin.ID}, m.RoleIDs)
				return nil
			})
		f.expectEvent(t, events.UserRegistered)
		f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
				assert.Nil(t, e.Actor.ID)
				assert.Equal(t, "member.bootstrap-super-admin", e.Action)
				assert.Equal(t, platform.ID, *e.OrganizationID)
				return nil
			})

		ok, err := f.bootstrap.Run(context.Background(), "Admin", "admin@agora.ao", phone, "uma-frase-longa")

		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, enums.RoleStaff, created.Role)
		assert.True(t, created.IsVerified)
		assert.Equal(t, phone, created.Phone, "the admin signs in with the phone")
	})

	t.Run("does nothing when an active super admin exists", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.EXPECT().Platform(gomock.Any()).Return(platform, nil)
		f.roles.EXPECT().FindSystemRole(gomock.Any(), enums.OrganizationTypePlatform, entities.RoleCodeSuperAdmin).Return(superAdmin, nil)
		f.memberships.EXPECT().ExistsActiveWithRole(gomock.Any(), platform.ID, superAdmin.ID).Return(true, nil)

		ok, err := f.bootstrap.Run(context.Background(), "Admin", "admin@agora.ao", phone, "uma-frase-longa")

		require.NoError(t, err)
		assert.False(t, ok)
	})
}
