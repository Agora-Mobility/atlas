package usecases_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func (f fixture) expectRegistration(t *testing.T) *entities.User {
	t.Helper()
	created := &entities.User{}
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u *entities.User) error { *created = *u; return nil })
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, events.UserRegistered, env.EventType)
			return nil
		})
	f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeVerifyPhone, phone).Return("123456", nil)
	return created
}

func registerInput() dtos.RegisterInput {
	return dtos.RegisterInput{Phone: phone}
}

func newSignUpUser(t *testing.T, role enums.Role) *entities.User {
	t.Helper()
	user, err := entities.NewSignUpUser(phone, role)
	require.NoError(t, err)
	return user
}

func TestRegister_CreatesAnUnverifiedAccountWithOnlyThePhone(t *testing.T) {
	for client, role := range map[*entities.Client]enums.Role{customerApp: enums.RoleCustomer, providerApp: enums.RoleProvider} {
		t.Run(client.ClientID, func(t *testing.T) {
			f := newFixture(t)
			created := f.expectRegistration(t)
			f.sms.EXPECT().Send(gomock.Any(), phone, "Código de verificação: 123456. Válido por 5 min. Não o partilhe.").Return(nil)

			out, err := f.auth.Register(context.Background(), registerInput(), client)

			require.NoError(t, err)
			assert.Equal(t, role, out.Role)
			assert.Equal(t, role, created.Role)
			assert.False(t, out.IsVerified)
			assert.Empty(t, created.Name)
			assert.Empty(t, created.Email)
			assert.False(t, created.HasPassword())
		})
	}
}

func TestRegister_StoresPhoneInInternationalFormat(t *testing.T) {
	f := newFixture(t)
	created := f.expectRegistration(t)
	f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).Return(nil)

	out, err := f.auth.Register(context.Background(), dtos.RegisterInput{Phone: "923 000 000"}, customerApp)

	require.NoError(t, err)
	assert.Equal(t, phone, created.Phone)
	assert.Equal(t, phone, out.Phone)
}

func TestRegister_RejectsInvalidPhone(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), gomock.Any()).Return(nil, nil)

	_, err := f.auth.Register(context.Background(), dtos.RegisterInput{Phone: "123"}, customerApp)

	_, isValidation := customerrors.AsValidation(err)
	assert.True(t, isValidation)
}

func TestRegister_StaffClientsCannotRegister(t *testing.T) {
	for _, client := range []*entities.Client{partnerPanel, backoffice} {
		t.Run(client.ClientID, func(t *testing.T) {
			f := newFixture(t)

			_, err := f.auth.Register(context.Background(), registerInput(), client)

			require.ErrorIs(t, err, usecases.ErrRegistrationNotAllowed)
		})
	}
}

func TestRegister_RejectsTakenPhone(t *testing.T) {
	t.Run("verified account", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, true), nil)

		_, err := f.auth.Register(context.Background(), registerInput(), customerApp)

		require.ErrorIs(t, err, usecases.ErrPhoneAlreadyExists)
	})

	t.Run("unverified account of another role", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newSignUpUser(t, enums.RoleProvider), nil)

		_, err := f.auth.Register(context.Background(), registerInput(), customerApp)

		require.ErrorIs(t, err, usecases.ErrPhoneAlreadyExists)
	})
}

func TestRegister_UnfinishedRegistrationGetsANewCode(t *testing.T) {
	f := newFixture(t)
	pending := newSignUpUser(t, enums.RoleCustomer)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(pending, nil)
	f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeVerifyPhone, phone).Return("123456", nil)
	f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).Return(nil)

	out, err := f.auth.Register(context.Background(), registerInput(), customerApp)

	require.NoError(t, err)
	assert.Equal(t, pending.ID, out.ID)
}

func TestRegister_SucceedsEvenWhenSMSIsUnavailable(t *testing.T) {
	f := newFixture(t)
	f.expectRegistration(t)
	f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).Return(services.ErrSMSUnavailable)

	_, err := f.auth.Register(context.Background(), registerInput(), customerApp)

	require.NoError(t, err)
}

func TestLogin_AcceptsNationalPhoneFormat(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.expectSessionStarted(t, user, customerApp)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: "923000000", Password: strongPass}, customerApp, "", "")

	require.NoError(t, err)
}

func TestLogin_WithPhoneStartsSessionBoundToClient(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.expectSessionStarted(t, user, customerApp)

	login, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: strongPass}, customerApp, "10.0.0.1", "app")

	require.NoError(t, err)
	require.Nil(t, login.MFA)
	out := login.Session
	assert.Nil(t, out.Access)
	assert.Equal(t, "access", out.AccessToken)
	assert.Equal(t, rawRefresh, out.RefreshToken)
	assert.Equal(t, "Bearer", out.TokenType)
	assert.Equal(t, "agora-customer-app", out.Client)
	assert.InDelta(t, 900, out.ExpiresIn, 1)
	assert.NotNil(t, out.User.LastLoginAt)
}

