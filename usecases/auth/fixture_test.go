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
)

var (
	customerApp  = &entities.Client{ID: uuid.New(), ClientID: "agora-customer-app", Type: enums.ClientTypeCustomerApp, IsActive: true}
	providerApp  = &entities.Client{ID: uuid.New(), ClientID: "agora-provider-app", Type: enums.ClientTypeProviderApp, IsActive: true}
	partnerPanel = &entities.Client{ID: uuid.New(), ClientID: "agora-partner-panel", Type: enums.ClientTypePartnerPanel, IsActive: true}
	backoffice   = &entities.Client{ID: uuid.New(), ClientID: "agora-backoffice", Type: enums.ClientTypeBackoffice, IsActive: true}
)

type fixture struct {
	users         *repomocks.MockIUserRepository
	refreshTokens *repomocks.MockIRefreshTokenRepository
	outbox        *repomocks.MockIOutboxRepository
	tx            *repomocks.MockITransactionManager
	hasher        *servicemocks.MockIPasswordHasher
	accessTokens  *servicemocks.MockIAccessTokenIssuer
	opaqueTokens  *servicemocks.MockIOpaqueTokenGenerator
	otp           *servicemocks.MockIOTPService
	sms           *servicemocks.MockISMSSender

	auth              *usecases.AuthUsecase
	refresh           *usecases.RefreshUsecase
	phoneVerification *usecases.PhoneVerificationUsecase
	recovery          *usecases.RecoveryUsecase
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		users:         repomocks.NewMockIUserRepository(ctrl),
		refreshTokens: repomocks.NewMockIRefreshTokenRepository(ctrl),
		outbox:        repomocks.NewMockIOutboxRepository(ctrl),
		tx:            repomocks.NewMockITransactionManager(ctrl),
		hasher:        servicemocks.NewMockIPasswordHasher(ctrl),
		accessTokens:  servicemocks.NewMockIAccessTokenIssuer(ctrl),
		opaqueTokens:  servicemocks.NewMockIOpaqueTokenGenerator(ctrl),
		otp:           servicemocks.NewMockIOTPService(ctrl),
		sms:           servicemocks.NewMockISMSSender(ctrl),
	}
	f.tx.EXPECT().WithinTransaction(gomock.Any(), gomock.Any()).AnyTimes().
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })

	codeSender := usecases.NewVerificationCodeSender(f.otp, f.sms, otpTTL)
	sessions := usecases.NewSessionIssuer(f.users, f.refreshTokens, f.tx, f.accessTokens, f.opaqueTokens, refreshTTL)
	f.auth = usecases.NewAuthUsecase(f.users, f.refreshTokens, f.outbox, f.tx, f.hasher, codeSender, sessions)
	f.refresh = usecases.NewRefreshUsecase(f.refreshTokens, f.users, f.tx, f.opaqueTokens, sessions, refreshTTL)
	f.phoneVerification = usecases.NewPhoneVerificationUsecase(f.users, f.outbox, f.tx, f.otp, codeSender, sessions)
	f.recovery = usecases.NewRecoveryUsecase(f.users, f.refreshTokens, f.outbox, f.tx, f.hasher, f.otp, codeSender)
	return f
}

func (f fixture) expectSessionStarted(t *testing.T, user *entities.User, client *entities.Client) {
	t.Helper()
	var family uuid.UUID
	f.opaqueTokens.EXPECT().Generate().Return(rawRefresh, hashRefresh, nil)
	f.refreshTokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, token *entities.RefreshToken) error {
			require.Equal(t, user.ID, token.UserID)
			require.Equal(t, client.ID, token.ClientID)
			require.Equal(t, hashRefresh, token.TokenHash)
			family = token.FamilyID
			return nil
		})
	f.users.EXPECT().UpdateLastLogin(gomock.Any(), user.ID, gomock.Any()).Return(nil)
	f.expectAccessToken(t, user, client, &family)
}

func (f fixture) expectAccessToken(t *testing.T, user *entities.User, client *entities.Client, family *uuid.UUID) {
	t.Helper()
	f.accessTokens.EXPECT().Generate(gomock.Any()).
		DoAndReturn(func(claims services.AccessTokenClaims) (string, time.Time, error) {
			require.Equal(t, user.ID, claims.UserID)
			require.Equal(t, user.Role, claims.Role)
			require.Equal(t, client.ClientID, claims.ClientID)
			require.Equal(t, *family, claims.SessionID)
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
