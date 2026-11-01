//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/infra/security/totp"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

const backofficeClient = "agora-backoffice"

var allDocuments = []string{
	"profile_photo", "id_card", "driving_license", "criminal_record", "vehicle_registration", "vehicle_ownership",
	"vehicle_insurance", "vehicle_inspection", "vehicle_photo_front", "vehicle_photo_rear", "vehicle_photo_side", "vehicle_photo_interior",
}

var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, bytes.Repeat([]byte{1}, 2048)...)

func serviceBySlug(t *testing.T, slug string) string {
	t.Helper()
	var id string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id FROM services WHERE slug = $1`, slug).Scan(&id))
	return id
}

func comfortWithTariff(t *testing.T) string {
	t.Helper()
	id := serviceBySlug(t, "taxi-comfort")
	ctx := context.Background()
	_, err := env.owner.Exec(ctx, `
		INSERT INTO service_tariffs (service_id, currency, base_fee_cents, per_km_fee_cents, per_minute_fee_cents, minimum_fee_cents, commission_bps)
		VALUES ($1, 'AOA', 45000, 18000, 2000, 70000, 1000)`, id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := env.owner.Exec(context.Background(), `UPDATE service_tariffs SET valid_until = now() WHERE service_id = $1 AND valid_until IS NULL`, id)
		require.NoError(t, err)
	})
	return id
}

func staffToken(t *testing.T) *person {
	t.Helper()
	ctx := context.Background()
	const email, secret = "admin-e2e@agora.ao", "uma-frase-longa-segura"
	pool := env.api.container.Pool
	bootstrap := usecases.NewAdminBootstrap(repository.NewUserRepository(pool), repository.NewOrganizationRepository(pool), repository.NewRoleRepository(pool),
		repository.NewMembershipRepository(pool), outbox.NewRepository(pool), repository.NewAuditLogRepository(pool), postgres.NewTxManager(pool), password.NewArgon2Hasher(password.DefaultParams))
	_, err := bootstrap.Run(ctx, "Admin", email, secret)
	require.NoError(t, err)

	staff := &person{client: backofficeClient, api: env.api.baseURL}
	login := staff.post(t, "/api/v1/auth/login", map[string]string{"identifier": email, "password": secret})
	require.Equal(t, http.StatusAccepted, login.status, login.text)
	setup := staff.post(t, "/api/v1/auth/mfa/setup", map[string]string{"mfa_token": login.str("mfa_token")})
	require.Equal(t, http.StatusOK, setup.status, setup.text)
	code, err := totp.CodeAt(setup.str("secret"), time.Now())
	require.NoError(t, err)
	verified := staff.post(t, "/api/v1/auth/mfa/verify", map[string]string{"mfa_token": login.str("mfa_token"), "code": code})
	require.Equal(t, http.StatusOK, verified.status, verified.text)
	staff.token = verified.str("access_token")
	return staff
}

func signUpProvider(t *testing.T, name string) *person {
	t.Helper()
	phone := fmt.Sprintf("+24492%07d", uuid.New().ID()%10_000_000)
	anonymous := &person{client: providerClient, api: env.api.baseURL}
	created := anonymous.post(t, "/api/v1/providers/register", map[string]string{"name": name, "phone": phone, "password": "uma-frase-longa-segura"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, "draft", created.str("status"))
	assert.Equal(t, false, created.body["is_verified"])

	session := anonymous.post(t, "/api/v1/auth/phone/verify", map[string]string{"phone": phone, "code": env.sms.lastCode(t, phone)})
	require.Equal(t, http.StatusOK, session.status, session.text)
	return &person{id: uuid.MustParse(created.str("id")), token: session.str("access_token"), client: providerClient, api: env.api.baseURL}
}

func (p *person) upload(t *testing.T, documentType, contentType string, content []byte) string {
	t.Helper()
	signed := p.post(t, "/api/v1/providers/me/documents/upload-url", map[string]any{"document_type": documentType, "content_type": contentType, "size": len(content)})
	require.Equal(t, http.StatusOK, signed.status, signed.text)
	req, err := http.NewRequest(http.MethodPut, signed.str("upload_url"), bytes.NewReader(content))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode, string(body))
	return signed.str("object_key")
}

func (p *person) sendDocument(t *testing.T, documentType string) {
	t.Helper()
	key := p.upload(t, documentType, "image/jpeg", jpegBytes)
	res := p.post(t, "/api/v1/providers/me/documents", map[string]string{"document_type": documentType, "object_key": key})
	require.Equal(t, http.StatusCreated, res.status, res.text)
}

func fillProfile(t *testing.T, p *person, serviceID string, seats int) {
	t.Helper()
	require.Equal(t, http.StatusOK, p.do(t, http.MethodPut, "/api/v1/providers/me/service", map[string]string{"service_id": serviceID}, nil).status)
	personal := p.do(t, http.MethodPut, "/api/v1/providers/me/personal", map[string]string{"birth_date": "1990-05-20", "id_number": "000000000LA000", "address": "Rua Rainha Ginga, 29, Luanda"}, nil)
	require.Equal(t, http.StatusOK, personal.status, personal.text)
	vehicle := p.do(t, http.MethodPut, "/api/v1/providers/me/vehicle", map[string]any{"brand": "Toyota", "model": "Corolla", "year": 2020, "color": "Branco", "plate": "LD-45-21-AF", "seats": seats}, nil)
	require.Equal(t, http.StatusOK, vehicle.status, vehicle.text)
}

func TestPedroCungaBecomesAComfortDriver(t *testing.T) {
	clearOnlineProviders(t)
	comfortID, economyID := comfortWithTariff(t), serviceBySlug(t, "taxi")
	admin := staffToken(t)

	var pedro *person
	t.Run("he creates the account himself and only the provider app can do it", func(t *testing.T) {
		customerApp := &person{client: customerClient, api: env.api.baseURL}
		assert.Equal(t, "REGISTRATION_NOT_ALLOWED", customerApp.post(t, "/api/v1/providers/register", map[string]string{"name": "X", "phone": "+244923111222", "password": "uma-frase-longa-segura"}).code())
		providerApp := &person{client: providerClient, api: env.api.baseURL}
		assert.Equal(t, "REGISTRATION_NOT_ALLOWED", providerApp.post(t, "/api/v1/auth/register", map[string]string{"phone": "+244923111222"}).code(), "the plain sign-up does not create providers")
		assert.Equal(t, http.StatusUnprocessableEntity, providerApp.post(t, "/api/v1/providers/register", map[string]string{"name": "X", "phone": "+244923111222", "password": "curta"}).status)

		pedro = signUpProvider(t, "Pedro Cunga")
		assert.Equal(t, "draft", pedro.get(t, "/api/v1/providers/me/profile").str("status"))
	})

	t.Run("he cannot go online or be approved yet", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, pedro.post(t, "/api/v1/providers/me/online", nil).status)
		assert.Equal(t, "INVALID_PROVIDER_TRANSITION", admin.do(t, http.MethodPut, "/api/v1/admin/providers/"+pedro.id.String(), map[string]any{"status": "approved", "service_id": comfortID}, nil).code(), "a draft cannot skip the review")
	})

	t.Run("the catalog offers Económico, Conforto and XL under Táxi", func(t *testing.T) {
		open := (&person{client: providerClient, api: env.api.baseURL}).get(t, "/api/v1/providers/onboarding/services")
		require.Equal(t, http.StatusOK, open.status, open.text)
		categories := open.body["categories"].([]any)
		require.Len(t, categories, 1, "only services with configured requirements are open")
		taxi := categories[0].(map[string]any)
		assert.Equal(t, "taxi", taxi["slug"])
		names := []string{}
		for _, raw := range taxi["services"].([]any) {
			names = append(names, raw.(map[string]any)["name"].(string))
		}
		assert.Equal(t, []string{"Económico", "Conforto", "XL"}, names)
		xl := taxi["services"].([]any)[2].(map[string]any)
		assert.EqualValues(t, 7, xl["min_vehicle_seats"])
		assert.Equal(t, "SERVICE_NOT_OPEN", pedro.do(t, http.MethodPut, "/api/v1/providers/me/service", map[string]string{"service_id": serviceBySlug(t, "towing")}, nil).code())
	})

	t.Run("an incomplete submission lists what is missing", func(t *testing.T) {
		none := pedro.post(t, "/api/v1/providers/me/submit", nil)
		assert.Equal(t, http.StatusUnprocessableEntity, none.status)
		assert.Equal(t, "PROFILE_INCOMPLETE", none.code())

		fillProfile(t, pedro, comfortID, 5)
		incomplete := pedro.post(t, "/api/v1/providers/me/submit", nil)
		assert.Equal(t, "PROFILE_INCOMPLETE", incomplete.code())
		details := incomplete.body["error"].(map[string]any)["details"].(map[string]any)
		for _, document := range allDocuments {
			assert.Contains(t, details, "documents."+document)
		}
		assert.NotContains(t, details, "vehicle")
		assert.Equal(t, "draft", pedro.get(t, "/api/v1/providers/me/profile").str("status"))
	})

	t.Run("bad uploads are refused", func(t *testing.T) {
		upload := func(documentType, contentType string, size int) result {
			return pedro.post(t, "/api/v1/providers/me/documents/upload-url", map[string]any{"document_type": documentType, "content_type": contentType, "size": size})
		}
		assert.Equal(t, "FILE_TYPE_NOT_ACCEPTED", upload("id_card", "application/zip", 100).code())
		assert.Equal(t, "FILE_TYPE_NOT_ACCEPTED", upload("profile_photo", "application/pdf", 100).code(), "a photo cannot be a PDF")
		assert.Equal(t, "FILE_TOO_LARGE", upload("id_card", "image/jpeg", 11*1024*1024).code())
		assert.Equal(t, "UNKNOWN_DOCUMENT_TYPE", upload("passport", "image/jpeg", 100).code())

		missing := "providers/" + pedro.id.String() + "/id_card/" + uuid.NewString() + ".jpg"
		assert.Equal(t, "FILE_NOT_UPLOADED", pedro.post(t, "/api/v1/providers/me/documents", map[string]string{"document_type": "id_card", "object_key": missing}).code())
		stolen := "providers/" + uuid.NewString() + "/id_card/" + uuid.NewString() + ".jpg"
		assert.Equal(t, "INVALID_OBJECT_KEY", pedro.post(t, "/api/v1/providers/me/documents", map[string]string{"document_type": "id_card", "object_key": stolen}).code(), "another provider's file cannot be claimed")

		disguised := pedro.upload(t, "id_card", "image/jpeg", []byte("<html><script>alert(1)</script></html>"))
		assert.Equal(t, "FILE_TYPE_NOT_ACCEPTED", pedro.post(t, "/api/v1/providers/me/documents", map[string]string{"document_type": "id_card", "object_key": disguised}).code(), "the content must match the declared type")
	})

	t.Run("documents stay private", func(t *testing.T) {
		key := pedro.upload(t, "profile_photo", "image/jpeg", jpegBytes)
		res, err := http.Get(env.storageURL + "/" + minioBucket + "/" + key)
		require.NoError(t, err)
		_ = res.Body.Close()
		assert.Equal(t, http.StatusForbidden, res.StatusCode, "no public URL")
	})

	t.Run("he uploads everything and submits", func(t *testing.T) {
		for _, document := range allDocuments {
			pedro.sendDocument(t, document)
		}
		submitted := pedro.post(t, "/api/v1/providers/me/submit", nil)
		require.Equal(t, http.StatusOK, submitted.status, submitted.text)
		assert.Equal(t, "pending", submitted.str("status"))

		assert.Equal(t, "PROFILE_LOCKED", pedro.do(t, http.MethodPut, "/api/v1/providers/me/service", map[string]string{"service_id": economyID}, nil).code())
		assert.Equal(t, "PROFILE_LOCKED", pedro.post(t, "/api/v1/providers/me/documents/upload-url", map[string]any{"document_type": "id_card", "content_type": "image/jpeg", "size": 100}).code())
		assert.Equal(t, http.StatusForbidden, pedro.post(t, "/api/v1/providers/me/online", nil).status)
	})

	t.Run("the admin sees the application with temporary links", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, pedro.get(t, "/api/v1/admin/providers/"+pedro.id.String()).status, "a provider is not staff")
		list := admin.get(t, "/api/v1/admin/providers?status=pending")
		require.Equal(t, http.StatusOK, list.status, list.text)
		assert.Contains(t, list.text, pedro.id.String())
		assert.Contains(t, list.text, "Conforto")

		view := admin.get(t, "/api/v1/admin/providers/"+pedro.id.String())
		require.Equal(t, http.StatusOK, view.status, view.text)
		assert.Equal(t, comfortID, view.str("requested_service_id"))
		documents := view.body["documents"].([]any)
		require.Len(t, documents, len(allDocuments))
		link, err := http.Get(documents[0].(map[string]any)["url"].(string))
		require.NoError(t, err)
		content, _ := io.ReadAll(link.Body)
		_ = link.Body.Close()
		assert.Equal(t, http.StatusOK, link.StatusCode)
		assert.Equal(t, jpegBytes, content)
	})

	t.Run("approval needs a service and approved documents", func(t *testing.T) {
		path := "/api/v1/admin/providers/" + pedro.id.String()
		assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, path, map[string]any{"status": "approved"}, nil).status, "without service_id")
		assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_ids": []string{comfortID}}, nil).status, "a list is ignored")
		early := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": comfortID}, nil)
		assert.Equal(t, "APPROVAL_REQUIREMENTS_NOT_MET", early.code())
		assert.Equal(t, "REASON_REQUIRED", admin.do(t, http.MethodPut, path, map[string]any{"status": "rejected"}, nil).code())
	})

	t.Run("one rejected document goes back and is sent again on its own", func(t *testing.T) {
		path := "/api/v1/admin/providers/" + pedro.id.String()
		assert.Equal(t, "REASON_REQUIRED", admin.post(t, path+"/documents/id_card/review", map[string]string{"status": "rejected"}).code())
		rejectedDocument := admin.post(t, path+"/documents/id_card/review", map[string]string{"status": "rejected", "reason": "Foto ilegível"})
		require.Equal(t, http.StatusOK, rejectedDocument.status, rejectedDocument.text)
		for _, document := range allDocuments {
			if document != "id_card" {
				require.Equal(t, http.StatusOK, admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved"}).status)
			}
		}
		rejected := admin.do(t, http.MethodPut, path, map[string]any{"status": "rejected", "reason": "Corrige o BI"}, nil)
		require.Equal(t, http.StatusOK, rejected.status, rejected.text)

		mine := pedro.get(t, "/api/v1/providers/me/profile")
		assert.Equal(t, "rejected", mine.str("status"))
		assert.Equal(t, "Corrige o BI", mine.str("status_reason"))
		assert.Contains(t, mine.body["missing"].(map[string]any)["documents.id_card"], "Foto ilegível")
		assert.Equal(t, "PROFILE_INCOMPLETE", pedro.post(t, "/api/v1/providers/me/submit", nil).code(), "the rejected document must be replaced")

		pedro.sendDocument(t, "id_card")
		again := pedro.post(t, "/api/v1/providers/me/submit", nil)
		require.Equal(t, http.StatusOK, again.status, again.text)
		assert.Equal(t, "pending", again.str("status"))
		for _, raw := range again.body["documents"].([]any) {
			document := raw.(map[string]any)
			if document["type"] == "id_card" {
				assert.Equal(t, "uploaded", document["status"])
			} else {
				assert.Equal(t, "approved", document["status"], "only the corrected document needs a new review")
			}
		}
	})

	t.Run("the XL rule comes from the data", func(t *testing.T) {
		path := "/api/v1/admin/providers/" + pedro.id.String()
		require.Equal(t, http.StatusOK, admin.post(t, path+"/documents/id_card/review", map[string]string{"status": "approved"}).status)
		tooSmall := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": serviceBySlug(t, "taxi-xl")}, nil)
		assert.Equal(t, "APPROVAL_REQUIREMENTS_NOT_MET", tooSmall.code())
		assert.Contains(t, tooSmall.text, "vehicle.seats")
	})

	t.Run("approved, he goes online and only gets his service", func(t *testing.T) {
		approved := admin.do(t, http.MethodPut, "/api/v1/admin/providers/"+pedro.id.String(), map[string]any{"status": "approved", "service_id": comfortID}, nil)
		require.Equal(t, http.StatusOK, approved.status, approved.text)
		assert.Equal(t, comfortID, approved.str("service_id"))

		position := positionAt(point{-8.8391, 13.2341})
		position["age_s"] = 120
		online := pedro.post(t, "/api/v1/providers/me/online", map[string]any{"position": position})
		require.Equal(t, http.StatusOK, online.status, online.text)
		stream := pedro.listen(t, "")

		customer := newCustomer(t)
		estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
		require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
		options := map[string]string{}
		for _, raw := range estimate.body["items"].([]any) {
			item := raw.(map[string]any)
			options[item["service_id"].(string)] = item["fare_estimate_id"].(string)
		}
		order := func(serviceID string) string {
			res := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": serviceID, "fare_estimate_id": options[serviceID], "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
			require.Equal(t, http.StatusCreated, res.status, res.text)
			return res.str("id")
		}
		order(economyID)
		time.Sleep(500 * time.Millisecond)
		assert.Empty(t, stream.ofType("request.offer"), "an Económico request never reaches a Conforto driver")

		comfortRequest := order(comfortID)
		offer := stream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == comfortRequest })
		accepted := acceptOffer(t, pedro, offer)
		require.Equal(t, http.StatusOK, accepted.status, accepted.text)
		view := customer.get(t, "/api/v1/requests/"+comfortRequest)
		assert.Equal(t, "LD-45-21-AF", view.body["provider"].(map[string]any)["vehicle_plate"])
		assert.Equal(t, "Pedro Cunga", view.body["provider"].(map[string]any)["name"])
		assert.Equal(t, "Toyota Corolla Branco", view.body["provider"].(map[string]any)["vehicle_description"])
		require.Equal(t, http.StatusOK, pedro.post(t, "/api/v1/requests/"+comfortRequest+"/cancel", map[string]string{"reason": "Fim do teste"}).status)
	})

	t.Run("the decisions reach him and the audit trail", func(t *testing.T) {
		ctx := context.Background()
		require.Eventually(t, func() bool {
			var sent int
			require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND channel = 'sms' AND template = 'provider_review'`, pedro.id).Scan(&sent))
			return sent == 2
		}, 5*time.Second, 50*time.Millisecond, "one SMS for the rejection, one for the approval")
		var audited int
		require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type = 'provider_profile' AND entity_id = $1`, pedro.id.String()).Scan(&audited))
		assert.Equal(t, 2, audited)
	})

	t.Run("a suspended driver cannot go online or accept", func(t *testing.T) {
		clearOnlineProviders(t)
		position := positionAt(point{-8.8391, 13.2341})
		position["age_s"] = 600
		require.Equal(t, http.StatusOK, pedro.post(t, "/api/v1/providers/me/online", map[string]any{"position": position}).status)
		stream := pedro.listen(t, "")
		customer := newCustomer(t)
		estimate := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "taxi", "pickup": pickup, "destination": target})
		require.Equal(t, http.StatusCreated, estimate.status, estimate.text)
		var fareID string
		for _, raw := range estimate.body["items"].([]any) {
			if item := raw.(map[string]any); item["service_id"] == comfortID {
				fareID = item["fare_estimate_id"].(string)
			}
		}
		created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": comfortID, "fare_estimate_id": fareID, "pickup": pickup, "destination": target}, map[string]string{"Idempotency-Key": uuid.NewString()})
		require.Equal(t, http.StatusCreated, created.status, created.text)
		offer := stream.waitFor(t, "request.offer", func(event sseEvent) bool { return event.Data["request_id"] == created.str("id") })

		path := "/api/v1/admin/providers/" + pedro.id.String()
		assert.Equal(t, "REASON_REQUIRED", admin.do(t, http.MethodPut, path, map[string]any{"status": "suspended"}, nil).code())
		require.Equal(t, http.StatusOK, admin.do(t, http.MethodPut, path, map[string]any{"status": "suspended", "reason": "Queixas"}, nil).status)

		assert.Equal(t, http.StatusForbidden, acceptOffer(t, pedro, offer).status)
		assert.Equal(t, http.StatusForbidden, pedro.post(t, "/api/v1/providers/me/online", nil).status)
	})
}

func TestProviderSignUpEdges(t *testing.T) {
	first := signUpProvider(t, "Ana")
	phone := first.get(t, "/api/v1/providers/me/profile").str("phone")
	anonymous := &person{client: providerClient, api: env.api.baseURL}
	assert.Equal(t, "PHONE_ALREADY_EXISTS", anonymous.post(t, "/api/v1/providers/register", map[string]string{"name": "Outra", "phone": phone, "password": "uma-frase-longa-segura"}).code())

	unverified := fmt.Sprintf("+24492%07d", uuid.New().ID()%10_000_000)
	require.Equal(t, http.StatusCreated, anonymous.post(t, "/api/v1/providers/register", map[string]string{"name": "Primeiro", "phone": unverified, "password": "uma-frase-longa-segura"}).status)
	resumed := anonymous.post(t, "/api/v1/providers/register", map[string]string{"name": "Segundo", "phone": unverified, "password": "outra-frase-longa-segura"})
	require.Equal(t, http.StatusCreated, resumed.status, resumed.text)
	assert.Equal(t, "Segundo", resumed.str("name"), "an unverified sign-up can be restarted")

	customer := newCustomer(t)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/providers/me/profile").status)

	legacy := newPerson(t, "provider")
	assert.Equal(t, "draft", legacy.get(t, "/api/v1/providers/me/profile").str("status"), "an account from before this flow can fill in its profile")
	assert.Equal(t, strconv.Itoa(0), strconv.Itoa(len(legacy.get(t, "/api/v1/providers/me/profile").body["documents"].([]any))))
}
