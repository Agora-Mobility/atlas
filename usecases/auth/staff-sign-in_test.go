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
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func (f fixture) expectChallenge(user *entities.User, membershipID uuid.UUID, client *entities.Client) {
	f.challenges.EXPECT().Find(gomock.Any(), "mfa-token").Return(&services.MFAChallenge{
		UserID: user.ID, ClientID: client.ID, MembershipID: membershipID, IPAddress: "10.0.0.1", UserAgent: "argus",
	}, nil)
}

func TestMFASetup_StoresOnlyTheSealedSecret(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	f.expectChallenge(staff, uuid.New(), backoffice)
	f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(nil, nil)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
	f.totp.EXPECT().NewSecret().Return("JBSWY3DPEHPK3PXP", nil)
	f.sealer.EXPECT().Seal("JBSWY3DPEHPK3PXP").Return("sealed", nil)
	f.mfa.EXPECT().SavePending(gomock.Any(), staff.ID, "sealed", gomock.Any()).Return(nil)
	f.totp.EXPECT().URI("JBSWY3DPEHPK3PXP", "ana@agora.ao").Return("otpauth://totp/AGORA:ana@agora.ao")

	out, err := f.staff.Setup(context.Background(), backoffice, dtos.MFATokenInput{MFAToken: "mfa-token"})

	require.NoError(t, err)
	assert.Equal(t, "JBSWY3DPEHPK3PXP", out.Secret)
}

func TestMFASetup_Rejections(t *testing.T) {
	t.Run("already set up", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{ConfirmedAt: &time.Time{}}, nil)

		_, err := f.staff.Setup(context.Background(), backoffice, dtos.MFATokenInput{MFAToken: "mfa-token"})

		require.ErrorIs(t, err, usecases.ErrMFAAlreadyConfigured)
	})

	t.Run("challenge of another client", func(t *testing.T) {
		f := newFixture(t)
		f.expectChallenge(newUser(t, enums.RoleStaff, true), uuid.New(), partnerPanel)

		_, err := f.staff.Setup(context.Background(), backoffice, dtos.MFATokenInput{MFAToken: "mfa-token"})

		require.ErrorIs(t, err, usecases.ErrInvalidMFAToken)
	})

	t.Run("expired challenge", func(t *testing.T) {
		f := newFixture(t)
		f.challenges.EXPECT().Find(gomock.Any(), "mfa-token").Return(nil, services.ErrMFAChallengeNotFound)

		_, err := f.staff.Setup(context.Background(), backoffice, dtos.MFATokenInput{MFAToken: "mfa-token"})

		require.ErrorIs(t, err, usecases.ErrInvalidMFAToken)
	})
}

func TestMFAVerify_StartsTheSessionOfTheChallengedMembership(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	_, access := staffAccess(staff, enums.OrganizationTypePlatform, true)
	f.expectChallenge(staff, access.MembershipID, backoffice)
	f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{UserID: staff.ID, Secret: "sealed"}, nil)
	f.sealer.EXPECT().Open("sealed").Return("JBSWY3DPEHPK3PXP", nil)
	f.totp.EXPECT().Match("JBSWY3DPEHPK3PXP", "123456", gomock.Any()).Return(int64(42), true)
	f.mfa.EXPECT().UseStep(gomock.Any(), staff.ID, int64(42), gomock.Any()).Return(true, nil)
	f.challenges.EXPECT().Delete(gomock.Any(), "mfa-token").Return(nil)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
	f.memberships.EXPECT().Access(gomock.Any(), access.MembershipID).Return(access, nil)
	f.expectStaffSessionStarted(t, staff, backoffice, access)
	f.opaqueTokens.EXPECT().Hash(gomock.Any()).Times(10).DoAndReturn(func(code string) string { return "hash:" + code })
	f.mfa.EXPECT().ReplaceRecoveryCodes(gomock.Any(), staff.ID, gomock.Len(10), gomock.Any()).Return(nil)

	out, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "123456"})

	require.NoError(t, err)
	assert.Equal(t, access.MembershipID, out.Access.MembershipID)
	require.Len(t, out.MFARecoveryCodes, 10, "the first confirmation hands out the recovery codes once")
	assert.Regexp(t, `^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`, out.MFARecoveryCodes[0])
}

func TestMFAVerify_LaterSignInsGetNoNewRecoveryCodes(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	_, access := staffAccess(staff, enums.OrganizationTypePlatform, true)
	f.expectChallenge(staff, access.MembershipID, backoffice)
	f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{UserID: staff.ID, Secret: "sealed", ConfirmedAt: &time.Time{}}, nil)
	f.sealer.EXPECT().Open("sealed").Return("SECRET", nil)
	f.totp.EXPECT().Match("SECRET", "123456", gomock.Any()).Return(int64(43), true)
	f.mfa.EXPECT().UseStep(gomock.Any(), staff.ID, int64(43), gomock.Any()).Return(true, nil)
	f.challenges.EXPECT().Delete(gomock.Any(), "mfa-token").Return(nil)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
	f.memberships.EXPECT().Access(gomock.Any(), access.MembershipID).Return(access, nil)
	f.expectStaffSessionStarted(t, staff, backoffice, access)

	out, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "123456"})

	require.NoError(t, err)
	assert.Empty(t, out.MFARecoveryCodes)
}