func staffAccess(user *entities.User, scope enums.OrganizationType, requiresMFA bool) (*entities.Membership, *entities.Access) {
	membership := &entities.Membership{ID: uuid.New(), UserID: user.ID, OrganizationID: uuid.New(), Status: enums.MembershipStatusActive, PermVersion: 4}
	return membership, &entities.Access{
		MembershipID: membership.ID, UserID: user.ID, OrganizationID: membership.OrganizationID, Scope: scope,
		Status: enums.MembershipStatusActive, PermVersion: 4, RequiresMFA: requiresMFA,
		Permissions: []entities.Permission{entities.PermMembersRead},
	}
}

func (f fixture) expectStaffPassword(user *entities.User) {
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)
}

func TestLogin_StaffSessionCarriesTheMembership(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	membership, access := staffAccess(staff, enums.OrganizationTypePlatform, false)
	f.expectStaffPassword(staff)
	f.memberships.EXPECT().ListActiveForSignIn(gomock.Any(), staff.ID, enums.OrganizationTypePlatform).Return([]*entities.Membership{membership}, nil)
	f.memberships.EXPECT().Access(gomock.Any(), membership.ID).Return(access, nil)
	f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(nil, nil)
	f.expectStaffSessionStarted(t, staff, backoffice, access)

	out, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, backoffice, "", "")

	require.NoError(t, err)
	require.NotNil(t, out.Session)
	assert.Equal(t, enums.RoleStaff, out.Session.User.Role)
	assert.Equal(t, access.Permissions, out.Session.Access.Permissions)
}

func TestLogin_StaffWithoutActiveMembershipGetsTheGenericAnswer(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	f.expectStaffPassword(staff)
	f.memberships.EXPECT().ListActiveForSignIn(gomock.Any(), staff.ID, enums.OrganizationTypePartner).Return(nil, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, partnerPanel, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_StaffInSeveralOrganizationsEntersTheOldest(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	oldest, access := staffAccess(staff, enums.OrganizationTypePartner, false)
	newer, _ := staffAccess(staff, enums.OrganizationTypePartner, false)
	f.expectStaffPassword(staff)
	f.memberships.EXPECT().ListActiveForSignIn(gomock.Any(), staff.ID, enums.OrganizationTypePartner).Return([]*entities.Membership{oldest, newer}, nil)
	f.memberships.EXPECT().Access(gomock.Any(), oldest.ID).Return(access, nil)
	f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(nil, nil)
	f.expectStaffSessionStarted(t, staff, partnerPanel, access)

	out, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, partnerPanel, "", "")

	require.NoError(t, err)
	assert.Equal(t, oldest.OrganizationID, out.Session.Access.OrganizationID)
}

func TestLogin_EmailIsNotAnIdentifier(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), "ana@agora.ao").Return(nil, nil)
	f.hasher.EXPECT().Hash(gomock.Any()).Return("$argon2id$dummy", nil)
	f.hasher.EXPECT().Verify("x", "$argon2id$dummy").Return(false, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: "ana@agora.ao", Password: "x"}, backoffice, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_MFAIsRequiredBeforeAnySession(t *testing.T) {
	cases := []struct {
		name        string
		requiresMFA bool
		mfa         *entities.UserMFA
		status      string
	}{
		{"role requires mfa and it is not set up", true, nil, usecases.MFAStatusSetupRequired},
		{"pending setup is not enough", true, &entities.UserMFA{}, usecases.MFAStatusSetupRequired},
		{"person set mfa up although the role does not require it", false, &entities.UserMFA{ConfirmedAt: &time.Time{}}, usecases.MFAStatusCodeRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			staff := newUser(t, enums.RoleStaff, true)
			membership, access := staffAccess(staff, enums.OrganizationTypePlatform, tc.requiresMFA)
			f.expectStaffPassword(staff)
			f.memberships.EXPECT().ListActiveForSignIn(gomock.Any(), staff.ID, enums.OrganizationTypePlatform).Return([]*entities.Membership{membership}, nil)
			f.memberships.EXPECT().Access(gomock.Any(), membership.ID).Return(access, nil)
			f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(tc.mfa, nil)
			f.challenges.EXPECT().Create(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, c services.MFAChallenge) (string, error) {
					assert.Equal(t, staff.ID, c.UserID)
					assert.Equal(t, membership.ID, c.MembershipID)
					assert.Equal(t, backoffice.ID, c.ClientID)
					return "mfa-token", nil
				})

			out, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, backoffice, "", "")

			require.NoError(t, err)
			assert.Nil(t, out.Session, "no session before the second factor")
			assert.Equal(t, tc.status, out.MFA.Status)
			assert.Equal(t, "mfa-token", out.MFA.MFAToken)
		})
	}
}

