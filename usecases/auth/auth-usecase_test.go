package usecases_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func (f fixture) expectRegistration(t *testing.T) *entities.User {
	t.Helper()
	created := &entities.User{}
	f.users.EXPECT().FindByEmail(gomock.Any(), "ana@agora.ao").Return(nil, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)
	f.hasher.EXPECT().Hash(strongPass).Return(storedHash, nil)
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
	return dtos.RegisterInput{Name: "Ana", Email: "Ana@Agora.ao", Phone: phone, Password: strongPass}
}

func TestRegister_RoleComesFromTheClient(t *testing.T) {
	for client, role := range map[*entities.Client]enums.Role{customerApp: enums.RoleCustomer, providerApp: enums.RoleProvider} {
		t.Run(client.ClientID, func(t *testing.T) {
			f := newFixture(t)
			created := f.expectRegistration(t)
			f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).Return(nil)

			out, err := f.auth.Register(context.Background(), registerInput(), client)

			require.NoError(t, err)
			assert.Equal(t, role, out.Role)
			assert.Equal(t, role, created.Role)
			assert.False(t, out.IsVerified)
			assert.Equal(t, storedHash, created.Password)
		})
	}
}

func TestRegister_StoresPhoneInInternationalFormat(t *testing.T) {
	f := newFixture(t)
	created := f.expectRegistration(t)
	f.sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).Return(nil)
	input := registerInput()
	input.Phone = "923 000 000"

	out, err := f.auth.Register(context.Background(), input, customerApp)

	require.NoError(t, err)
	assert.Equal(t, phone, created.Phone)
	assert.Equal(t, phone, out.Phone)
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

func TestRegister_RejectsTakenIdentity(t *testing.T) {
	t.Run("email", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByEmail(gomock.Any(), "ana@agora.ao").Return(newUser(t, enums.RoleCustomer, true), nil)

		_, err := f.auth.Register(context.Background(), registerInput(), customerApp)

		require.ErrorIs(t, err, usecases.ErrEmailAlreadyExists)
	})

	t.Run("phone", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByEmail(gomock.Any(), "ana@agora.ao").Return(nil, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, true), nil)

		_, err := f.auth.Register(context.Background(), registerInput(), customerApp)

		require.ErrorIs(t, err, usecases.ErrPhoneAlreadyExists)
	})
}

func TestRegister_RejectsShortPassword(t *testing.T) {
	f := newFixture(t)
	input := registerInput()
	input.Password = "curta"

	_, err := f.auth.Register(context.Background(), input, customerApp)

	require.ErrorIs(t, err, usecases.ErrPasswordTooShort)
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

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: "923000000", Password: strongPass}, customerApp, "", "")

	require.NoError(t, err)
}

func TestLogin_WithPhoneStartsSessionBoundToClient(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.expectSessionStarted(t, user, customerApp)

	out, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: strongPass}, customerApp, "10.0.0.1", "app")

	require.NoError(t, err)
	assert.Equal(t, "access", out.AccessToken)
	assert.Equal(t, rawRefresh, out.RefreshToken)
	assert.Equal(t, "Bearer", out.TokenType)
	assert.Equal(t, "agora-customer-app", out.Client)
	assert.InDelta(t, 900, out.ExpiresIn, 1)
	assert.NotNil(t, out.User.LastLoginAt)
}

func TestLogin_StaffWithEmailThroughBackoffice(t *testing.T) {
	f := newFixture(t)
	admin := newUser(t, enums.RoleAdmin, true)
	f.users.EXPECT().FindByEmail(gomock.Any(), "ana@agora.ao").Return(admin, nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)
	f.expectSessionStarted(t, admin, backoffice)

	out, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: " ANA@agora.ao ", Password: "x"}, backoffice, "", "")

	require.NoError(t, err)
	assert.Equal(t, enums.RoleAdmin, out.User.Role)
}

func TestLogin_RoleMustMatchTheClient(t *testing.T) {
	cases := []struct {
		role   enums.Role
		client *entities.Client
	}{
		{enums.RoleAdmin, customerApp},
		{enums.RoleCustomer, backoffice},
		{enums.RoleCustomer, providerApp},
		{enums.RoleProvider, customerApp},
		{enums.RolePartner, backoffice},
		{enums.RoleOperator, partnerPanel},
	}
	for _, tc := range cases {
		t.Run(tc.role.String()+"@"+tc.client.ClientID, func(t *testing.T) {
			f := newFixture(t)
			f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, tc.role, true), nil)
			f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

			_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "x"}, tc.client, "", "")

			require.ErrorIs(t, err, usecases.ErrClientNotAllowed)
		})
	}
}

func TestLogin_UnknownUserSpendsHashTimeAndFails(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)
	f.hasher.EXPECT().Hash(gomock.Any()).Return("$argon2id$dummy", nil)
	f.hasher.EXPECT().Verify("x", "$argon2id$dummy").Return(false, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "x"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_WrongPasswordFails(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, true), nil)
	f.hasher.EXPECT().Verify("errada", storedHash).Return(false, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "errada"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidCredentials)
}

func TestLogin_UnverifiedPhoneIsReported(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, false), nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "x"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrPhoneNotVerified)
}

func TestLogin_BlockedUserIsRejected(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleProvider, true)
	user.Block()
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Verify(gomock.Any(), storedHash).Return(true, false, nil)

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "x"}, providerApp, "", "")

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

	_, err := f.auth.Login(context.Background(), dtos.LoginInput{Identifier: phone, Password: "x"}, customerApp, "", "")

	require.NoError(t, err)
	assert.Equal(t, "$argon2id$upgraded", user.Password)
}

func TestLogout_RevokesOnlyTheCurrentSession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	session := user.ID
	f.refreshTokens.EXPECT().RevokeFamily(gomock.Any(), session, gomock.Any()).Return(nil)

	require.NoError(t, f.auth.Logout(context.Background(), session))
}

func TestLogoutAll_RevokesEverySession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.refreshTokens.EXPECT().RevokeAllForUser(gomock.Any(), user.ID, gomock.Any()).Return(nil)

	require.NoError(t, f.auth.LogoutAll(context.Background(), user.ID))
}

func TestChangePassword_RejectsWrongCurrentPassword(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleOperator, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify("errada", storedHash).Return(false, false, nil)

	err := f.auth.ChangePassword(context.Background(), user.ID, "errada", "N0vo!segredo")

	require.ErrorIs(t, err, usecases.ErrInvalidCurrentPassword)
}

func TestChangePassword_ReplacesHashAndEndsAllSessions(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleOperator, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.hasher.EXPECT().Hash("N0vo!segredo").Return("$argon2id$new", nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.refreshTokens.EXPECT().RevokeAllForUser(gomock.Any(), user.ID, gomock.Any()).Return(nil)

	require.NoError(t, f.auth.ChangePassword(context.Background(), user.ID, strongPass, "N0vo!segredo"))
	assert.Equal(t, "$argon2id$new", user.Password)
}

func TestMe_UnknownUser(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(nil, nil)

	_, err := f.auth.Me(context.Background(), user.ID)

	require.ErrorIs(t, err, usecases.ErrUserNotFound)
}