func TestMFARecover(t *testing.T) {
	confirmed := func(user *entities.User) *entities.UserMFA {
		return &entities.UserMFA{UserID: user.ID, Secret: "sealed", ConfirmedAt: &time.Time{}}
	}

	t.Run("a recovery code starts the session once", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		_, access := staffAccess(staff, enums.OrganizationTypePlatform, true)
		f.expectChallenge(staff, access.MembershipID, backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(confirmed(staff), nil)
		f.opaqueTokens.EXPECT().Hash("7KQM2XRTPL9D").Return("hash-code")
		f.mfa.EXPECT().UseRecoveryCode(gomock.Any(), staff.ID, "hash-code", gomock.Any()).Return(true, nil)
		f.mfa.EXPECT().CountUnusedRecoveryCodes(gomock.Any(), staff.ID).Return(int64(9), nil)
		f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
				assert.Equal(t, "mfa.recovery-code-used", e.Action)
				assert.Contains(t, string(e.After), `"codes_left":9`)
				return nil
			})
		f.challenges.EXPECT().Delete(gomock.Any(), "mfa-token").Return(nil)
		f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
		f.memberships.EXPECT().Access(gomock.Any(), access.MembershipID).Return(access, nil)
		f.expectStaffSessionStarted(t, staff, backoffice, access)

		_, err := f.staff.Recover(context.Background(), backoffice, dtos.MFARecoverInput{MFAToken: "mfa-token", RecoveryCode: " 7kqm-2xrt-pl9d "}, "", "")

		require.NoError(t, err)
	})

	t.Run("a used or unknown code counts as a failed attempt", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(confirmed(staff), nil)
		f.opaqueTokens.EXPECT().Hash("AAAABBBBCCCC").Return("hash-used")
		f.mfa.EXPECT().UseRecoveryCode(gomock.Any(), staff.ID, "hash-used", gomock.Any()).Return(false, nil)
		f.challenges.EXPECT().RecordFailure(gomock.Any(), "mfa-token").Return(false, nil)

		_, err := f.staff.Recover(context.Background(), backoffice, dtos.MFARecoverInput{MFAToken: "mfa-token", RecoveryCode: "AAAA-BBBB-CCCC"}, "", "")

		require.ErrorIs(t, err, usecases.ErrInvalidMFACode)
	})
}

func TestRegenerateRecoveryCodes(t *testing.T) {
	t.Run("requires a fresh authenticator code", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{UserID: staff.ID, Secret: "sealed", ConfirmedAt: &time.Time{}}, nil)
		f.sealer.EXPECT().Open("sealed").Return("SECRET", nil)
		f.totp.EXPECT().Match("SECRET", "123456", gomock.Any()).Return(int64(50), true)
		f.mfa.EXPECT().UseStep(gomock.Any(), staff.ID, int64(50), gomock.Any()).Return(true, nil)
		f.opaqueTokens.EXPECT().Hash(gomock.Any()).Times(10).Return("hash")
		f.mfa.EXPECT().ReplaceRecoveryCodes(gomock.Any(), staff.ID, gomock.Len(10), gomock.Any()).Return(nil)
		f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).Return(nil)

		out, err := f.staff.RegenerateRecoveryCodes(context.Background(), staff.ID, dtos.MFACodeInput{Code: "123456"}, "", "")

		require.NoError(t, err)
		assert.Len(t, out.RecoveryCodes, 10)
	})

	t.Run("without mfa there is nothing to regenerate", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(nil, nil)

		_, err := f.staff.RegenerateRecoveryCodes(context.Background(), staff.ID, dtos.MFACodeInput{Code: "123456"}, "", "")

		require.ErrorIs(t, err, usecases.ErrMFANotConfigured)
	})
}

func TestMFAVerify_Rejections(t *testing.T) {
	t.Run("wrong code", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{Secret: "sealed"}, nil)
		f.sealer.EXPECT().Open("sealed").Return("SECRET", nil)
		f.totp.EXPECT().Match("SECRET", "000000", gomock.Any()).Return(int64(0), false)
		f.challenges.EXPECT().RecordFailure(gomock.Any(), "mfa-token").Return(false, nil)

		_, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "000000"})

		require.ErrorIs(t, err, usecases.ErrInvalidMFACode)
	})

	t.Run("too many wrong codes end the challenge", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{Secret: "sealed"}, nil)
		f.sealer.EXPECT().Open("sealed").Return("SECRET", nil)
		f.totp.EXPECT().Match("SECRET", "000000", gomock.Any()).Return(int64(0), false)
		f.challenges.EXPECT().RecordFailure(gomock.Any(), "mfa-token").Return(true, nil)

		_, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "000000"})

		require.ErrorIs(t, err, usecases.ErrInvalidMFAToken)
	})

	t.Run("a code cannot be replayed", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(&entities.UserMFA{Secret: "sealed"}, nil)
		f.sealer.EXPECT().Open("sealed").Return("SECRET", nil)
		f.totp.EXPECT().Match("SECRET", "123456", gomock.Any()).Return(int64(42), true)
		f.mfa.EXPECT().UseStep(gomock.Any(), staff.ID, int64(42), gomock.Any()).Return(false, nil)
		f.challenges.EXPECT().RecordFailure(gomock.Any(), "mfa-token").Return(false, nil)

		_, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "123456"})

		require.ErrorIs(t, err, usecases.ErrInvalidMFACode)
	})

	t.Run("setup required", func(t *testing.T) {
		f := newFixture(t)
		staff := newUser(t, enums.RoleStaff, true)
		f.expectChallenge(staff, uuid.New(), backoffice)
		f.mfa.EXPECT().Find(gomock.Any(), staff.ID).Return(nil, nil)

		_, err := f.staff.Verify(context.Background(), backoffice, dtos.MFAVerifyInput{MFAToken: "mfa-token", Code: "123456"})

		require.ErrorIs(t, err, usecases.ErrMFASetupRequired)
	})
}
