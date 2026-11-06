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

	ErrRecoveryTicketInvalid = errors.New("recovery ticket is invalid or expired")
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

// IRecoveryTicketService trades a verified recovery code for a short-lived,
// single-use ticket. The ticket carries the phone or e-mail that received the
// code, so the second step of the recovery needs no code and no identifier.
// Peek reads the ticket without spending it, so a rejected password can be
// corrected on the same ticket.
type IRecoveryTicketService interface {
	Issue(ctx context.Context, subject string) (ticket string, err error)
	Peek(ctx context.Context, ticket string) (subject string, err error)
	Consume(ctx context.Context, ticket string) (subject string, err error)
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
