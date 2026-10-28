package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	repomocks "github.com/Agora-Mobility/atlas/domain/repository/mocks"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const secret = "test-secret-that-is-at-least-32-characters"

var (
	customerApp = &entities.Client{ID: uuid.New(), ClientID: "agora-customer-app", Type: enums.ClientTypeCustomerApp, IsActive: true}
	backoffice  = &entities.Client{ID: uuid.New(), ClientID: "agora-backoffice", Type: enums.ClientTypeBackoffice, IsActive: true}
)

func newEngine(t *testing.T) (*gin.Engine, *authjwt.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	clients := repomocks.NewMockIClientRepository(gomock.NewController(t))
	clients.EXPECT().FindActiveByClientID(gomock.Any(), gomock.Any()).AnyTimes().
		DoAndReturn(func(_ any, clientID string) (*entities.Client, error) {
			for _, c := range []*entities.Client{customerApp, backoffice} {
				if c.ClientID == clientID {
					return c, nil
				}
			}
			return nil, nil
		})

	tokens := authjwt.NewService(secret, "atlas", time.Minute)
	authenticator := middleware.NewAuthenticator(tokens)

	r := gin.New()
	r.GET("/me",
		middleware.NewClientAuthorizer(clients).RequireClient(),
		authenticator.RequireAuth(),
		authenticator.RequireRole(enums.RoleCustomer),
		func(ctx *gin.Context) {
			userID, _ := middleware.UserIDFromContext(ctx.Request.Context())
			ctx.String(http.StatusOK, userID.String())
		})
	return r, tokens
}

func accessToken(t *testing.T, tokens *authjwt.Service, role enums.Role, client *entities.Client) (string, uuid.UUID) {
	t.Helper()
	userID := uuid.New()
	token, _, err := tokens.Generate(services.AccessTokenClaims{UserID: userID, SessionID: uuid.New(), Role: role, ClientID: client.ClientID})
	require.NoError(t, err)
	return token, userID
}

func call(r *gin.Engine, clientID, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/me", http.NoBody)
	if clientID != "" {
		req.Header.Set(middleware.HeaderClientID, clientID)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body shared.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code
}

func TestAuthenticatedRequestWithMatchingClient(t *testing.T) {
	r, tokens := newEngine(t)
	token, userID := accessToken(t, tokens, enums.RoleCustomer, customerApp)

	rec := call(r, customerApp.ClientID, token)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, userID.String(), rec.Body.String())
}

func TestRequestsAreRejected(t *testing.T) {
	r, tokens := newEngine(t)
	customerToken, _ := accessToken(t, tokens, enums.RoleCustomer, customerApp)
	adminToken, _ := accessToken(t, tokens, enums.RoleAdmin, backoffice)

	cases := []struct {
		name     string
		clientID string
		token    string
		status   int
		code     string
	}{
		{"missing client header", "", customerToken, http.StatusBadRequest, "MISSING_CLIENT"},
		{"unknown client", "someone-else", customerToken, http.StatusUnauthorized, "UNKNOWN_CLIENT"},
		{"missing token", customerApp.ClientID, "", http.StatusUnauthorized, "UNAUTHORIZED"},
		{"invalid token", customerApp.ClientID, "garbage", http.StatusUnauthorized, "INVALID_TOKEN"},
		{"token issued for another client", backoffice.ClientID, customerToken, http.StatusForbidden, "TOKEN_CLIENT_MISMATCH"},
		{"staff token replayed in the customer app", customerApp.ClientID, adminToken, http.StatusForbidden, "TOKEN_CLIENT_MISMATCH"},
		{"role not allowed on the route", backoffice.ClientID, adminToken, http.StatusForbidden, "INSUFFICIENT_ROLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(r, tc.clientID, tc.token)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.code, errorCode(t, rec))
		})
	}
}
