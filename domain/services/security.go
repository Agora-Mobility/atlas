package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

var (
	ErrOTPInvalid     = errors.New("otp code is invalid")
	ErrOTPExpired     = errors.New("otp code expired or was not requested")
	ErrOTPMaxAttempts = errors.New("otp max attempts exceeded")
	ErrOTPCooldown    = errors.New("otp requested too recently")
)

type AccessTokenClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	Role      enums.Role
	ClientID  string
}

type IPasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (matches bool, needsRehash bool, err error)
}

type IAccessTokenIssuer interface {
	Generate(claims AccessTokenClaims) (token string, expiresAt time.Time, err error)
}

type IOpaqueTokenGenerator interface {
	Generate() (token string, tokenHash string, err error)
	Hash(token string) string
}

type IOTPService interface {
	Issue(ctx context.Context, purpose enums.OTPPurpose, phone string) (code string, err error)
	Verify(ctx context.Context, purpose enums.OTPPurpose, phone, code string) error
}
