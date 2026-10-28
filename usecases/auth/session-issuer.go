package usecases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrSessionCreate = errors.New("failed to create session")
	ErrTokenSigning  = errors.New("failed to sign token")
)

const bearerTokenType = "Bearer"

type SessionIssuer struct {
	userRepo         repository.IUserRepository
	refreshTokenRepo repository.IRefreshTokenRepository
	txManager        repository.ITransactionManager
	accessTokens     services.IAccessTokenIssuer
	opaqueTokens     services.IOpaqueTokenGenerator
	refreshTTL       time.Duration
}

func NewSessionIssuer(
	userRepo repository.IUserRepository,
	refreshTokenRepo repository.IRefreshTokenRepository,
	txManager repository.ITransactionManager,
	accessTokens services.IAccessTokenIssuer,
	opaqueTokens services.IOpaqueTokenGenerator,
	refreshTTL time.Duration,
) *SessionIssuer {
	return &SessionIssuer{
		userRepo:         userRepo,
		refreshTokenRepo: refreshTokenRepo,
		txManager:        txManager,
		accessTokens:     accessTokens,
		opaqueTokens:     opaqueTokens,
		refreshTTL:       refreshTTL,
	}
}

func (s *SessionIssuer) Start(ctx context.Context, user *entities.User, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	rawRefreshToken, refreshTokenHash, err := s.opaqueTokens.Generate()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenSigning, err)
	}
	refreshToken, err := entities.StartRefreshTokenFamily(user.ID, client.ID, refreshTokenHash, s.refreshTTL, sessionMetadata(ip, userAgent))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSessionCreate, err)
	}
	user.RecordLogin()

	err = s.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.refreshTokenRepo.Create(ctx, refreshToken); err != nil {
			return err
		}
		return s.userRepo.UpdateLastLogin(ctx, user.ID, *user.LastLoginAt)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSessionCreate, err)
	}

	return s.Output(user, client, rawRefreshToken, refreshToken)
}

func (s *SessionIssuer) Output(user *entities.User, client *entities.Client, rawRefreshToken string, refreshToken *entities.RefreshToken) (*dtos.SessionOutput, error) {
	accessToken, expiresAt, err := s.accessTokens.Generate(services.AccessTokenClaims{
		UserID:    user.ID,
		SessionID: refreshToken.FamilyID,
		Role:      user.Role,
		ClientID:  client.ClientID,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenSigning, err)
	}

	return &dtos.SessionOutput{
		AccessToken:      accessToken,
		TokenType:        bearerTokenType,
		ExpiresIn:        int64(time.Until(expiresAt).Round(time.Second).Seconds()),
		RefreshToken:     rawRefreshToken,
		RefreshExpiresAt: refreshToken.ExpiresAt,
		Client:           client.ClientID,
		User:             dtos.ToUserView(user),
	}, nil
}

func sessionMetadata(ip, userAgent string) entities.SessionMetadata {
	return entities.SessionMetadata{IPAddress: ip, UserAgent: userAgent}
}
