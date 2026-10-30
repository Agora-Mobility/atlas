package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
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

// revocations is an in-memory session revocation store.
type revocations struct {
	revoked map[uuid.UUID]bool
	err     error
}

func (r *revocations) RevokeSessions(_ context.Context, ids ...uuid.UUID) error {
	for _, id := range ids {
		r.revoked[id] = true
	}
	return nil
}

func (r *revocations) IsSessionRevoked(_ context.Context, id uuid.UUID) (bool, error) {
	return r.revoked[id], r.err
}

type resolver struct {
	access map[uuid.UUID]*entities.Access
	err    error
}

func (r *resolver) Resolve(_ context.Context, membershipID uuid.UUID) (*entities.Access, error) {
	return r.access[membershipID], r.err
}

func newEngine(t *testing.T) (*gin.Engine, *authjwt.Service) {
	r, tokens, _, _ := newEngineWithStores(t)
	return r, tokens
}

func newEngineWithRevocations(t *testing.T) (*gin.Engine, *authjwt.Service, *revocations) {
	r, tokens, store, _ := newEngineWithStores(t)
	return r, tokens, store
}

func newEngineWithStores(t *testing.T) (*gin.Engine, *authjwt.Service, *revocations, *resolver) {
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
	store := &revocations{revoked: map[uuid.UUID]bool{}}
	authenticator := middleware.NewAuthenticator(tokens, store)
	access := &resolver{access: map[uuid.UUID]*entities.Access{}}
	authorizer := middleware.NewAuthorizer(access)

	r := gin.New()
	r.Use(middleware.NewClientAuthorizer(clients).RequireClient(), authenticator.RequireAuth())
	r.GET("/me", func(ctx *gin.Context) {
		userID, _ := middleware.UserIDFromContext(ctx.Request.Context())
		ctx.String(http.StatusOK, userID.String())
	})
	r.GET("/members", authorizer.RequirePermission(entities.PermMembersRead), func(ctx *gin.Context) {
		granted, _ := middleware.AccessFromContext(ctx.Request.Context())
		ctx.String(http.StatusOK, granted.MembershipID.String())
	})
	return r, tokens, store, access
}

func accessToken(t *testing.T, tokens *authjwt.Service, role enums.Role, client *entities.Client) (string, uuid.UUID) {
	t.Helper()
	token, userID, _ := sessionToken(t, tokens, role, client)
	return token, userID
}

func sessionToken(t *testing.T, tokens *authjwt.Service, role enums.Role, client *entities.Client) (string, uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, sessionID := uuid.New(), uuid.New()
	token, _, err := tokens.Generate(services.AccessTokenClaims{UserID: userID, SessionID: sessionID, Role: role, ClientID: client.ClientID})
	require.NoError(t, err)
	return token, userID, sessionID
}

func staffToken(t *testing.T, tokens *authjwt.Service, client *entities.Client, scope enums.OrganizationType) (string, *entities.Access) {
	t.Helper()
	access := &entities.Access{
		MembershipID: uuid.New(), UserID: uuid.New(), OrganizationID: uuid.New(), Scope: scope,
		Status: enums.MembershipStatusActive, PermVersion: 1, Permissions: []entities.Permission{entities.PermMembersRead},
	}
	token, _, err := tokens.Generate(services.AccessTokenClaims{
		UserID: access.UserID, SessionID: uuid.New(), Role: enums.RoleStaff, ClientID: client.ClientID,
		MembershipID: &access.MembershipID, OrganizationID: &access.OrganizationID, Scope: scope, PermVersion: 1,
	})
	require.NoError(t, err)
	return token, access
}

func call(r *gin.Engine, clientID, token string) *httptest.ResponseRecorder {
	return callPath(r, "/me", clientID, token)
}

func callPath(r *gin.Engine, path, clientID, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
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
	adminToken, _ := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	partnerScopedToken, _ := staffToken(t, tokens, backoffice, enums.OrganizationTypePartner)

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
		{"partner membership replayed in the backoffice", backoffice.ClientID, partnerScopedToken, http.StatusForbidden, "TOKEN_CLIENT_MISMATCH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(r, tc.clientID, tc.token)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.code, errorCode(t, rec))
		})
	}
}

func TestRevokedSessionIsRejectedBeforeTheTokenExpires(t *testing.T) {
	r, tokens, store := newEngineWithRevocations(t)
	token, _, sessionID := sessionToken(t, tokens, enums.RoleCustomer, customerApp)
	other, _ := accessToken(t, tokens, enums.RoleCustomer, customerApp)
	require.Equal(t, http.StatusOK, call(r, customerApp.ClientID, token).Code)

	require.NoError(t, store.RevokeSessions(context.Background(), sessionID))

	rec := call(r, customerApp.ClientID, token)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "SESSION_REVOKED", errorCode(t, rec))
	assert.Equal(t, http.StatusOK, call(r, customerApp.ClientID, other).Code, "other sessions keep working")
}

func TestRevocationCheckFailsClosed(t *testing.T) {
	r, tokens, store := newEngineWithRevocations(t)
	token, _ := accessToken(t, tokens, enums.RoleCustomer, customerApp)
	store.err = errors.New("redis down")

	rec := call(r, customerApp.ClientID, token)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "AUTH_UNAVAILABLE", errorCode(t, rec))
}

func TestRequirePermission(t *testing.T) {
	r, tokens, _, resolved := newEngineWithStores(t)
	customerToken, _ := accessToken(t, tokens, enums.RoleCustomer, customerApp)
	allowedToken, allowed := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	deniedToken, denied := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	denied.Permissions = []entities.Permission{entities.PermRolesRead}
	suspendedToken, suspended := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	suspended.Status = enums.MembershipStatusSuspended
	movedToken, moved := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	moved.OrganizationID = uuid.New()
	unknownToken, _ := staffToken(t, tokens, backoffice, enums.OrganizationTypePlatform)
	for _, a := range []*entities.Access{allowed, denied, suspended, moved} {
		resolved.access[a.MembershipID] = a
	}

	rec := callPath(r, "/members", backoffice.ClientID, allowedToken)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, allowed.MembershipID.String(), rec.Body.String())

	cases := []struct {
		name     string
		clientID string
		token    string
		status   int
		code     string
	}{
		{"customer without membership", customerApp.ClientID, customerToken, http.StatusForbidden, "PERMISSION_DENIED"},
		{"missing permission", backoffice.ClientID, deniedToken, http.StatusForbidden, "PERMISSION_DENIED"},
		{"suspended membership", backoffice.ClientID, suspendedToken, http.StatusUnauthorized, "SESSION_REVOKED"},
		{"membership of another organization", backoffice.ClientID, movedToken, http.StatusUnauthorized, "SESSION_REVOKED"},
		{"membership gone", backoffice.ClientID, unknownToken, http.StatusUnauthorized, "SESSION_REVOKED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := callPath(r, "/members", tc.clientID, tc.token)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.code, errorCode(t, rec))
		})
	}

	t.Run("resolver failure fails closed", func(t *testing.T) {
		resolved.err = errors.New("database down")
		defer func() { resolved.err = nil }()

		rec := callPath(r, "/members", backoffice.ClientID, allowedToken)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "AUTH_UNAVAILABLE", errorCode(t, rec))
	})
}
