package usecases_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	repomocks "github.com/Agora-Mobility/atlas/domain/repository/mocks"
	"github.com/Agora-Mobility/atlas/domain/services"
	servicemocks "github.com/Agora-Mobility/atlas/domain/services/mocks"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

const (
	storedHash  = "$argon2id$stored"
	phone       = "+244923000000"
	strongPass  = "S3gredo!forte"
	refreshTTL  = 30 * 24 * time.Hour
	rawRefresh  = "raw-refresh"
	hashRefresh = "hash-refresh"
	otpTTL      = 5 * time.Minute
	otpCooldown = 60 * time.Second
	accessTTL   = 15 * time.Minute
)

var (
	customerApp  = &entities.Client{ID: uuid.New(), ClientID: "agora-customer-app", Type: enums.ClientTypeCustomerApp, IsActive: true}
	providerApp  = &entities.Client{ID: uuid.New(), ClientID: "agora-provider-app", Type: enums.ClientTypeProviderApp, IsActive: true}
	partnerPanel = &entities.Client{ID: uuid.New(), ClientID: "agora-partner-panel", Type: enums.ClientTypePartnerPanel, IsActive: true}
	backoffice   = &entities.Client{ID: uuid.New(), ClientID: "agora-backoffice", Type: enums.ClientTypeBackoffice, IsActive: true}
)

type fixture struct {
	users          *repomocks.MockIUserRepository
	refreshTokens  *repomocks.MockIRefreshTokenRepository
	outbox         *repomocks.MockIOutboxRepository
	tx             *repomocks.MockITransactionManager
	hasher         *servicemocks.MockIPasswordHasher
	accessTokens   *servicemocks.MockIAccessTokenIssuer
	opaqueTokens   *servicemocks.MockIOpaqueTokenGenerator
	otp            *servicemocks.MockIOTPService
	recoveryTicket *servicemocks.MockIRecoveryTicketService
	sms            *servicemocks.MockISMSSender
	email          *servicemocks.MockIEmailSender
	revocations    *servicemocks.MockISessionRevocationStore
	audit          *repomocks.MockIAuditLogRepository
	blockReasons   *repomocks.MockIBlockReasonRepository
	orgs           *repomocks.MockIOrganizationRepository
	roles          *repomocks.MockIRoleRepository
	memberships    *repomocks.MockIMembershipRepository
	invitations    *repomocks.MockIInvitationRepository
	accessCache    *servicemocks.MockIAccessCache

	auth              *usecases.AuthUsecase
	refresh           *usecases.RefreshUsecase
	phoneVerification *usecases.PhoneVerificationUsecase
	recovery          *usecases.RecoveryUsecase
	profile           *usecases.ProfileUsecase
	userAdmin         *usecases.UserAdminUsecase
	bootstrap         *usecases.AdminBootstrap
	staff             *usecases.StaffSignIn
	members           *usecases.MemberUsecase
	invitationFlow    *usecases.InvitationUsecase
	organizations     *usecases.OrganizationUsecase
	roleAdmin         *usecases.RoleUsecase
}

var invitationLinks = usecases.InvitationLinks{Platform: "https://argus.agora.ao/convite", Partner: "https://athena.agora.ao/convite"}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		users:          repomocks.NewMockIUserRepository(ctrl),
		refreshTokens:  repomocks.NewMockIRefreshTokenRepository(ctrl),
		outbox:         repomocks.NewMockIOutboxRepository(ctrl),
		tx:             repomocks.NewMockITransactionManager(ctrl),
		hasher:         servicemocks.NewMockIPasswordHasher(ctrl),
		accessTokens:   servicemocks.NewMockIAccessTokenIssuer(ctrl),
		opaqueTokens:   servicemocks.NewMockIOpaqueTokenGenerator(ctrl),
		otp:            servicemocks.NewMockIOTPService(ctrl),
		recoveryTicket: servicemocks.NewMockIRecoveryTicketService(ctrl),
		sms:            servicemocks.NewMockISMSSender(ctrl),
		email:          servicemocks.NewMockIEmailSender(ctrl),
		revocations:    servicemocks.NewMockISessionRevocationStore(ctrl),
		audit:          repomocks.NewMockIAuditLogRepository(ctrl),
		blockReasons:   repomocks.NewMockIBlockReasonRepository(ctrl),
		orgs:           repomocks.NewMockIOrganizationRepository(ctrl),
		roles:          repomocks.NewMockIRoleRepository(ctrl),
		memberships:    repomocks.NewMockIMembershipRepository(ctrl),
		invitations:    repomocks.NewMockIInvitationRepository(ctrl),
		accessCache:    servicemocks.NewMockIAccessCache(ctrl),
	}
	f.tx.EXPECT().WithinTransaction(gomock.Any(), gomock.Any()).AnyTimes().
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })

	codeSender := usecases.NewVerificationCodeSender(f.otp, f.sms, otpTTL)
	sessions := usecases.NewSessionIssuer(f.users, f.refreshTokens, f.tx, f.accessTokens, f.opaqueTokens, refreshTTL)
	revoker := usecases.NewSessionRevoker(f.refreshTokens, f.revocations, accessTTL)
	access := usecases.NewAccessResolver(f.memberships, f.accessCache)
	f.staff = usecases.NewStaffSignIn(f.users, f.memberships, sessions)
	f.auth = usecases.NewAuthUsecase(f.users, f.outbox, f.tx, f.hasher, codeSender, sessions, f.staff, revoker)
	f.refresh = usecases.NewRefreshUsecase(f.refreshTokens, f.users, access, f.tx, f.opaqueTokens, sessions, revoker, refreshTTL)
	f.phoneVerification = usecases.NewPhoneVerificationUsecase(f.users, f.outbox, f.tx, f.otp, codeSender, sessions)
	f.recovery = usecases.NewRecoveryUsecase(f.users, f.outbox, f.tx, f.hasher, f.otp, codeSender, revoker, f.recoveryTicket, f.email, otpCooldown)
	f.profile = usecases.NewProfileUsecase(f.users, f.outbox, f.tx, f.hasher, f.otp, codeSender, revoker)
	f.userAdmin = usecases.NewUserAdminUsecase(f.users, f.outbox, f.audit, f.blockReasons, f.tx, f.hasher, revoker)
	f.bootstrap = usecases.NewAdminBootstrap(f.users, f.orgs, f.roles, f.memberships, f.outbox, f.audit, f.tx, f.hasher)
	f.members = usecases.NewMemberUsecase(usecases.MemberStores{
		Users: f.users, Orgs: f.orgs, Roles: f.roles, Memberships: f.memberships, Invitations: f.invitations,
		Audit: f.audit, Outbox: f.outbox, Tx: f.tx,
	}, f.opaqueTokens, f.sms, f.email, revoker, access, invitationLinks)
	f.invitationFlow = usecases.NewInvitationUsecase(f.users, f.orgs, f.memberships, f.invitations, f.audit, f.outbox, f.tx,
		f.hasher, f.opaqueTokens, f.otp, codeSender, access)
	f.organizations = usecases.NewOrganizationUsecase(f.orgs, f.roles, f.memberships, f.audit, f.outbox, f.tx)
	f.roleAdmin = usecases.NewRoleUsecase(f.roles, f.audit, f.outbox, f.tx, access)
	return f
}

