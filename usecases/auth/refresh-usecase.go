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
	ErrInvalidRefreshToken   = errors.New("invalid refresh token")
	ErrRefreshClientMismatch = errors.New("refresh token bound to another client")
	ErrRefreshTokenRevoked   = errors.New("refresh token revoked")
	ErrRefreshTokenExpired   = errors.New("refresh token expired")
	ErrRefreshTokenReused    = errors.New("refresh token reuse detected")
)

type RefreshUsecase struct {
	refreshTokenRepo repository.IRefreshTokenRepository
	userRepo         repository.IUserRepository
	txManager        repository.ITransactionManager
	opaqueTokens     services.IOpaqueTokenGenerator
	sessions         *SessionIssuer
	refreshTTL       time.Duration
}

func NewRefreshUsecase(
	refreshTokenRepo repository.IRefreshTokenRepository,
	userRepo repository.IUserRepository,
	txManager repository.ITransactionManager,
	opaqueTokens services.IOpaqueTokenGenerator,
	sessions *SessionIssuer,
	refreshTTL time.Duration,
) *RefreshUsecase {
	return &RefreshUsecase{
		refreshTokenRepo: refreshTokenRepo,
		userRepo:         userRepo,
		txManager:        txManager,
		opaqueTokens:     opaqueTokens,
		sessions:         sessions,
		refreshTTL:       refreshTTL,
	}
}

type rotation struct {
	user            *entities.User
	next            *entities.RefreshToken
	rawRefreshToken string
	rejection       error
}

func (uc *RefreshUsecase) Execute(ctx context.Context, req dtos.RefreshRequest, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	var result rotation
	err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		result, err = uc.rotate(ctx, req.RefreshToken, client, sessionMetadata(ip, userAgent))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("refresh session: %w", err)
	}
	if result.rejection != nil {
		return nil, result.rejection
	}

	return uc.sessions.Output(result.user, client, result.rawRefreshToken, result.next)
}

func (uc *RefreshUsecase) rotate(ctx context.Context, rawRefreshToken string, client *entities.Client, meta entities.SessionMetadata) (rotation, error) {
	current, err := uc.refreshTokenRepo.FindByHashForUpdate(ctx, uc.opaqueTokens.Hash(rawRefreshToken))
	if err != nil {
		return rotation{}, err
	}
	if current == nil {
		return rotation{rejection: ErrInvalidRefreshToken}, nil
	}
	if !current.BelongsTo(client) {
		return rotation{rejection: ErrRefreshClientMismatch}, nil
	}
	if current.ReplacedByID != nil {
		return rotation{rejection: ErrRefreshTokenReused}, uc.revokeFamily(ctx, current)
	}
	if current.IsRevoked() {
		return rotation{rejection: ErrRefreshTokenRevoked}, nil
	}
	if current.IsExpired(time.Now()) {
		return rotation{rejection: ErrRefreshTokenExpired}, nil
	}

	user, err := uc.userRepo.FindByID(ctx, current.UserID)
	if err != nil {
		return rotation{}, err
	}
	if user == nil {
		return rotation{rejection: ErrInvalidRefreshToken}, uc.revokeFamily(ctx, current)
	}
	if err := ensureCanSignIn(user, client); err != nil {
		return rotation{rejection: err}, uc.revokeFamily(ctx, current)
	}

	rawNext, nextHash, err := uc.opaqueTokens.Generate()
	if err != nil {
		return rotation{}, fmt.Errorf("%w: %w", ErrTokenSigning, err)
	}
	next, err := current.Rotate(nextHash, uc.refreshTTL, meta)
	if err != nil {
		return rotation{}, err
	}
	if err := uc.refreshTokenRepo.Create(ctx, next); err != nil {
		return rotation{}, err
	}
	if err := uc.refreshTokenRepo.MarkRotated(ctx, current); err != nil {
		return rotation{}, err
	}

	return rotation{user: user, next: next, rawRefreshToken: rawNext}, nil
}

func (uc *RefreshUsecase) revokeFamily(ctx context.Context, token *entities.RefreshToken) error {
	return uc.refreshTokenRepo.RevokeFamily(ctx, token.FamilyID, time.Now().UTC())
}
