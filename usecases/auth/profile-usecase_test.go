package usecases_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

const newPhone = "+244923111111"

func ptr[T any](v T) *T { return &v }

func TestUpdateProfile_ChangesNameWithoutPassword(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

	out, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Name: ptr("  Ana Maria ")})

	require.NoError(t, err)
	assert.Equal(t, "Ana Maria", out.Name)
	assert.Equal(t, "ana@agora.ao", out.Email)
}

func TestUpdateProfile_EmailChangeNeedsTheCurrentPassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		matches  bool
		want     error
	}{
		{"missing password", "", false, usecases.ErrCurrentPasswordRequired},
		{"wrong password", "errada", false, usecases.ErrInvalidCurrentPassword},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			user := newUser(t, enums.RoleCustomer, true)
			f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
			if tc.password != "" {
				f.hasher.EXPECT().Verify(tc.password, storedHash).Return(tc.matches, false, nil)
			}

			_, err := f.profile.UpdateProfile(context.Background(), user.ID,
				dtos.UpdateProfileInput{Email: ptr("nova@agora.ao"), CurrentPassword: tc.password})

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestUpdateProfile_EmailOfAnotherUserIsRejected(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	other := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.users.EXPECT().FindByEmail(gomock.Any(), "nova@agora.ao").Return(other, nil)

	_, err := f.profile.UpdateProfile(context.Background(), user.ID,
		dtos.UpdateProfileInput{Email: ptr("Nova@Agora.ao"), CurrentPassword: strongPass})

	require.ErrorIs(t, err, usecases.ErrEmailAlreadyExists)
}

func TestUpdateProfile_RemovesEmailWhenThePhoneRemains(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

	out, err := f.profile.UpdateProfile(context.Background(), user.ID,
		dtos.UpdateProfileInput{Email: ptr(""), CurrentPassword: strongPass})

	require.NoError(t, err)
	assert.Empty(t, out.Email)
}

func TestUpdateProfile_StaffKeepsTheEmailTheySignInWith(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)

	_, err := f.profile.UpdateProfile(context.Background(), staff.ID, dtos.UpdateProfileInput{Email: ptr("  "), CurrentPassword: strongPass})

	validation, ok := customerrors.AsValidation(err)
	require.True(t, ok, "%v", err)
	assert.Contains(t, validation.Fields, "email")
}

func TestUpdateProfile_SetsTheNameOfANewAccount(t *testing.T) {
	f := newFixture(t)
	user := newSignUpUser(t, enums.RoleCustomer)
	user.MarkVerified()
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

	out, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Name: ptr("  Nelândio Manuel ")})

	require.NoError(t, err)
	assert.Equal(t, "Nelândio Manuel", out.Name)
}

func TestUpdateProfile_BlankNameIsAValidationError(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	_, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Name: ptr("   ")})

	_, isValidation := customerrors.AsValidation(err)
	assert.True(t, isValidation)
}

func TestUpdateProfile_InvalidNameIsAValidationError(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	_, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Name: ptr("A")})

	_, isValidation := customerrors.AsValidation(err)
	assert.True(t, isValidation)
}

func TestRequestPhoneChange_SendsACodeBoundToTheUser(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), newPhone).Return(nil, nil)
	f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeChangePhone, user.ID.String()+":"+newPhone).Return("654321", nil)
	f.sms.EXPECT().Send(gomock.Any(), newPhone, "Código de novo número: 654321. Válido por 5 min. Não o partilhe.").Return(nil)

	err := f.profile.RequestPhoneChange(context.Background(), user.ID, dtos.PhoneChangeRequestInput{Phone: "923 111 111"})

	require.NoError(t, err)
}

func TestRequestPhoneChange_Rejections(t *testing.T) {
	t.Run("same phone", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.profile.RequestPhoneChange(context.Background(), user.ID, dtos.PhoneChangeRequestInput{Phone: phone})

		require.ErrorIs(t, err, usecases.ErrPhoneUnchanged)
	})

	t.Run("phone of another account", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), newPhone).Return(newUser(t, enums.RoleProvider, true), nil)

		err := f.profile.RequestPhoneChange(context.Background(), user.ID, dtos.PhoneChangeRequestInput{Phone: newPhone})

		require.ErrorIs(t, err, usecases.ErrPhoneAlreadyExists)
	})

	t.Run("invalid phone", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.profile.RequestPhoneChange(context.Background(), user.ID, dtos.PhoneChangeRequestInput{Phone: "12"})

		_, isValidation := customerrors.AsValidation(err)
		assert.True(t, isValidation)
	})

	t.Run("code sent recently", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
		f.users.EXPECT().FindByPhone(gomock.Any(), newPhone).Return(nil, nil)
		f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeChangePhone, gomock.Any()).Return("", services.ErrOTPCooldown)

		err := f.profile.RequestPhoneChange(context.Background(), user.ID, dtos.PhoneChangeRequestInput{Phone: newPhone})

		require.ErrorIs(t, err, usecases.ErrCodeRecentlySent)
	})
}

func TestConfirmPhoneChange_UpdatesThePhoneAndPublishesTheChange(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), newPhone).Return(nil, nil)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeChangePhone, user.ID.String()+":"+newPhone, "654321").Return(nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, events.UserPhoneChanged, env.EventType)
			return nil
		})

	out, err := f.profile.ConfirmPhoneChange(context.Background(), user.ID,
		dtos.PhoneChangeConfirmInput{Phone: newPhone, Code: "654321"})

	require.NoError(t, err)
	assert.Equal(t, newPhone, out.Phone)
	assert.True(t, out.IsVerified)
}

