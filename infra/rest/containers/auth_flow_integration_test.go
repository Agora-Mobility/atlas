//go:build integration

package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/cache"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/notification/push"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/infra/security/revocation"
	"github.com/Agora-Mobility/atlas/infra/sse"
	"github.com/Agora-Mobility/atlas/infra/storage/r2"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

const (
	customerApp   = "agora-customer-app"
	backofficeApp = "agora-backoffice"
	partnerApp    = "agora-partner-panel"
	customerPhone = "+244923000000"
	operatorPhone = "+244923000020"
	ownerPhone    = "+244923000030"
	adminEmail    = "admin@agora.ao"
	adminPhone    = "+244923000040"
	adminPassword = "uma-frase-longa-segura"
)

// smsInbox keeps the last SMS sent to each phone.
type smsInbox struct {
	mu   sync.Mutex
	last map[string]string
}

func (s *smsInbox) Send(_ context.Context, phone, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last[phone] = message
	return nil
}

var (
	codePattern  = regexp.MustCompile(`\b\d{6}\b`)
	tokenPattern = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)
)

func (s *smsInbox) invitationToken(t *testing.T, phone string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	match := tokenPattern.FindStringSubmatch(s.last[phone])
	require.Len(t, match, 2, "no invitation sent to %s", phone)
	delete(s.last, phone)
	return match[1]
}

func (s *smsInbox) code(t *testing.T, phone string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	code := codePattern.FindString(s.last[phone])
	require.NotEmpty(t, code, "no code sent to %s", phone)
	delete(s.last, phone)
	return code
}

type emailOutbox struct {
	mu   sync.Mutex
	last map[string]services.Email
}

func (o *emailOutbox) Send(_ context.Context, email services.Email) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last[email.To] = email
	return nil
}

func (o *emailOutbox) to(t *testing.T, address string) services.Email {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	email, ok := o.last[address]
	require.True(t, ok, "no email sent to %s", address)
	return email
}

type api struct {
	t      *testing.T
	engine *gin.Engine
}

type response struct {
	status int
	body   map[string]any
}

func (r response) code() string {
	errBody, _ := r.body["error"].(map[string]any)
	code, _ := errBody["code"].(string)
	return code
}

