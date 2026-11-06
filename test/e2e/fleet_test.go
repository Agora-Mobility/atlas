//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const partnerClient = "agora-partner-panel"

type partner struct {
	*person
	organizationID string
}

func newPartner(t *testing.T) *partner {
	t.Helper()
	ctx := context.Background()
	const secret = "uma-frase-longa-do-parceiro"
	hash, err := password.NewArgon2Hasher(password.DefaultParams).Hash(secret)
	require.NoError(t, err)
	userID, orgID, membershipID := uuid.New(), uuid.New(), uuid.New()
	email := userID.String()[:8] + "@parceiro.ao"
	phone := fmt.Sprintf("+24493%07d", uuid.New().ID()%10_000_000)
	_, err = env.owner.Exec(ctx, `INSERT INTO organizations (id, type, name) VALUES ($1, 'partner', $2)`, orgID, "Parceiro "+userID.String()[:6])
	require.NoError(t, err)
	_, err = env.owner.Exec(ctx, `INSERT INTO users (id, name, email, phone, password, is_verified, status, role) VALUES ($1, 'Dono Parceiro', $2, $3, $4, true, 'active', 'staff')`, userID, email, phone, hash)
	require.NoError(t, err)
	_, err = env.owner.Exec(ctx, `INSERT INTO memberships (id, user_id, organization_id, status, activated_at) VALUES ($1, $2, $3, 'active', now())`, membershipID, userID, orgID)
	require.NoError(t, err)
	_, err = env.owner.Exec(ctx, `INSERT INTO membership_roles (membership_id, role_id) SELECT $1, id FROM roles WHERE organization_id IS NULL AND scope = 'partner' AND code = 'owner'`, membershipID)
	require.NoError(t, err)

	staff := &person{id: userID, client: partnerClient, api: env.api.baseURL}
	login := staff.post(t, "/api/v1/auth/login", map[string]string{"phone": phone, "password": secret})
	require.Equal(t, http.StatusOK, login.status, login.text)
	staff.token = login.str("access_token")
	return &partner{person: staff, organizationID: orgID.String()}
}

func vehicleBody(plate string) map[string]any {
	return map[string]any{"kind": "tow_truck", "brand": "Isuzu", "model": "NPR", "year": 2020, "color": "Branco", "plate": plate, "seats": 3, "capacity_kg": 3500}
}

func registerDriver(t *testing.T, owner *partner, serviceSlug, vehicleID string) *person {
	t.Helper()
	phone := fmt.Sprintf("+24493%07d", uuid.New().ID()%10_000_000)
	res := owner.post(t, "/api/v1/fleet/drivers", map[string]any{"name": "Motorista Frota", "phone": phone, "service_id": serviceBySlug(t, serviceSlug), "vehicle_id": vehicleID})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	userID := uuid.MustParse(res.str("user_id"))
	token, _, err := env.api.container.JWT.Generate(services.AccessTokenClaims{UserID: userID, SessionID: uuid.New(), Role: enums.RoleProvider, ClientID: providerClient})
	require.NoError(t, err)
	return &person{id: userID, token: token, client: providerClient, api: env.api.baseURL}
}

