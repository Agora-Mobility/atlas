package authjwt_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const secret = "test-secret-that-is-at-least-32-characters"

func accessClaims(role enums.Role) services.AccessTokenClaims {
	return services.AccessTokenClaims{UserID: uuid.New(), SessionID: uuid.New(), Role: role, ClientID: "agora-backoffice"}
}

func TestGenerateAndVerify(t *testing.T) {
	svc := authjwt.NewService(secret, "atlas", time.Minute)
	access := accessClaims(enums.RoleAdmin)
	userID := access.UserID

	token, _, err := svc.Generate(access)
	require.NoError(t, err)

	claims, err := svc.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, enums.RoleAdmin, claims.Role)
	assert.Equal(t, access.SessionID, claims.SessionID)
	assert.Equal(t, "agora-backoffice", claims.ClientID)
	got, err := claims.UserID()
	require.NoError(t, err)
	assert.Equal(t, userID, got)
}

func TestVerify_Rejects(t *testing.T) {
	svc := authjwt.NewService(secret, "atlas", time.Minute)
	valid, _, err := svc.Generate(accessClaims(enums.RoleCustomer))
	require.NoError(t, err)

	expired, _, err := authjwt.NewService(secret, "atlas", -time.Minute).Generate(accessClaims(enums.RoleCustomer))
	require.NoError(t, err)

	otherIssuer, _, err := authjwt.NewService(secret, "someone-else", time.Minute).Generate(accessClaims(enums.RoleCustomer))
	require.NoError(t, err)

	otherSecret, _, err := authjwt.NewService(secret+"x", "atlas", time.Minute).Generate(accessClaims(enums.RoleCustomer))
	require.NoError(t, err)

	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": uuid.NewString(), "iss": "atlas", "role": "admin"}).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	withoutClient := accessClaims(enums.RoleCustomer)
	withoutClient.ClientID = ""
	noClient, _, err := svc.Generate(withoutClient)
	require.NoError(t, err)

	cases := map[string]string{
		"without client": noClient,
		"expired":        expired,
		"wrong issuer":   otherIssuer,
		"wrong secret":   otherSecret,
		"alg none":       none,
		"tampered":       valid + "x",
		"garbage":        "not-a-token",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Verify(token)
			require.ErrorIs(t, err, authjwt.ErrInvalidToken)
		})
	}
}