// expectSessionRevoked expects one session to end and its access tokens to be denied.
func (f fixture) expectSessionRevoked(sessionID uuid.UUID) {
	f.refreshTokens.EXPECT().RevokeFamily(gomock.Any(), sessionID, gomock.Any()).Return(nil)
	f.revocations.EXPECT().RevokeSessions(gomock.Any(), sessionID).Return(nil)
}

// expectAllSessionsRevoked expects every session of the user to end and the
// access tokens of recent sessions to be denied. It returns those sessions.
func (f fixture) expectAllSessionsRevoked(t *testing.T, userID uuid.UUID) []uuid.UUID {
	t.Helper()
	recent := []uuid.UUID{uuid.New(), uuid.New()}
	f.refreshTokens.EXPECT().RevokeAllForUser(gomock.Any(), userID, gomock.Any()).Return(nil)
	f.refreshTokens.EXPECT().ListSessionsIssuedSince(gomock.Any(), userID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, since time.Time) ([]uuid.UUID, error) {
			require.WithinDuration(t, time.Now().Add(-accessTTL), since, 2*time.Minute)
			require.True(t, since.Before(time.Now().Add(-accessTTL)), "window must cover every live access token")
			return recent, nil
		})
	f.revocations.EXPECT().RevokeSessions(gomock.Any(), recent[0], recent[1]).Return(nil)
	return recent
}

func (f fixture) expectSessionStarted(t *testing.T, user *entities.User, client *entities.Client) {
	t.Helper()
	f.expectStaffSessionStarted(t, user, client, nil)
}

func (f fixture) expectStaffSessionStarted(t *testing.T, user *entities.User, client *entities.Client, access *entities.Access) {
	t.Helper()
	var family uuid.UUID
	f.opaqueTokens.EXPECT().Generate().Return(rawRefresh, hashRefresh, nil)
	f.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, token *entities.RefreshToken) error {
			require.Equal(t, user.ID, token.UserID)
			require.Equal(t, client.ID, token.ClientID)
			require.Equal(t, hashRefresh, token.TokenHash)
			if access == nil {
				require.Nil(t, token.MembershipID)
			} else {
				require.Equal(t, access.MembershipID, *token.MembershipID)
			}
			family = token.FamilyID
			return nil
		})
	f.users.EXPECT().UpdateLastLogin(gomock.Any(), user.ID, gomock.Any()).Return(nil)
	f.expectAccessTokenFor(t, user, client, &family, access)
}

func (f fixture) expectAccessToken(t *testing.T, user *entities.User, client *entities.Client, family *uuid.UUID) {
	t.Helper()
	f.expectAccessTokenFor(t, user, client, family, nil)
}

func (f fixture) expectAccessTokenFor(t *testing.T, user *entities.User, client *entities.Client, family *uuid.UUID, access *entities.Access) {
	t.Helper()
	f.accessTokens.EXPECT().Generate(gomock.Any()).
		DoAndReturn(func(claims services.AccessTokenClaims) (string, time.Time, error) {
			require.Equal(t, user.ID, claims.UserID)
			require.Equal(t, user.Role, claims.Role)
			require.Equal(t, client.ClientID, claims.ClientID)
			require.Equal(t, *family, claims.SessionID)
			if access == nil {
				require.Nil(t, claims.MembershipID)
			} else {
				require.Equal(t, access.MembershipID, *claims.MembershipID)
				require.Equal(t, access.OrganizationID, *claims.OrganizationID)
				require.Equal(t, access.Scope, claims.Scope)
				require.Equal(t, access.PermVersion, claims.PermVersion)
			}
			return "access", time.Now().Add(15 * time.Minute), nil
		})
}

func newUser(t *testing.T, role enums.Role, verified bool) *entities.User {
	t.Helper()
	user, err := entities.NewUser("Ana", "ana@agora.ao", phone, storedHash, role)
	require.NoError(t, err)
	if verified {
		user.MarkVerified()
	}
	return user
}