func TestAPartnerRunsItsFleetAndOnlyItsFleet(t *testing.T) {
	clearOnlineProviders(t)
	owner, stranger := newPartner(t), newPartner(t)
	plate := "LD-" + uuid.NewString()[:6]

	assert.Equal(t, http.StatusUnprocessableEntity, owner.post(t, "/api/v1/fleet/vehicles", map[string]any{"kind": "tow_truck"}).status, "required fields")
	big := vehicleBody("LD-BIG1")
	big["capacity_kg"] = 99_999_999_999
	assert.Equal(t, http.StatusUnprocessableEntity, owner.post(t, "/api/v1/fleet/vehicles", big).status, "over the column")
	unknown := vehicleBody("LD-BAD1")
	unknown["kind"] = "nave"
	assert.NotEqual(t, http.StatusCreated, owner.post(t, "/api/v1/fleet/vehicles", unknown).status, "unknown kind")

	vehicle := owner.post(t, "/api/v1/fleet/vehicles", vehicleBody(plate))
	require.Equal(t, http.StatusCreated, vehicle.status, vehicle.text)
	vehicleID := vehicle.str("id")
	assert.EqualValues(t, 3500, vehicle.num("capacity_kg"))
	var stored int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT capacity_kg FROM vehicles WHERE id = $1`, vehicleID).Scan(&stored))
	assert.Equal(t, 3500, stored)

	assert.Equal(t, http.StatusNotFound, stranger.get(t, "/api/v1/fleet/vehicles/"+vehicleID).status, "another partner does not see it")
	assert.Equal(t, http.StatusNotFound, stranger.do(t, http.MethodPut, "/api/v1/fleet/vehicles/"+vehicleID, vehicleBody(plate), nil).status)
	assert.Equal(t, http.StatusNotFound, stranger.do(t, http.MethodDelete, "/api/v1/fleet/vehicles/"+vehicleID, nil, nil).status)
	assert.Equal(t, http.StatusForbidden, newCustomer(t).get(t, "/api/v1/fleet/vehicles").status, "a customer has no fleet")
	assert.Equal(t, http.StatusForbidden, owner.get(t, "/api/v1/partners/"+stranger.organizationID+"/fleet/vehicles").status, "a partner cannot use the platform routes")

	driver := registerDriver(t, owner, "towing", vehicleID)
	assert.Equal(t, http.StatusNotFound, stranger.get(t, "/api/v1/fleet/drivers/"+driver.id.String()).status)
	assert.Equal(t, http.StatusNotFound, stranger.post(t, "/api/v1/fleet/drivers", map[string]any{"name": "X", "phone": "+244930000009", "service_id": serviceBySlug(t, "towing"), "vehicle_id": vehicleID}).status, "the vehicle of another partner")
	assert.Equal(t, http.StatusForbidden, driver.post(t, "/api/v1/providers/me/online", map[string]any{"position": positionAt(point{-8.84, 13.235})}).status, "not approved yet")

	inUse := owner.post(t, "/api/v1/fleet/drivers", map[string]any{"name": "Outro", "phone": "+244930000008", "service_id": serviceBySlug(t, "towing"), "vehicle_id": vehicleID})
	assert.Equal(t, "VEHICLE_NOT_AVAILABLE", inUse.code(), inUse.text)

	list := owner.get(t, "/api/v1/fleet/vehicles")
	require.Equal(t, http.StatusOK, list.status)
	assert.EqualValues(t, 1, list.num("total"), "only its own")
	admin := staffToken(t)
	assert.Equal(t, http.StatusOK, admin.get(t, "/api/v1/partners/"+owner.organizationID+"/fleet/vehicles").status)
	all := admin.get(t, "/api/v1/admin/vehicles?organization_id="+owner.organizationID)
	require.Equal(t, http.StatusOK, all.status, all.text)
	assert.EqualValues(t, 1, all.num("total"))
	assert.Equal(t, http.StatusNotFound, admin.get(t, "/api/v1/partners/"+uuid.NewString()+"/fleet/vehicles").status)
}

func TestAPartnerManagesWorkshopsWithValidatedFees(t *testing.T) {
	owner, stranger := newPartner(t), newPartner(t)
	body := map[string]any{"name": "Auto Kianda", "currency": "AOA", "offers_home_service": true, "home_service_fee_cents": 300_000, "address": map[string]any{"lat": -8.9, "lng": 13.2, "line": "Rua 12"}}

	negative := map[string]any{"name": "X", "currency": "AOA", "home_service_fee_cents": -5, "address": map[string]any{"lat": -8.9, "lng": 13.2, "line": "R"}}
	assert.Equal(t, http.StatusUnprocessableEntity, owner.post(t, "/api/v1/fleet/workshops", negative).status)
	outside := map[string]any{"name": "X", "currency": "AOA", "address": map[string]any{"lat": 900, "lng": 13.2, "line": "R"}}
	assert.NotEqual(t, http.StatusCreated, owner.post(t, "/api/v1/fleet/workshops", outside).status)

	workshop := owner.post(t, "/api/v1/fleet/workshops", body)
	require.Equal(t, http.StatusCreated, workshop.status, workshop.text)
	id := workshop.str("id")
	assert.Equal(t, http.StatusNotFound, stranger.get(t, "/api/v1/fleet/workshops/"+id).status)
	assert.Equal(t, http.StatusNotFound, stranger.post(t, "/api/v1/fleet/workshops/"+id+"/services", map[string]any{"name": "X", "price_cents": 1}).status)

	tooLong := owner.post(t, "/api/v1/fleet/workshops/"+id+"/services", map[string]any{"name": "Óleo", "price_cents": 1, "duration_minutes": 99_999_999_999})
	assert.Equal(t, http.StatusUnprocessableEntity, tooLong.status, "duration over the column: %s", tooLong.text)
	service := owner.post(t, "/api/v1/fleet/workshops/"+id+"/services", map[string]any{"name": "Óleo", "price_cents": 1_800_000, "duration_minutes": 45})
	require.Equal(t, http.StatusCreated, service.status, service.text)
	serviceID := service.str("id")

	got := owner.get(t, "/api/v1/fleet/workshops/"+id)
	require.Equal(t, http.StatusOK, got.status)
	assert.Len(t, got.body["services"], 1)
	assert.Equal(t, http.StatusNotFound, stranger.do(t, http.MethodDelete, "/api/v1/fleet/workshops/"+id+"/services/"+serviceID, nil, nil).status)
	require.Less(t, owner.do(t, http.MethodDelete, "/api/v1/fleet/workshops/"+id+"/services/"+serviceID, nil, nil).status, 300)
	require.Less(t, owner.do(t, http.MethodDelete, "/api/v1/fleet/workshops/"+id, nil, nil).status, 300)

	admin := staffToken(t)
	all := admin.get(t, "/api/v1/admin/workshops?organization_id="+owner.organizationID)
	require.Equal(t, http.StatusOK, all.status, all.text)
	assert.EqualValues(t, 1, all.num("total"))
}

func TestRequestReportsSeparateThePartnersAndHideTheCustomerPhone(t *testing.T) {
	clearOnlineProviders(t)
	owner, stranger := newPartner(t), newPartner(t)
	vehicle := owner.post(t, "/api/v1/fleet/vehicles", vehicleBody("LD-"+uuid.NewString()[:6]))
	require.Equal(t, http.StatusCreated, vehicle.status, vehicle.text)
	driver := registerDriver(t, owner, "towing", vehicle.str("id"))
	_, err := env.owner.Exec(context.Background(), `UPDATE providers SET status = 'approved', service_id = $2 WHERE user_id = $1`, driver.id, serviceBySlug(t, "towing"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/providers/me/online", map[string]any{"position": positionAt(point{-8.84, 13.235})}).status)
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	estimateID, serviceID := estimateFor(t, customer, "towing")
	created := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"towing": towingDetails(t, customer), "service_id": serviceID, "fare_estimate_id": estimateID, "pickup": pickup, "destination": target, "contact_phone": "+244923456789"}, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	offer := stream.waitFor(t, "request.offer", nil)
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, offer).status)
	for _, step := range []string{"en-route", "arrive", "start"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status, step)
	}
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/complete", map[string]any{"code": created.str("completion_code")}).status)

	var stored string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT organization_id FROM service_requests WHERE id = $1`, id).Scan(&stored))
	assert.Equal(t, owner.organizationID, stored, "the request belongs to the partner of the driver")

	mine := owner.get(t, "/api/v1/fleet/requests?category=towing")
	require.Equal(t, http.StatusOK, mine.status, mine.text)
	assert.EqualValues(t, 1, mine.num("total"))
	theirs := stranger.get(t, "/api/v1/fleet/requests")
	require.Equal(t, http.StatusOK, theirs.status)
	assert.EqualValues(t, 0, theirs.num("total"), "another partner sees nothing")
	assert.Equal(t, http.StatusNotFound, stranger.get(t, "/api/v1/fleet/requests/"+id).status)

	detail := owner.get(t, "/api/v1/fleet/requests/"+id)
	require.Equal(t, http.StatusOK, detail.status, detail.text)
	assert.NotContains(t, detail.text, "+244923456789", "a partner does not get the phone of the customer")
	assert.NotEmpty(t, detail.body["history"])

	admin := staffToken(t)
	assert.Equal(t, http.StatusForbidden, owner.get(t, "/api/v1/admin/requests").status, "a partner is not the backoffice")
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/admin/requests").status)
	filtered := admin.get(t, "/api/v1/admin/requests?category=towing&organization_id="+owner.organizationID+"&status=completed")
	require.Equal(t, http.StatusOK, filtered.status, filtered.text)
	assert.EqualValues(t, 1, filtered.num("total"))
	none := admin.get(t, "/api/v1/admin/requests?category=taxi&organization_id="+owner.organizationID)
	require.Equal(t, http.StatusOK, none.status)
	assert.EqualValues(t, 0, none.num("total"), "the filter really filters")
	assert.Equal(t, http.StatusBadRequest, admin.get(t, "/api/v1/admin/requests?from=ontem").status)
	assert.Equal(t, http.StatusNotFound, admin.get(t, "/api/v1/admin/requests/"+uuid.NewString()).status)
	assert.Equal(t, "INVALID_GROUPING", admin.get(t, "/api/v1/admin/requests/summary?group_by=x").code())

	summary := admin.get(t, "/api/v1/admin/requests/summary?group_by=status&organization_id="+owner.organizationID)
	require.Equal(t, http.StatusOK, summary.status, summary.text)
	groups := summary.body["groups"].([]any)
	require.Len(t, groups, 1)
	group := groups[0].(map[string]any)
	assert.Equal(t, "completed", group["key"])
	assert.EqualValues(t, created.num("fare_cents"), group["completed_fare_cents"], "the sum of the completed fares")
}