func (a api) call(method, path, clientID, token string, payload any) response {
	a.t.Helper()
	var body bytes.Buffer
	if payload != nil {
		require.NoError(a.t, json.NewEncoder(&body).Encode(payload))
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.HeaderClientID, clientID)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)

	out := response{status: rec.Code, body: map[string]any{}}
	if rec.Body.Len() > 0 {
		require.NoError(a.t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func (a api) login(clientID, phoneNumber, secret string) (access, refresh string) {
	a.t.Helper()
	res := a.call(http.MethodPost, "/api/v1/auth/login", clientID, "", map[string]string{"phone": phoneNumber, "password": secret})
	require.Equal(a.t, http.StatusOK, res.status, res.body)
	return res.body["access_token"].(string), res.body["refresh_token"].(string)
}

func (a api) roleIDs(path, token string) map[string]string {
	a.t.Helper()
	res := a.call(http.MethodGet, path, backofficeApp, token, nil)
	require.Equal(a.t, http.StatusOK, res.status, res.body)
	ids := map[string]string{}
	for _, item := range res.body["items"].([]any) {
		role := item.(map[string]any)
		ids[role["code"].(string)] = role["id"].(string)
	}
	return ids
}

func permissionCatalog() []string {
	all := []entities.Permission{
		entities.PermMembersRead, entities.PermMembersInvite, entities.PermMembersUpdate, entities.PermMembersSuspend,
		entities.PermMembersDeactivate, entities.PermRolesRead, entities.PermRolesAssign, entities.PermRolesManage,
		entities.PermPartnersRead, entities.PermPartnersManage, entities.PermUsersRead, entities.PermUsersBlock,
		entities.PermUsersResetPassword, entities.PermProvidersRead, entities.PermProvidersApprove, entities.PermProvidersSuspend,
		entities.PermCategoriesManage, entities.PermServicesManage, entities.PermRequestsRead, entities.PermRequestsManage,
		entities.PermRequestsCancel, entities.PermFinanceRead, entities.PermRefundsCreate, entities.PermRefundsApprove,
		entities.PermReconciliationRun, entities.PermAuditRead, entities.PermPartnerProvidersRead,
		entities.PermPartnerProvidersManage, entities.PermPartnerRequestsRead, entities.PermPartnerFinanceRead,
	}
	codes := make([]string, 0, len(all))
	for _, p := range all {
		codes = append(codes, string(p))
	}
	return codes
}

func TestAuthFlow(t *testing.T) {
	pool, owner, rdb := setupStores(t)
	sms := &smsInbox{last: map[string]string{}}
	mail := &emailOutbox{last: map[string]services.Email{}}
	a := api{t: t, engine: newTestEngine(t, pool, rdb, sms, mail)}
	ctx := context.Background()

	var access, refresh string

	t.Run("sign up with the phone, verify it, then name the account and set a password", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/auth/register", customerApp, "", map[string]string{"phone": "923 000 000"})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		assert.Equal(t, "customer", res.body["role"])
		assert.Equal(t, false, res.body["is_verified"])

		res = a.call(http.MethodPost, "/api/v1/auth/phone/verify", customerApp, "", map[string]string{"phone": customerPhone, "code": sms.code(t, customerPhone)})
		require.Equal(t, http.StatusOK, res.status, res.body)
		access, refresh = res.body["access_token"].(string), res.body["refresh_token"].(string)

		res = a.call(http.MethodPost, "/api/v1/auth/register", customerApp, "", map[string]string{"phone": customerPhone})
		assert.Equal(t, "PHONE_ALREADY_EXISTS", res.code())

		res = a.call(http.MethodPatch, "/api/v1/users/me", customerApp, access, map[string]any{"name": "Ana Domingos"})
		require.Equal(t, http.StatusOK, res.status, res.body)

		res = a.call(http.MethodPut, "/api/v1/users/password", customerApp, access, map[string]string{"current_password": "x", "new_password": "S3gredo!forte"})
		assert.Equal(t, "PASSWORD_NOT_SET", res.code())

		res = a.call(http.MethodPost, "/api/v1/users/password", customerApp, access, map[string]string{"password": "S3gredo!forte"})
		require.Equal(t, http.StatusNoContent, res.status, res.body)
		res = a.call(http.MethodPost, "/api/v1/users/password", customerApp, access, map[string]string{"password": "Outro!segredo"})
		assert.Equal(t, "PASSWORD_ALREADY_SET", res.code())

		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/users/me", customerApp, access, nil).status)
		a.login(customerApp, customerPhone, "S3gredo!forte")
	})

	t.Run("manage the profile", func(t *testing.T) {
		res := a.call(http.MethodPatch, "/api/v1/users/me", customerApp, access, map[string]any{"name": "Ana Maria"})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, "Ana Maria", res.body["name"])

		res = a.call(http.MethodPatch, "/api/v1/users/me", customerApp, access, map[string]any{"email": "ana@agora.ao"})
		assert.Equal(t, "CURRENT_PASSWORD_REQUIRED", res.code())
		res = a.call(http.MethodPatch, "/api/v1/users/me", customerApp, access, map[string]any{"email": "ana@agora.ao", "current_password": "S3gredo!forte"})
		require.Equal(t, http.StatusOK, res.status, res.body)

		res = a.call(http.MethodGet, "/api/v1/users/me", customerApp, access, nil)
		require.Equal(t, http.StatusOK, res.status)
		assert.Equal(t, "ana@agora.ao", res.body["email"])
	})

	t.Run("change the phone with a code sent to the new number", func(t *testing.T) {
		const newPhone = "+244923111111"
		res := a.call(http.MethodPost, "/api/v1/users/me/phone/verification-code", customerApp, access, map[string]string{"phone": newPhone})
		require.Equal(t, http.StatusAccepted, res.status, res.body)

		res = a.call(http.MethodPut, "/api/v1/users/me/phone", customerApp, access, map[string]string{"phone": newPhone, "code": sms.code(t, newPhone)})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, newPhone, res.body["phone"])

		a.login(customerApp, newPhone, "S3gredo!forte")

		// Back to the original number for the rest of the flow.
		require.Equal(t, http.StatusAccepted, a.call(http.MethodPost, "/api/v1/users/me/phone/verification-code", customerApp, access, map[string]string{"phone": customerPhone}).status)
		res = a.call(http.MethodPut, "/api/v1/users/me/phone", customerApp, access, map[string]string{"phone": customerPhone, "code": sms.code(t, customerPhone)})
		require.Equal(t, http.StatusOK, res.status, res.body)
	})

	t.Run("a reused refresh token kills the session and its access token at once", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/auth/refresh", customerApp, "", map[string]string{"refresh_token": refresh})
		require.Equal(t, http.StatusOK, res.status, res.body)
		rotatedAccess := res.body["access_token"].(string)

		res = a.call(http.MethodPost, "/api/v1/auth/refresh", customerApp, "", map[string]string{"refresh_token": refresh})
		assert.Equal(t, "REFRESH_TOKEN_REUSED", res.code())

		for _, token := range []string{access, rotatedAccess} {
			res = a.call(http.MethodGet, "/api/v1/users/me", customerApp, token, nil)
			assert.Equal(t, http.StatusUnauthorized, res.status)
			assert.Equal(t, "SESSION_REVOKED", res.code())
		}
	})

	t.Run("logout ends the access token at once", func(t *testing.T) {
		token, _ := a.login(customerApp, customerPhone, "S3gredo!forte")
		other, _ := a.login(customerApp, customerPhone, "S3gredo!forte")

		require.Equal(t, http.StatusNoContent, a.call(http.MethodPost, "/api/v1/auth/logout", customerApp, token, nil).status)

		assert.Equal(t, "SESSION_REVOKED", a.call(http.MethodGet, "/api/v1/users/me", customerApp, token, nil).code())
		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/users/me", customerApp, other, nil).status)
	})

	t.Run("recovery sets a new password and ends every session", func(t *testing.T) {
		token, _ := a.login(customerApp, customerPhone, "S3gredo!forte")

		res := a.call(http.MethodPost, "/api/v1/auth/recovery/request", customerApp, "", map[string]string{"phone": customerPhone})
		require.Equal(t, http.StatusAccepted, res.status)
		res = a.call(http.MethodPost, "/api/v1/auth/recovery/confirm", customerApp, "", map[string]string{
			"phone": customerPhone, "code": sms.code(t, customerPhone), "new_password": "N0vo!segredo",
		})
		require.Equal(t, http.StatusOK, res.status, res.body)

		assert.Equal(t, "SESSION_REVOKED", a.call(http.MethodGet, "/api/v1/users/me", customerApp, token, nil).code())
		a.login(customerApp, customerPhone, "N0vo!segredo")
	})

	var adminToken string
	t.Run("a verified account signs in with an SMS code", func(t *testing.T) {
		request := func(clientID string) response {
			return a.call(http.MethodPost, "/api/v1/auth/login/otp/request", clientID, "", map[string]string{"phone": customerPhone})
		}
		verify := func(clientID, code string) response {
			return a.call(http.MethodPost, "/api/v1/auth/login/otp/verify", clientID, "", map[string]string{"phone": customerPhone, "code": code})
		}

		res := request(customerApp)
		require.Equal(t, http.StatusAccepted, res.status, res.body)
		code := sms.code(t, customerPhone)

		res = a.call(http.MethodPost, "/api/v1/auth/phone/verify", customerApp, "", map[string]string{"phone": customerPhone, "code": code})
		assert.Equal(t, "CODE_EXPIRED", res.code(), "a sign-in code does not serve the phone verification")
		assert.Equal(t, "CLIENT_NOT_ALLOWED", verify(backofficeApp, code).code())

		res = verify(customerApp, code)
		require.Equal(t, http.StatusOK, res.status, res.body)
		token, refreshToken := res.body["access_token"].(string), res.body["refresh_token"].(string)
		assert.Equal(t, customerApp, res.body["client"])
		require.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/users/me", customerApp, token, nil).status)

		res = a.call(http.MethodPost, "/api/v1/auth/refresh", customerApp, "", map[string]string{"refresh_token": refreshToken})
		require.Equal(t, http.StatusOK, res.status, res.body)

		assert.Equal(t, "CODE_EXPIRED", verify(customerApp, code).code(), "a code works once")
	})

	t.Run("the first super admin is bootstrapped and signs in with the password alone", func(t *testing.T) {
		bootstrap := usecases.NewAdminBootstrap(repository.NewUserRepository(pool), repository.NewOrganizationRepository(pool),
			repository.NewRoleRepository(pool), repository.NewMembershipRepository(pool), outbox.NewRepository(pool),
			repository.NewAuditLogRepository(pool), postgres.NewTxManager(pool), password.NewArgon2Hasher(password.DefaultParams))
		created, err := bootstrap.Run(ctx, "Admin", adminEmail, adminPhone, adminPassword)
		require.NoError(t, err)
		require.True(t, created)
		created, err = bootstrap.Run(ctx, "Admin", "outro@agora.ao", "+244923000041", adminPassword)
		require.NoError(t, err)
		assert.False(t, created, "a second run does nothing")

		res := a.call(http.MethodPost, "/api/v1/auth/login", backofficeApp, "", map[string]string{"phone": adminPhone, "password": adminPassword})
		require.Equal(t, http.StatusOK, res.status, res.body)
		adminToken = res.body["access_token"].(string)

		res = a.call(http.MethodGet, "/api/v1/users/me/permissions", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Contains(t, res.body["permissions"], "roles:manage")
	})

	t.Run("blocking a user ends the session at once and is audited", func(t *testing.T) {
		customerToken, _ := a.login(customerApp, customerPhone, "N0vo!segredo")
		me := a.call(http.MethodGet, "/api/v1/users/me", customerApp, customerToken, nil)
		customerID := me.body["id"].(string)

		res := a.call(http.MethodGet, "/api/v1/admin/block-reasons", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		reasonIDs := map[string]string{}
		for _, item := range res.body["items"].([]any) {
			reason := item.(map[string]any)
			reasonIDs[reason["code"].(string)] = reason["id"].(string)
		}
		require.Contains(t, reasonIDs, "fraud")
		require.Contains(t, reasonIDs, "duplicate_account")

		res = a.call(http.MethodPost, "/api/v1/admin/users/"+customerID+"/block", backofficeApp, adminToken, map[string]any{"reason_ids": []string{}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, res.body)
		res = a.call(http.MethodPost, "/api/v1/admin/users/"+customerID+"/block", backofficeApp, adminToken, map[string]any{"reason_ids": []string{uuid.NewString()}})
		assert.Equal(t, "UNKNOWN_BLOCK_REASON", res.code())

		res = a.call(http.MethodPost, "/api/v1/admin/users/"+customerID+"/block", backofficeApp, adminToken, map[string]any{
			"reason_ids": []string{reasonIDs["fraud"], reasonIDs["duplicate_account"]}, "note": "Reportado pelo cliente",
		})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, "blocked", res.body["status"])

		assert.Equal(t, "SESSION_REVOKED", a.call(http.MethodGet, "/api/v1/users/me", customerApp, customerToken, nil).code())
		res = a.call(http.MethodPost, "/api/v1/auth/login", customerApp, "", map[string]string{"phone": customerPhone, "password": "N0vo!segredo"})
		assert.Equal(t, "ACCOUNT_BLOCKED", res.code())

		var reason string
		require.NoError(t, owner.QueryRow(ctx, "SELECT reason FROM audit_log WHERE action = 'user.block' AND entity_id = $1", customerID).Scan(&reason))
		assert.Equal(t, "duplicate_account, fraud: Reportado pelo cliente", reason)

		var codes []string
		require.NoError(t, owner.QueryRow(ctx, `
			SELECT array_agg(r.code ORDER BY r.code)
			FROM user_blocks b
			JOIN user_block_reasons br ON br.user_block_id = b.id
			JOIN block_reasons r ON r.id = br.block_reason_id
			WHERE b.user_id = $1`, customerID).Scan(&codes))
		assert.Equal(t, []string{"duplicate_account", "fraud"}, codes)

		res = a.call(http.MethodPost, "/api/v1/admin/users/"+customerID+"/unblock", backofficeApp, adminToken, map[string]string{})
		require.Equal(t, http.StatusOK, res.status, res.body)
		a.login(customerApp, customerPhone, "N0vo!segredo")
	})

	var operatorMemberID, operatorToken string
	t.Run("an invited operator activates the account by SMS", func(t *testing.T) {
		roles := a.roleIDs("/api/v1/roles", adminToken)
		res := a.call(http.MethodPost, "/api/v1/members/invitations", backofficeApp, adminToken, map[string]any{
			"name": "Operador", "email": "Op@Agora.ao", "phone": "923 000 020", "role_ids": []string{roles["operations"]},
			"organization_id": uuid.NewString(),
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		assert.Equal(t, "invited", res.body["status"])
		operatorMemberID = res.body["id"].(string)

		token := sms.invitationToken(t, operatorPhone)
		invitation := mail.to(t, "op@agora.ao")
		assert.Contains(t, invitation.HTML, "token="+token, "the e-mail carries the same link as the SMS")
		var stored string
		require.NoError(t, owner.QueryRow(ctx, "SELECT token_hash FROM invitations WHERE membership_id = $1", operatorMemberID).Scan(&stored))
		assert.NotEqual(t, token, stored, "CA08.5 only the hash is stored")
		var organizationType string
		require.NoError(t, owner.QueryRow(ctx, "SELECT o.type FROM memberships m JOIN organizations o ON o.id = m.organization_id WHERE m.id = $1", operatorMemberID).Scan(&organizationType))
		assert.Equal(t, "platform", organizationType, "CAE.2 organization_id in the body is ignored")

		res = a.call(http.MethodPost, "/api/v1/auth/login", backofficeApp, "", map[string]string{"phone": operatorPhone, "password": "S3gredo!forte"})
		assert.Equal(t, "INVALID_CREDENTIALS", res.code(), "CA10.1 an invited member cannot sign in")

		res = a.call(http.MethodPost, "/api/v1/invitations/verification-code", backofficeApp, "", map[string]string{"token": token})
		require.Equal(t, http.StatusAccepted, res.status, res.body)
		res = a.call(http.MethodPost, "/api/v1/invitations/accept", backofficeApp, "", map[string]string{
			"token": token, "password": "S3gredo!forte", "code": sms.code(t, operatorPhone),
		})
		require.Equal(t, http.StatusNoContent, res.status, res.body)

		res = a.call(http.MethodPost, "/api/v1/invitations/accept", backofficeApp, "", map[string]string{"token": token, "password": "S3gredo!forte", "code": "123456"})
		assert.Equal(t, "INVALID_INVITATION", res.code(), "CA08.2 single use")

		operatorToken, _ = a.login(backofficeApp, operatorPhone, "S3gredo!forte")
	})

	t.Run("permissions decide every endpoint", func(t *testing.T) {
		res := a.call(http.MethodGet, "/api/v1/admin/users?role=customer", backofficeApp, operatorToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 1, res.body["total"])

		res = a.call(http.MethodGet, "/api/v1/admin/users", backofficeApp, operatorToken, nil)
		for _, item := range res.body["items"].([]any) {
			assert.NotEqual(t, "staff", item.(map[string]any)["role"], "staff are managed as members")
		}

		res = a.call(http.MethodPost, "/api/v1/members/invitations", backofficeApp, operatorToken, map[string]any{
			"name": "Outro", "email": "x@agora.ao", "phone": "923000099", "role_ids": []string{uuid.NewString()},
		})
		assert.Equal(t, "PERMISSION_DENIED", res.code())
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/partners", backofficeApp, operatorToken, nil).code())

		res = a.call(http.MethodGet, "/api/v1/admin/users?role=customer", customerApp, adminToken, nil)
		assert.Equal(t, "TOKEN_CLIENT_MISMATCH", res.code())
	})

	t.Run("CA07.1 role changes apply on the next request", func(t *testing.T) {
		require.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/admin/block-reasons", backofficeApp, operatorToken, nil).status)

		roles := a.roleIDs("/api/v1/roles", adminToken)
		res := a.call(http.MethodPut, "/api/v1/members/"+operatorMemberID+"/roles", backofficeApp, adminToken, map[string]any{"role_ids": []string{roles["support"]}})
		require.Equal(t, http.StatusOK, res.status, res.body)

		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/admin/block-reasons", backofficeApp, operatorToken, nil).code())
		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/admin/users", backofficeApp, operatorToken, nil).status)

		var version int64
		require.NoError(t, owner.QueryRow(ctx, "SELECT perm_version FROM memberships WHERE id = $1", operatorMemberID).Scan(&version))
		assert.Equal(t, int64(3), version, "activation and one role change")
	})

	t.Run("CA06.1 suspension ends the session at once", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/members/"+operatorMemberID+"/suspend", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, "suspended", res.body["status"])

		assert.Equal(t, "SESSION_REVOKED", a.call(http.MethodGet, "/api/v1/users/me", backofficeApp, operatorToken, nil).code())
		res = a.call(http.MethodPost, "/api/v1/auth/login", backofficeApp, "", map[string]string{"phone": operatorPhone, "password": "S3gredo!forte"})
		assert.Equal(t, "INVALID_CREDENTIALS", res.code())

		res = a.call(http.MethodPost, "/api/v1/members/"+operatorMemberID+"/reactivate", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		operatorToken, _ = a.login(backofficeApp, operatorPhone, "S3gredo!forte")
	})

	t.Run("the last super admin and the actor's own membership are protected", func(t *testing.T) {
		me := a.call(http.MethodGet, "/api/v1/users/me/permissions", backofficeApp, adminToken, nil)
		adminMemberID := me.body["membership_id"].(string)

		assert.Equal(t, "CANNOT_MANAGE_SELF", a.call(http.MethodPost, "/api/v1/members/"+adminMemberID+"/suspend", backofficeApp, adminToken, nil).code())
		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/members/"+adminMemberID, backofficeApp, adminToken, nil).status)
	})

	t.Run("AGORA creates a partner whose owner works isolated in the partner panel", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/partners", backofficeApp, adminToken, map[string]string{"name": "Transportes Kianda", "nif": "541 712 3456"})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		partnerID := res.body["id"].(string)
		assert.Equal(t, "5417123456", res.body["nif"])
		res = a.call(http.MethodPost, "/api/v1/partners", backofficeApp, adminToken, map[string]string{"name": "Outra", "nif": "5417123456"})
		assert.Equal(t, "NIF_ALREADY_EXISTS", res.code())
		res = a.call(http.MethodPost, "/api/v1/partners", backofficeApp, adminToken, map[string]string{"name": "Outra", "nif": "123"})
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, res.body)
		res = a.call(http.MethodPost, "/api/v1/partners", backofficeApp, adminToken, map[string]string{"name": "Sem NIF"})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		assert.NotContains(t, res.body, "nif")

		roles := a.roleIDs("/api/v1/partners/"+partnerID+"/roles", adminToken)
		platformRoles := a.roleIDs("/api/v1/roles", adminToken)
		res = a.call(http.MethodPost, "/api/v1/partners/"+partnerID+"/members/invitations", backofficeApp, adminToken, map[string]any{
			"name": "Dono", "email": "dono@kianda.ao", "phone": ownerPhone, "role_ids": []string{platformRoles["support"]},
		})
		assert.Equal(t, "ROLE_NOT_ASSIGNABLE", res.code(), "CA02.1")
		res = a.call(http.MethodPost, "/api/v1/partners/"+partnerID+"/members/invitations", backofficeApp, adminToken, map[string]any{
			"name": "Dono", "email": "dono@kianda.ao", "phone": ownerPhone, "role_ids": []string{roles["owner"]},
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)

		token := sms.invitationToken(t, ownerPhone)
		res = a.call(http.MethodPost, "/api/v1/invitations/verification-code", backofficeApp, "", map[string]string{"token": token})
		assert.Equal(t, "INVALID_INVITATION", res.code(), "a partner invitation is accepted in the partner panel")
		require.Equal(t, http.StatusAccepted, a.call(http.MethodPost, "/api/v1/invitations/verification-code", partnerApp, "", map[string]string{"token": token}).status)
		res = a.call(http.MethodPost, "/api/v1/invitations/accept", partnerApp, "", map[string]string{"token": token, "password": "S3gredo!forte", "code": sms.code(t, ownerPhone)})
		require.Equal(t, http.StatusNoContent, res.status, res.body)

		res = a.call(http.MethodPost, "/api/v1/auth/login", partnerApp, "", map[string]string{"phone": ownerPhone, "password": "S3gredo!forte"})
		require.Equal(t, http.StatusOK, res.status, res.body)
		ownerToken := res.body["access_token"].(string)

		res = a.call(http.MethodGet, "/api/v1/members", partnerApp, ownerToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 1, res.body["total"])
		res = a.call(http.MethodGet, "/api/v1/members/"+operatorMemberID, partnerApp, ownerToken, nil)
		assert.Equal(t, http.StatusNotFound, res.status, "CA14.1 another tenant answers 404")
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/partners", partnerApp, ownerToken, nil).code())
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/admin/users", partnerApp, ownerToken, nil).code())
		assert.Equal(t, "TOKEN_CLIENT_MISMATCH", a.call(http.MethodGet, "/api/v1/members", backofficeApp, ownerToken, nil).code())

		res = a.call(http.MethodGet, "/api/v1/partners/"+partnerID+"/members", backofficeApp, adminToken, nil)
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.EqualValues(t, 1, res.body["total"], "CA14.2")
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/partners/"+partnerID+"/members", backofficeApp, operatorToken, nil).code(), "CA14.3")
	})

	t.Run("custom roles reach their members on the next request", func(t *testing.T) {
		res := a.call(http.MethodPost, "/api/v1/roles", backofficeApp, adminToken, map[string]any{
			"code": "field_support", "name": "Suporte de campo", "permissions": []string{"users:read", "members:read"},
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
		roleID := res.body["id"].(string)
		assert.Equal(t, false, res.body["is_system"])

		res = a.call(http.MethodPost, "/api/v1/roles", backofficeApp, adminToken, map[string]any{
			"code": "field_support", "name": "Outra", "permissions": []string{"users:read"},
		})
		assert.Equal(t, "ROLE_CODE_TAKEN", res.code())
		res = a.call(http.MethodPost, "/api/v1/roles", backofficeApp, adminToken, map[string]any{
			"code": "mix", "name": "Mistura", "permissions": []string{"partner.finance:read"},
		})
		assert.Equal(t, "UNKNOWN_PERMISSION", res.code())

		res = a.call(http.MethodPut, "/api/v1/members/"+operatorMemberID+"/roles", backofficeApp, adminToken, map[string]any{"role_ids": []string{roleID}})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/admin/users", backofficeApp, operatorToken, nil).status)
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodGet, "/api/v1/admin/block-reasons", backofficeApp, operatorToken, nil).code())

		res = a.call(http.MethodPatch, "/api/v1/roles/"+roleID, backofficeApp, adminToken, map[string]any{"permissions": []string{"users:read", "users:block"}})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, http.StatusOK, a.call(http.MethodGet, "/api/v1/admin/block-reasons", backofficeApp, operatorToken, nil).status,
			"the permission added to the role works at once")

		assert.Equal(t, "ROLE_IN_USE", a.call(http.MethodDelete, "/api/v1/roles/"+roleID, backofficeApp, adminToken, nil).code())
		roles := a.roleIDs("/api/v1/roles", adminToken)
		assert.Equal(t, "SYSTEM_ROLE_IMMUTABLE", a.call(http.MethodPatch, "/api/v1/roles/"+roles["operations"], backofficeApp, adminToken, map[string]any{"name": "x"}).code())
		assert.Equal(t, "PERMISSION_DENIED", a.call(http.MethodPost, "/api/v1/roles", backofficeApp, operatorToken, map[string]any{
			"code": "x_role", "name": "X", "permissions": []string{"users:read"},
		}).code())

		res = a.call(http.MethodPut, "/api/v1/members/"+operatorMemberID+"/roles", backofficeApp, adminToken, map[string]any{"role_ids": []string{roles["operations"]}})
		require.Equal(t, http.StatusOK, res.status, res.body)
		assert.Equal(t, http.StatusNoContent, a.call(http.MethodDelete, "/api/v1/roles/"+roleID, backofficeApp, adminToken, nil).status)
		assert.Equal(t, http.StatusNotFound, a.call(http.MethodGet, "/api/v1/roles/"+roleID, backofficeApp, adminToken, nil).status)
	})

	t.Run("every membership change reaches the outbox", func(t *testing.T) {
		rows, err := owner.Query(ctx, "SELECT DISTINCT event_type FROM outbox WHERE event_type LIKE 'access.%' ORDER BY 1")
		require.NoError(t, err)
		types, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		assert.Subset(t, types, []string{
			"access.member-activated", "access.member-invited", "access.member-reactivated",
			"access.member-roles-changed", "access.member-suspended", "access.partner-created",
			"access.role-created", "access.role-deleted", "access.role-updated",
		})
	})

	t.Run("RN16 the API runs as a role subject to row-level security", func(t *testing.T) {
		var role string
		require.NoError(t, pool.QueryRow(ctx, "SELECT current_user").Scan(&role))
		assert.Equal(t, "atlas_app", role)
		var visible, all int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM memberships").Scan(&visible))
		require.NoError(t, owner.QueryRow(ctx, "SELECT count(*) FROM memberships").Scan(&all))
		assert.Zero(t, visible, "CA16.2 no tenant, no rows")
		assert.Positive(t, all)
		_, err := pool.Exec(ctx, "UPDATE audit_log SET reason = 'x'")
		assert.ErrorContains(t, err, "permission denied")
	})

	t.Run("the database enforces the role rules", func(t *testing.T) {
		_, err := owner.Exec(ctx, "UPDATE roles SET name = 'x' WHERE code = 'super_admin'")
		assert.ErrorContains(t, err, "system roles cannot be changed", "CA05.1")
		_, err = owner.Exec(ctx, "DELETE FROM role_permissions WHERE permission_code = 'members:read'")
		assert.ErrorContains(t, err, "system roles cannot be changed", "CA05.1")

		var partnerID string
		require.NoError(t, owner.QueryRow(ctx, "SELECT id FROM organizations WHERE type = 'partner' LIMIT 1").Scan(&partnerID))
		var customRole string
		require.NoError(t, owner.QueryRow(ctx, "INSERT INTO roles (organization_id, scope, code, name) VALUES ($1, 'partner', 'custom', 'Custom') RETURNING id", partnerID).Scan(&customRole))
		_, err = owner.Exec(ctx, "INSERT INTO role_permissions (role_id, permission_code) VALUES ($1, 'finance:read')", customRole)
		assert.ErrorContains(t, err, "not allowed in scope", "CA13.1")

		rows, err := owner.Query(ctx, "SELECT code FROM permissions ORDER BY code")
		require.NoError(t, err)
		inDatabase, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		assert.ElementsMatch(t, inDatabase, permissionCatalog(), "the catalog in code matches the database")
	})

	t.Run("deleting the account ends sessions and frees the phone", func(t *testing.T) {
		token, _ := a.login(customerApp, customerPhone, "N0vo!segredo")

		res := a.call(http.MethodDelete, "/api/v1/users/me", customerApp, token, map[string]string{"password": "errada"})
		assert.Equal(t, "INVALID_CURRENT_PASSWORD", res.code())
		res = a.call(http.MethodDelete, "/api/v1/users/me", customerApp, token, map[string]string{"password": "N0vo!segredo"})
		require.Equal(t, http.StatusNoContent, res.status, res.body)

		assert.Equal(t, "SESSION_REVOKED", a.call(http.MethodGet, "/api/v1/users/me", customerApp, token, nil).code())
		res = a.call(http.MethodPost, "/api/v1/auth/login", customerApp, "", map[string]string{"phone": customerPhone, "password": "N0vo!segredo"})
		assert.Equal(t, "INVALID_CREDENTIALS", res.code())

		res = a.call(http.MethodPost, "/api/v1/auth/register", customerApp, "", map[string]string{"phone": customerPhone})
		assert.Equal(t, http.StatusCreated, res.status, res.body)
	})
}

func newTestEngine(t *testing.T, pool *pgxpool.Pool, rdb *goredis.Client, sms *smsInbox, mail *emailOutbox) *gin.Engine {
	t.Helper()
	cfg := &config.Config{
		App:        config.AppConfig{Name: "atlas-test"},
		HTTP:       config.HTTPConfig{AuthRateLimit: 1000, AuthRateWindow: time.Minute},
		JWT:        config.JWTConfig{Secret: "test-secret-that-is-at-least-32-characters", Issuer: "atlas", AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour},
		OTP:        config.OTPConfig{TTL: 5 * time.Minute, MaxAttempts: 5},
		Dispatch:   config.DispatchConfig{WatcherInterval: time.Hour, WatcherBatch: 10, ChannelTimeout: time.Second, FareEstimateTTL: time.Minute, NearbyMinCount: 1, MaxAccuracyM: 100, MaxSpeedKmh: 200, OfferDeepLink: "agora://app", IdempotencyTTL: time.Hour, NotificationWorkers: 1},
		Realtime:   config.RealtimeConfig{Heartbeat: time.Second, PollInterval: time.Second, DeadAfter: 3 * time.Second, MailboxEntries: 200, MailboxTTL: time.Hour, ProjectorWorkers: 1},
		GoogleMaps: config.GoogleMapsConfig{Timeout: time.Second, BreakerFailures: 1, BreakerCooldown: time.Second, RoadFactor: 1.4, AverageSpeedKmh: 25},
		Access: config.AccessConfig{
			PermissionCacheTTL: time.Minute, InvitationBackoffice: "https://argus.test/convite", InvitationPartner: "https://athena.test/convite",
		},
	}
	jwt := authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.AccessTTL)
	revocations := revocation.NewRedisStore(rdb, cfg.App.Name+":", cfg.JWT.AccessTTL)
	c := &Container{
		Config:             cfg,
		Pool:               pool,
		Redis:              rdb,
		Cache:              cache.NewRedisCache(rdb, cfg.App.Name+":cache:"),
		TxManager:          postgres.NewTxManager(pool),
		Outbox:             outbox.NewRepository(pool),
		JWT:                jwt,
		SessionRevocations: revocations,
		Authenticator:      middleware.NewAuthenticator(jwt, revocations),
		ClientAuthorizer:   middleware.NewClientAuthorizer(repository.NewClientRepository(pool)),
		SMS:                sms,
		Email:              mail,
		SSE:                sse.NewHub(0),
	}
	auth, err := newAuthModule(c)
	require.NoError(t, err)
	c.Auth = auth
	c.Catalog = newCatalogModule(c)
	c.Dispatch = newDispatchModule(c, push.LogSender{}, newRoutingProvider(cfg.GoogleMaps))
	c.Providers = newProvidersModule(c, r2.Unavailable{})
	c.Fleet = newFleetModule(c)
	c.Payments = newPaymentsModule(c)
	c.Experience = newExperienceModule(c)
	c.Addresses = newAddressesModule(c)

	gin.SetMode(gin.TestMode)
	binding.Setup()
	r := gin.New()
	c.Auth.Router.Register(r.Group("/api/v1"))
	c.Catalog.Router.Register(r.Group("/api/v1"))
	c.Dispatch.Router.Register(r.Group("/api/v1"))
	c.Providers.Router.Register(r.Group("/api/v1"))
	c.Addresses.Router.Register(r.Group("/api/v1"))
	c.Fleet.Router.Register(r.Group("/api/v1"))
	c.Payments.Router.Register(r.Group("/api/v1"))
	c.Experience.Router.Register(r.Group("/api/v1"))
	return r
}

func setupStores(t *testing.T) (app, owner *pgxpool.Pool, rdb *goredis.Client) {
	t.Helper()
	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx, "postgis/postgis:18-3.6-alpine",
		tcpostgres.WithDatabase("atlas_test"),
		tcpostgres.WithUsername("atlas"),
		tcpostgres.WithPassword("atlas"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(url, "up"))
	dbCfg := config.DatabaseConfig{URL: url, MaxConns: 5, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute}
	app, err = postgres.Connection(ctx, dbCfg)
	require.NoError(t, err)
	t.Cleanup(app.Close)
	owner, err = postgres.OwnerConnection(ctx, dbCfg)
	require.NoError(t, err)
	t.Cleanup(owner.Close)

	redisContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:8.8.4-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisContainer.Terminate(context.Background()) })
	endpoint, err := redisContainer.Endpoint(ctx, "")
	require.NoError(t, err)
	rdb = goredis.NewClient(&goredis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = rdb.Close() })

	return app, owner, rdb
}
