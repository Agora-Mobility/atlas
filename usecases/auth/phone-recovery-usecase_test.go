package usecases_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func TestConfirmPhone_MarksUserVerifiedAndStartsSession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, false)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeVerifyPhone, phone, "123456").Return(nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, events.UserPhoneVerified, env.EventType)
			return nil
		})
	f.expectSessionStarted(t, user, customerApp)

	out, err := f.phoneVerification.Confirm(context.Background(),
		dtos.PhoneVerificationConfirmInput{Phone: phone, Code: "123456"}, customerApp, "", "")

	require.NoError(t, err)
	assert.True(t, out.User.IsVerified)
}

func TestConfirmPhone_AcceptsInternationalDialingPrefix(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeVerifyPhone, phone, "123456").Return(nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.expectSessionStarted(t, user, customerApp)

	_, err := f.phoneVerification.Confirm(context.Background(),
		dtos.PhoneVerificationConfirmInput{Phone: "00244 923 000 000", Code: "123456"}, customerApp, "", "")

	require.NoError(t, err)
}

func TestConfirmPhone_WrongCodeStopsTheFlow(t *testing.T) {
	f := newFixture(t)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeVerifyPhone, phone, "000000").Return(services.ErrOTPInvalid)

	_, err := f.phoneVerification.Confirm(context.Background(),
		dtos.PhoneVerificationConfirmInput{Phone: phone, Code: "000000"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidOTPCode)
}

func TestConfirmPhone_WrongClientGetsNoSession(t *testing.T) {
	f := newFixture(t)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeVerifyPhone, phone, "123456").Return(nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleProvider, false), nil)

	_, err := f.phoneVerification.Confirm(context.Background(),
		dtos.PhoneVerificationConfirmInput{Phone: phone, Code: "123456"}, customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrClientNotAllowed)
}

func TestRequestPhoneCode_IsSilentForUnknownPhonesAndCooldown(t *testing.T) {
	t.Run("unknown phone", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)

		require.NoError(t, f.phoneVerification.RequestCode(context.Background(), dtos.PhoneVerificationRequestInput{Phone: phone}, customerApp))
	})

	t.Run("cooldown", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, false), nil)
		f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeVerifyPhone, phone).Return("", services.ErrOTPCooldown)

		require.NoError(t, f.phoneVerification.RequestCode(context.Background(), dtos.PhoneVerificationRequestInput{Phone: phone}, customerApp))
	})
}

func TestRequestRecovery_SendsCodeOnlyForAccountsOfTheClient(t *testing.T) {
	t.Run("own client", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleCustomer, true), nil)
		f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeResetPassword, phone).Return("123456", nil)
		f.sms.EXPECT().Send(gomock.Any(), phone, "Código de recuperação: 123456. Válido por 5 min. Não o partilhe.").Return(nil)

		require.NoError(t, f.recovery.RequestRecovery(context.Background(), dtos.RecoveryRequestInput{Phone: phone}, customerApp))
	})

	t.Run("staff account through the customer app", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(newUser(t, enums.RoleAdmin, true), nil)

		require.NoError(t, f.recovery.RequestRecovery(context.Background(), dtos.RecoveryRequestInput{Phone: phone}, customerApp))
	})

	t.Run("unknown phone", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(nil, nil)

		require.NoError(t, f.recovery.RequestRecovery(context.Background(), dtos.RecoveryRequestInput{Phone: phone}, customerApp))
	})
}

func TestConfirmRecovery_ReplacesHashAndEndsAllSessions(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeResetPassword, phone, "123456").Return(nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), phone).Return(user, nil)
	f.hasher.EXPECT().Hash("N0vo!segredo").Return("$argon2id$new", nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.refreshTokens.EXPECT().RevokeAllForUser(gomock.Any(), user.ID, gomock.Any()).Return(nil)

	err := f.recovery.ConfirmRecovery(context.Background(),
		dtos.RecoveryConfirmInput{Phone: phone, Code: "123456", NewPassword: "N0vo!segredo"}, customerApp)

	require.NoError(t, err)
	assert.Equal(t, "$argon2id$new", user.Password)
}

func TestConfirmRecovery_RejectsShortPasswordBeforeSpendingTheCode(t *testing.T) {
	f := newFixture(t)

	err := f.recovery.ConfirmRecovery(context.Background(),
		dtos.RecoveryConfirmInput{Phone: phone, Code: "123456", NewPassword: "curta"}, customerApp)

	require.ErrorIs(t, err, usecases.ErrPasswordTooShort)
}