func TestConfirmPhoneChange_WrongCodeKeepsThePhone(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.users.EXPECT().FindByPhone(gomock.Any(), newPhone).Return(nil, nil)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeChangePhone, gomock.Any(), "000000").Return(services.ErrOTPInvalid)

	_, err := f.profile.ConfirmPhoneChange(context.Background(), user.ID,
		dtos.PhoneChangeConfirmInput{Phone: newPhone, Code: "000000"})

	require.ErrorIs(t, err, usecases.ErrInvalidOTPCode)
	assert.Equal(t, phone, user.Phone)
}

func TestDeleteAccount_SoftDeletesAndEndsEverySession(t *testing.T) {
	for _, role := range []enums.Role{enums.RoleCustomer, enums.RoleProvider} {
		t.Run(role.String(), func(t *testing.T) {
			f := newFixture(t)
			user := newUser(t, role, true)
			f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
			f.hasher.EXPECT().Verify(strongPass, storedHash).Return(true, false, nil)
			f.users.EXPECT().SoftDelete(gomock.Any(), user.ID, gomock.Any()).Return(nil)
			f.expectAllSessionsRevoked(t, user.ID)
			f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, env events.Envelope) error {
					assert.Equal(t, events.UserDeleted, env.EventType)
					return nil
				})

			require.NoError(t, f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{Password: strongPass}))
		})
	}
}

func TestDeleteAccount_Rejections(t *testing.T) {
	t.Run("staff account", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleStaff, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{Password: strongPass})

		require.ErrorIs(t, err, usecases.ErrAccountDeletionNotAllowed)
	})

	t.Run("wrong password", func(t *testing.T) {
		f := newFixture(t)
		user := newUser(t, enums.RoleCustomer, true)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
		f.hasher.EXPECT().Verify("errada", storedHash).Return(false, false, nil)

		err := f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{Password: "errada"})

		require.ErrorIs(t, err, usecases.ErrInvalidCurrentPassword)
	})

	t.Run("account without password", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{Password: strongPass})

		require.ErrorIs(t, err, usecases.ErrPasswordNotSet)
	})

	t.Run("account without password and no code", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		err := f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{})

		require.ErrorIs(t, err, usecases.ErrConfirmationRequired)
	})
}

func TestRequestIdentityCode_SendsACodeToTheAccountPhone(t *testing.T) {
	f := newFixture(t)
	user := newSignUpUser(t, enums.RoleCustomer)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.otp.EXPECT().Issue(gomock.Any(), enums.OTPPurposeConfirmIdentity, user.ID.String()).Return("654321", nil)
	f.sms.EXPECT().Send(gomock.Any(), user.Phone, "Código de confirmação: 654321. Válido por 5 min. Não o partilhe.").Return(nil)

	require.NoError(t, f.profile.RequestIdentityCode(context.Background(), user.ID))
}

func TestRequestIdentityCode_StaffConfirmWithThePassword(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	require.ErrorIs(t, f.profile.RequestIdentityCode(context.Background(), user.ID), usecases.ErrClientNotAllowed)
}

func TestUpdateProfile_AccountWithoutPasswordConfirmsTheEmailWithACode(t *testing.T) {
	t.Run("with the code", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
		f.users.EXPECT().FindByEmail(gomock.Any(), "nova@agora.ao").Return(nil, nil)
		f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeConfirmIdentity, user.ID.String(), "123456").Return(nil)
		f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

		out, err := f.profile.UpdateProfile(context.Background(), user.ID,
			dtos.UpdateProfileInput{Name: ptr("Ana Domingos"), Email: ptr("nova@agora.ao"), Code: "123456"})

		require.NoError(t, err)
		assert.Equal(t, "nova@agora.ao", out.Email)
	})

	t.Run("without confirmation", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

		_, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Email: ptr("nova@agora.ao")})

		require.ErrorIs(t, err, usecases.ErrConfirmationRequired)
	})

	t.Run("wrong code keeps the e-mail", func(t *testing.T) {
		f := newFixture(t)
		user := newSignUpUser(t, enums.RoleCustomer)
		f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
		f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeConfirmIdentity, user.ID.String(), "000000").Return(services.ErrOTPInvalid)

		_, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Email: ptr("nova@agora.ao"), Code: "000000"})

		require.ErrorIs(t, err, usecases.ErrInvalidOTPCode)
	})
}

func TestUpdateProfile_StaffCannotSwapThePasswordForACode(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleStaff, true)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	_, err := f.profile.UpdateProfile(context.Background(), user.ID, dtos.UpdateProfileInput{Email: ptr("nova@agora.ao"), Code: "123456"})

	require.ErrorIs(t, err, usecases.ErrCurrentPasswordRequired)
}

func TestDeleteAccount_AccountWithoutPasswordConfirmsWithACode(t *testing.T) {
	f := newFixture(t)
	user := newSignUpUser(t, enums.RoleCustomer)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.otp.EXPECT().Verify(gomock.Any(), enums.OTPPurposeConfirmIdentity, user.ID.String(), "123456").Return(nil)
	f.users.EXPECT().SoftDelete(gomock.Any(), user.ID, gomock.Any()).Return(nil)
	f.expectAllSessionsRevoked(t, user.ID)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)

	require.NoError(t, f.profile.DeleteAccount(context.Background(), user.ID, dtos.DeleteAccountInput{Code: "123456"}))
}
