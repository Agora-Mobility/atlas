package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var (
	ErrOTPInvalid     = errors.New("otp code is invalid")
	ErrOTPExpired     = errors.New("otp code expired or was not requested")
	ErrOTPMaxAttempts = errors.New("otp max attempts exceeded")
	ErrOTPCooldown    = errors.New("otp requested too recently")
)

type AccessTokenClaims struct {
	UserID         uuid.UUID
	SessionID      uuid.UUID
	Role           enums.Role
	ClientID       string
	MembershipID   *uuid.UUID
	OrganizationID *uuid.UUID
	Scope          enums.OrganizationType
	PermVersion    int64
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

// IOTPService issues one-time codes. The subject identifies who may use the
// code: usually the phone number, or user id plus phone number when a signed-in
// user proves a new number.
type IOTPService interface {
	Issue(ctx context.Context, purpose enums.OTPPurpose, subject string) (code string, err error)
	Verify(ctx context.Context, purpose enums.OTPPurpose, subject, code string) error
}

// ISessionRevocationStore denies the access tokens of revoked sessions until
// they expire, so a logout, block or password change takes effect at once.
type ISessionRevocationStore interface {
	RevokeSessions(ctx context.Context, sessionIDs ...uuid.UUID) error
	IsSessionRevoked(ctx context.Context, sessionID uuid.UUID) (bool, error)
}

type IAccessCache interface {
	Get(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error)
	Set(ctx context.Context, access *entities.Access) error
	Delete(ctx context.Context, membershipIDs ...uuid.UUID) error
}

type IAccessResolver interface {
	Resolve(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error)
}

type ITOTP interface {
	NewSecret() (string, error)
	URI(secret, account string) string
	Match(secret, code string, at time.Time) (step int64, ok bool)
}

type ISecretSealer interface {
	Seal(plaintext string) (string, error)
	Open(sealed string) (string, error)
}

var ErrMFAChallengeNotFound = errors.New("mfa challenge not found or expired")

type MFAChallenge struct {
	UserID       uuid.UUID `json:"user_id"`
	ClientID     uuid.UUID `json:"client_id"`
	MembershipID uuid.UUID `json:"membership_id"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type IMFAChallengeStore interface {
	Create(ctx context.Context, challenge MFAChallenge) (token string, err error)
	Find(ctx context.Context, token string) (*MFAChallenge, error)
	RecordFailure(ctx context.Context, token string) (exhausted bool, err error)
	Delete(ctx context.Context, token string) error
}
