package authjwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	Role      enums.Role `json:"role"`
	SessionID uuid.UUID  `json:"sid"`
	ClientID  string     `json:"cid"`
	jwt.RegisteredClaims
}

func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

type Service struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

var _ services.IAccessTokenIssuer = (*Service)(nil)

func NewService(secret, issuer string, ttl time.Duration) *Service {
	return &Service{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

func (s *Service) Generate(access services.AccessTokenClaims) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(s.ttl)
	claims := Claims{
		Role:      access.Role,
		SessionID: access.SessionID,
		ClientID:  access.ClientID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   access.UserID.String(),
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
	if claims.ClientID == "" || claims.SessionID == uuid.Nil {
		return nil, fmt.Errorf("%w: missing client or session", ErrInvalidToken)
	}
	return claims, nil
}