func TestRejectionReasonsAreValidatedAndScoped(t *testing.T) {
	admin := staffToken(t)
	customer := newCustomer(t)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/admin/rejection-reasons").status)

	code := fmt.Sprintf("qa_%d", uuid.New().ID())
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/rejection-reasons", map[string]any{"scope": "nada", "code": code, "label": "x"}).status, "unknown scope")
	tooBig := admin.post(t, "/api/v1/admin/rejection-reasons", map[string]any{"scope": "document", "code": code, "label": "x", "display_order": 99_999_999_999})
	assert.Equal(t, http.StatusUnprocessableEntity, tooBig.status, "over the column: %s", tooBig.text)
	created := admin.post(t, "/api/v1/admin/rejection-reasons", map[string]any{"scope": "document", "code": code, "label": "Documento rasgado"})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, http.StatusConflict, admin.post(t, "/api/v1/admin/rejection-reasons", map[string]any{"scope": "document", "code": code, "label": "Outro"}).status, "the code is unique per scope")

	pedro := signUpProvider(t, "Rejeitado Teste")
	fillProfile(t, pedro, comfortWithTariff(t), 4)
	for _, document := range allDocuments {
		pedro.sendDocument(t, document)
	}
	require.Equal(t, http.StatusOK, pedro.post(t, "/api/v1/providers/me/submit", nil).status)
	review := "/api/v1/admin/providers/" + pedro.id.String() + "/documents/id_card/review"

	assert.NotEqual(t, http.StatusOK, admin.post(t, review, map[string]any{"status": "rejected", "reason_ids": []string{uuid.NewString()}}).status, "a reason that does not exist")
	assert.NotEqual(t, http.StatusOK, admin.post(t, review, map[string]any{"status": "rejected", "reason_ids": []string{reasonID(t, admin, "provider", "invalid_documents")}}).status, "a reason of another scope")
	other := reasonID(t, admin, "document", "other")
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, review, map[string]any{"status": "rejected", "reason_ids": []string{other}}).status, "this reason needs a note")

	require.Equal(t, http.StatusOK, admin.do(t, http.MethodPut, "/api/v1/admin/rejection-reasons/"+created.str("id"), map[string]any{"label": "Documento rasgado", "is_active": false}, nil).status)
	assert.NotEqual(t, http.StatusOK, admin.post(t, review, map[string]any{"status": "rejected", "reason_ids": []string{created.str("id")}}).status, "an inactive reason")

	accepted := admin.post(t, review, map[string]any{"status": "rejected", "reason_ids": []string{other}, "note": "Foto cortada"})
	require.Equal(t, http.StatusOK, accepted.status, accepted.text)
	var stored string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT rejection_reason FROM provider_documents WHERE provider_id = $1 AND document_type = 'id_card'`, pedro.id).Scan(&stored))
	assert.Contains(t, stored, "Foto cortada")
}