func TestLogin_InvitedStaffWithoutPasswordCannotSignIn(t *testing.T) {
	f := newFixture(t)
	invited, err := entities.NewInvitedStaffUser("Ana", "ana@agora.ao", phone)
	require.NoError(t, err)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(invited, nil)
	f.hasher.EXPECT().Hash(gomock.Any()).Return("$argon2id$dummy", nil)
	f.hasher.EXPECT().Verify("x", "$argon2id$dummy").Return(false, false, nil)

	_, err = f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, backoffice, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_RoleMustMatchTheClient(t *testing.T) {
	cases := []struct {
		role   enums.Role
		client *entities.Client
	}{
		{enums.RoleStaff, customerApp},
		{enums.RoleStaff, providerApp},
		{enums.RoleCustomer, backoffice},
		{enums.RoleCustomer, providerApp},
		{enums.RoleProvider, customerApp},
		{enums.RoleProvider, partnerPanel},
	}
	for _, tc := range cases {
		t.Run(tc.role.String()+"@"+tc.client.ClientID, func(t *testing.T) {
			f := newFixture(t)
			f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, tc.role, true), nil)
			f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

			_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, tc.client, "", "")

			require.ErrorIs(t, err, usecases.ErrClientNotAllowed)
		})
	}
}

func TestLogin_UnknownUserSpendsHashTimeAndFails(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)
	f.hasher.EXPECT().Hash(gomock.Any()).Return("$argon2id$dummy", nil)
	f.hasher.EXPECT().Verify("x", "$argon2id$dummy").Return(false, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_WrongPasswordFails(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, true), nil)
	f.hasher.EXPECT().Verify("errada", storedHash).Return(false, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "errada"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_UnverifiedPhoneIsReported(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, false), nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrPhoneNotVerified)
}

func TestLogin_BlockedUserIsRejected(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleProvider, true)
	user.Block()
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, providerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrAccountBlocked)
}

func TestLogin_UpgradesOutdatedHash(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify("x", storedHash).Return(true, true, nil)
	f.hasher.EXPECT().Hash("x").Return("$argon2id$upgraded", nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.expectSessionStarted(t, user, customerApp)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Phone: phone, Password: "x"}, customerApp, "", "")

	require.NoError(t, err)
	assert.Equal(t, "$argon2id$upgraded", user.Password)
}

func TestLogout_RevokesOnlyTheCurrentSession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	session := user.ID
	f.expectSessionRevoked(session)

	require.NoError(t, f.auth.Logout(context.Background(), session))
}

func TestLogoutAll_RevokesEverySession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.expectAllSessionsRevoked(t, user.ID)

	require.NoError(t, f.auth.LogoutAll(context.Background(), user.ID))
}

func TestChangePassword_RejectsWrongCurrentPassword(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify("errada", storedHash).Return(false, false, nil)

	err := f.auth.ChangePassword(context.Background(), user.ID, "errada", "N0vo!segredo")

	require.ErrorIs(t, err, usecases.ErrInvalidCurrentPassword)
}

func TestChangePassword_ReplacesHashAndEndsAllSessions(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.hasher.EXPECT().Hash("N0vo!segredo").Return("$argon2id$new", nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.expectAllSessionsRevoked(t, user.ID)

	require.NoError(t, f.auth.ChangePassword(context.Background(), user.ID, strongPass, "N0vo!segredo"))
	assert.Equal(t, "$argon2id$new", user.Password)
}

func TestChangePassword_AccountWithoutPasswordMustSetOne(t *testing.T) {
	f := newFixture(t)
	user := newSignUpUser(t, enums.RoleCustomer)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	err := f.auth.ChangePassword(context.Background(), user.ID, "", "N0vo!segredo")

	require.ErrorIs(t, err, usecases.ErrPasswordNotSet)
}

func TestSetPassword_StoresTheFirstPasswordAndKeepsTheSessions(t *testing.T) {
	f := newFixture(t)
	user := newSignUpUser(t, enums.RoleCustomer)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Hash(strongPass).Return(storedHash, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

	require.NoError(t, f.auth.SetPassword(context.Background(), user.ID, strongPass))
	assert.Equal(t, storedHash, user.Password)
}

func TestSetPassword_Rejections(t *testing.T) {
	t.Run("account already has a password", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.auth.SetPassword(context.Background(), user.ID, strongPass)

		require.ErrorIs(t, err, usecases.ErrPasswordAlreadySet)
	})

	t.Run("short password", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.auth.SetPassword(context.Background(), user.ID, "curta")

		require.ErrorIs(t, err, usecases.ErrPasswordTooShort)
	})
}

func TestMe_UnknownUser(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(nil, nil)

	_, err := f.auth.Me(context.Background(), user.ID)

	require.ErrorIs(t, err, usecases.ErrUserNotFound)
}
