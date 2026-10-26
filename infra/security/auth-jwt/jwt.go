// Package authjwt issues and verifies HS256 access tokens and provides the
// Gin middlewares that enforce authentication and roles.
package authjwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	Role enums.Role `json:"role"`
	jwt.RegisteredClaims
}

// UserID returns the subject as a UUID.
func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

type Service struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewService(secret, issuer string, ttl time.Duration) *Service {
	return &Service{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

// Generate issues an access token for userID with role.
func (s *Service) Generate(userID uuid.UUID, role enums.Role) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(s.ttl)
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   userID.String(),
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return token, expires, nil
}

// Verify parses the token, enforcing HS256, issuer and expiry.
func (s *Service) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !claims.Role.IsValid() {
		return nil, fmt.Errorf("%w: unknown role %q", ErrInvalidToken, claims.Role)
	}
	return claims, nil
}
