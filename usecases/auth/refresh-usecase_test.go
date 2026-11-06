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
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

const rawCurrent = "raw-current"

func activeToken(t *testing.T, user *entities.User, client *entities.Client) *entities.RefreshToken {
	t.Helper()
	token, err := entities.StartRefreshTokenFamily(user.ID, client.ID, nil, "hash-current", refreshTTL, entities.SessionMetadata{})
	require.NoError(t, err)
	return token
}

func (f fixture) expectLookup(token *entities.RefreshToken) {
	f.opaqueTokens.EXPECT().Hash(rawCurrent).Return("hash-current")
	f.refreshTokens.EXPECT().FindByHashForUpdate(gomock.Any(), "hash-current").Return(token, nil)
}

func refreshRequest() dtos.RefreshRequest {
	return dtos.RefreshRequest{RefreshToken: rawCurrent}
}

func TestRefresh_RotatesTokenWithinFamily(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	current := activeToken(t, user, customerApp)
	f.expectLookup(current)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.opaqueTokens.EXPECT().Generate().Return("raw-next", "hash-next", nil)

	var next *entities.RefreshToken
	createNext := f.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, token *entities.RefreshToken) error { next = token; return nil })
	f.refreshTokens.EXPECT().MarkRotated(gomock.Any(), current).Return(nil).After(createNext)
	f.expectAccessToken(t, user, customerApp, &current.FamilyID)

	out, err := f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.NoError(t, err)
	assert.Equal(t, "raw-next", out.RefreshToken)
	assert.Equal(t, current.FamilyID, next.FamilyID)
	assert.Equal(t, customerApp.ID, next.ClientID)
	assert.True(t, current.IsRevoked())
	assert.Equal(t, next.ID, *current.ReplacedByID)
}

func TestRefresh_TokenFromAnotherClientIsRejected(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	f.expectLookup(activeToken(t, user, customerApp))

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), providerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrRefreshClientMismatch)
}

func TestRefresh_ReusedTokenRevokesWholeFamily(t *testing.T) {
	f := newFixture(t)
	current := activeToken(t, newUser(t, enums.RoleCustomer, true), customerApp)
	_, err := current.Rotate("hash-other", refreshTTL, entities.SessionMetadata{})
	require.NoError(t, err)
	f.expectLookup(current)
	f.expectSessionRevoked(current.FamilyID)

	_, err = f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrRefreshTokenReused)
}

func TestRefresh_LoggedOutTokenIsRevoked(t *testing.T) {
	f := newFixture(t)
	current := activeToken(t, newUser(t, enums.RoleCustomer, true), customerApp)
	revokedAt := time.Now().UTC()
	current.RevokedAt = &revokedAt
	f.expectLookup(current)

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrRefreshTokenRevoked)
}

func TestRefresh_ExpiredToken(t *testing.T) {
	f := newFixture(t)
	current := activeToken(t, newUser(t, enums.RoleCustomer, true), customerApp)
	current.ExpiresAt = time.Now().Add(-time.Minute)
	f.expectLookup(current)

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrRefreshTokenExpired)
}

func TestRefresh_BlockedUserLosesSession(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, enums.RoleCustomer, true)
	user.Block()
	current := activeToken(t, user, customerApp)
	f.expectLookup(current)
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.expectSessionRevoked(current.FamilyID)

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrAccountBlocked)
}

func TestRefresh_UnknownToken(t *testing.T) {
	f := newFixture(t)
	f.expectLookup(nil)

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), customerApp, "", "")

	require.ErrorIs(t, err, usecases.ErrInvalidRefreshToken)
}

func staffToken(t *testing.T, user *entities.User, client *entities.Client, membershipID uuid.UUID) *entities.RefreshToken {
	t.Helper()
	token, err := entities.StartRefreshTokenFamily(user.ID, client.ID, &membershipID, "hash-current", refreshTTL, entities.SessionMetadata{})
	require.NoError(t, err)
	return token
}

func TestRefresh_StaffSessionKeepsTheMembershipAndItsLatestPermissions(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	_, access := staffAccess(staff, enums.OrganizationTypePlatform)
	access.PermVersion = 9
	current := staffToken(t, staff, backoffice, access.MembershipID)
	f.expectLookup(current)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
	f.memberships.EXPECT().Access(gomock.Any(), access.MembershipID).Return(access, nil)
	f.opaqueTokens.EXPECT().Generate().Return("raw-next", "hash-next", nil)
	f.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, token *entities.RefreshToken) error {
			assert.Equal(t, access.MembershipID, *token.MembershipID)
			return nil
		})
	f.refreshTokens.EXPECT().MarkRotated(gomock.Any(), current).Return(nil)
	f.expectAccessTokenFor(t, staff, backoffice, &current.FamilyID, access)

	out, err := f.refresh.Execute(context.Background(), refreshRequest(), backoffice, "", "")

	require.NoError(t, err)
	assert.Equal(t, int64(9), access.PermVersion)
	assert.Equal(t, access.MembershipID, out.Access.MembershipID)
}

func TestRefresh_SuspendedMembershipEndsTheSession(t *testing.T) {
	f := newFixture(t)
	staff := newUser(t, enums.RoleStaff, true)
	_, access := staffAccess(staff, enums.OrganizationTypePlatform)
	access.Status = enums.MembershipStatusSuspended
	current := staffToken(t, staff, backoffice, access.MembershipID)
	f.expectLookup(current)
	f.users.EXPECT().FindByID(gomock.Any(), staff.ID).Return(staff, nil)
	f.memberships.EXPECT().Access(gomock.Any(), access.MembershipID).Return(access, nil)
	f.expectSessionRevoked(current.FamilyID)

	_, err := f.refresh.Execute(context.Background(), refreshRequest(), backoffice, "", "")

	require.ErrorIs(t, err, usecases.ErrRefreshTokenRevoked)
}
