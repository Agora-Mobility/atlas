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

	verificationadapters "github.com/Agora-Mobility/atlas/infra/verification"
	verificationusecase "github.com/Agora-Mobility/atlas/usecases/verification"
	verificationdtos "github.com/Agora-Mobility/atlas/usecases/verification/dtos"
)

type driverCase struct {
	name       string
	service    string
	categories []string
	vehicle    map[string]any
}

func kycProvider(t *testing.T, name, service string) *person {
	t.Helper()
	provider := signUpProvider(t, name)
	profile := provider.do(t, http.MethodPut, "/api/v1/providers/me/profile", profileOf(serviceBySlug(t, service), name), nil)
	require.Equal(t, http.StatusOK, profile.status, profile.text)
	view := provider.get(t, verificationPath)
	require.Equal(t, http.StatusOK, view.status, view.text)
	return provider
}

func assertProgress(t *testing.T, p *person, completed, total int) {
	t.Helper()
	progress := p.get(t, verificationPath).body["progress"].(map[string]any)
	assert.EqualValues(t, completed, progress["completed"])
	assert.EqualValues(t, total, progress["total"])
}

func runDriverKYC(t *testing.T, driver *person, c driverCase) {
	t.Helper()
	view := driver.get(t, verificationPath)
	require.Equal(t, "driver", view.str("type"))

	driver.confirmIdentity(t, "provider")
	assertProgress(t, driver, 1, 8)
	driver.passSelfie(t, "provider")

	driver.sendPhotos(t, "driving_license", "provider", "front", "back")
	license := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": c.categories, "number": "LA-" + uuid.NewString()[:6], "expires_at": day(900)}, nil)
	require.Equal(t, http.StatusOK, license.status, license.text)

	require.Equal(t, "accepted", driver.sendFile(t, "criminal_record", "main", "", "application/pdf", pdfBytes).str("status"))
	record := driver.do(t, http.MethodPut, verificationPath+"/criminal-record", map[string]any{"certificate_number": "RC-" + uuid.NewString()[:6], "issued_at": day(-15)}, nil)
	require.Equal(t, http.StatusOK, record.status, record.text)
	assertProgress(t, driver, 4, 8)

	vehicle := driver.do(t, http.MethodPut, verificationPath+"/vehicle", c.vehicle, nil)
	require.Equal(t, http.StatusOK, vehicle.status, vehicle.text)
	driver.sendPhotos(t, "vehicle_photo", "", "front", "rear", "side", "interior", "plate")
	driver.sendPhotos(t, "vehicle_registration", "", "page_1", "page_2")
	registration := driver.do(t, http.MethodPut, verificationPath+"/vehicle/registration", map[string]string{"owner_name": ocrName}, nil)
	require.Equal(t, http.StatusOK, registration.status, registration.text)
	driver.sendPhotos(t, "vehicle_insurance", "", "main")
	require.Equal(t, "accepted", driver.sendFile(t, "vehicle_inspection", "main", "", "application/pdf", pdfBytes).str("status"))
	insurance := driver.do(t, http.MethodPut, verificationPath+"/vehicle/insurance", map[string]any{
		"insurer_id": catalogID(t, driver, "/api/v1/providers/catalogs/insurers", "name", "Fidelidade Angola"), "insurance_expires_at": day(365), "inspection_expires_at": day(180),
	}, nil)
	require.Equal(t, http.StatusOK, insurance.status, insurance.text)
	assertProgress(t, driver, 7, 8)

	driver.sendPhotos(t, "bank_account", "", "main")
	bank := driver.do(t, http.MethodPut, verificationPath+"/bank-account", map[string]any{
		"bank_id": catalogID(t, driver, "/api/v1/providers/catalogs/banks", "short_name", "BAI"), "iban": baiIBAN, "holder_name": ocrName,
	}, nil)
	require.Equal(t, http.StatusOK, bank.status, bank.text)
	assertProgress(t, driver, 8, 8)

	review := driver.get(t, verificationPath+"/review")
	require.Equal(t, http.StatusOK, review.status, review.text)
	require.Empty(t, review.body["missing_items"], review.text)
	submitted := driver.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
	require.Equal(t, http.StatusOK, submitted.status, submitted.text)
	assert.Equal(t, "submitted", driver.get(t, verificationPath).str("status"))
}

func approveAll(t *testing.T, admin, provider *person, service string, documents []string, representative bool) {
	t.Helper()
	path := "/api/v1/admin/providers/" + provider.id.String()
	view := admin.get(t, path+"/verification")
	require.Equal(t, http.StatusOK, view.status, view.text)
	for _, document := range documents {
		reviewed := admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved"})
		require.Equal(t, http.StatusOK, reviewed.status, document+": "+reviewed.text)
	}
	if representative {
		for _, document := range []string{"id_card", "selfie"} {
			reviewed := admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved", "subject": "legal_representative"})
			require.Equal(t, http.StatusOK, reviewed.status, document+": "+reviewed.text)
		}
	}
	approved := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": serviceBySlug(t, service)}, nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)
	assert.Equal(t, "approved", provider.get(t, verificationPath).str("status"))
}

var driverDocuments = []string{"id_card", "selfie", "driving_license", "criminal_record", "vehicle_photo", "vehicle_registration", "vehicle_insurance", "vehicle_inspection", "bank_account"}

func TestFiveProvidersOfDifferentServicesCompleteTheKYC(t *testing.T) {
	clearOnlineProviders(t)
	admin := staffToken(t)
	ctx := context.Background()

	towingID := serviceBySlug(t, "towing")
	towing := signUpProvider(t, "Rui Reboques")
	closed := towing.do(t, http.MethodPut, "/api/v1/providers/me/profile", profileOf(towingID, "Rui Reboques"), nil)
	require.Equal(t, "SERVICE_NOT_OPEN", closed.code(), "towing is closed to self sign-up until the business opens it")
	_, err := env.owner.Exec(ctx, `UPDATE service_provider_requirements SET self_signup = true WHERE service_id = $1`, towingID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.owner.Exec(context.Background(), `UPDATE service_provider_requirements SET self_signup = false WHERE service_id = $1`, towingID)
	})

	drivers := []driverCase{
		{name: "Ana Económico", service: "taxi", categories: []string{"B"},
			vehicle: map[string]any{"plate": "LD-11-22-AA", "brand": "Toyota", "model": "Yaris", "year": 2019, "color": "Branco", "seats": 4}},
		{name: "Bruno Conforto", service: "taxi-comfort", categories: []string{"B", "A"},
			vehicle: map[string]any{"plate": "LD-33-44-BB", "brand": "Toyota", "model": "Corolla", "year": 2023, "color": "Preto", "seats": 4}},
		{name: "Carla XL", service: "taxi-xl", categories: []string{"B"},
			vehicle: map[string]any{"plate": "LD-55-66-CC", "brand": "Toyota", "model": "Hiace", "year": 2021, "color": "Cinzento", "seats": 7, "kind": "van"}},
		{name: "Rui Reboques", service: "towing", categories: []string{"C1"},
			vehicle: map[string]any{"plate": "LD-77-88-DD", "brand": "Isuzu", "model": "NPR", "year": 2018, "color": "Amarelo", "seats": 2, "kind": "tow_truck", "capacity_kg": 3500}},
	}

	for _, c := range drivers {
		t.Run(c.name+" ("+c.service+")", func(t *testing.T) {
			driver := towing
			if c.service == "towing" {
				profile := towing.do(t, http.MethodPut, "/api/v1/providers/me/profile", profileOf(towingID, c.name), nil)
				require.Equal(t, http.StatusOK, profile.status, profile.text)
			} else {
				driver = kycProvider(t, c.name, c.service)
			}
			if c.service == "towing" {
				wrong := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": []string{"B"}, "number": "LA-1", "expires_at": day(900)}, nil)
				assert.Equal(t, "DRIVING_CATEGORY_REQUIRED", wrong.code(), "towing needs C1 or C")
			}
			runDriverKYC(t, driver, c)
			approveAll(t, admin, driver, c.service, driverDocuments, false)

			online := driver.post(t, "/api/v1/providers/me/online", map[string]any{"position": positionAt(pickup)})
			require.Equal(t, http.StatusOK, online.status, online.text)
			var stored string
			require.NoError(t, env.owner.QueryRow(ctx, `SELECT s.slug FROM providers p JOIN services s ON s.id = p.service_id WHERE p.user_id = $1`, driver.id).Scan(&stored))
			assert.Equal(t, c.service, stored)
		})
	}

	t.Run("Daniel Oficina (maintenance)", func(t *testing.T) {
		workshop := kycProvider(t, "Daniel Oficina", "maintenance")
		require.Equal(t, "workshop", workshop.get(t, verificationPath).str("type"))
		nif := fmt.Sprintf("54%08d", uuid.New().ID()%100_000_000)
		company := "Oficina Daniel Lda"

		require.Equal(t, "accepted", workshop.sendFile(t, "business_license", "main", "", "application/pdf", pdfBytes).str("status"))
		require.Equal(t, http.StatusOK, workshop.do(t, http.MethodPut, verificationPath+"/workshop/business-license", map[string]any{"number": "ALV-9", "expires_at": day(500), "company_name": company}, nil).status)
		require.Equal(t, "accepted", workshop.sendFile(t, "commercial_certificate", "main", "", "application/pdf", pdfBytes).str("status"))
		require.Equal(t, http.StatusOK, workshop.do(t, http.MethodPut, verificationPath+"/workshop/commercial-certificate", map[string]any{"code": "C-9", "issued_at": day(-20)}, nil).status)
		taxID := workshop.do(t, http.MethodPut, verificationPath+"/workshop/tax-id", map[string]any{"nif": nif, "company_name": company, "company_type": "limited_company"}, nil)
		require.Equal(t, http.StatusOK, taxID.status, taxID.text)
		assertProgress(t, workshop, 3, 8)

		workshop.confirmIdentity(t, "legal_representative")
		workshop.passSelfie(t, "legal_representative")
		workshop.sendPhotos(t, "workshop_photo", "", "facade", "work_area", "equipment", "reception")
		location := workshop.do(t, http.MethodPut, verificationPath+"/workshop/location", map[string]any{
			"address": "Rua Major Kanhangulo, 12, Luanda", "lat": pickup.Lat, "lng": pickup.Lng,
			"opening_hours": []map[string]any{{"day": 1, "open": "08:00", "close": "18:00"}, {"day": 2, "open": "08:00", "close": "18:00"}, {"day": 0, "closed": true}},
		}, nil)
		require.Equal(t, http.StatusOK, location.status, location.text)
		services := workshop.do(t, http.MethodPut, verificationPath+"/workshop/services", map[string]any{"services": []map[string]any{
			{"service_id": catalogID(t, workshop, "/api/v1/providers/catalogs/workshop-services", "code", "oil"), "base_price": 12000},
			{"service_id": catalogID(t, workshop, "/api/v1/providers/catalogs/workshop-services", "code", "brakes"), "base_price": 25000},
		}}, nil)
		require.Equal(t, http.StatusOK, services.status, services.text)
		workshop.sendPhotos(t, "bank_account", "", "main")
		bank := workshop.do(t, http.MethodPut, verificationPath+"/workshop/bank-account", map[string]any{
			"bank_id": catalogID(t, workshop, "/api/v1/providers/catalogs/banks", "short_name", "BAI"), "iban": baiIBAN, "holder_name": company,
		}, nil)
		require.Equal(t, http.StatusOK, bank.status, bank.text)
		assertProgress(t, workshop, 8, 8)

		submitted := workshop.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
		require.Equal(t, http.StatusOK, submitted.status, submitted.text)
		approveAll(t, admin, workshop, "maintenance", []string{"business_license", "commercial_certificate", "tax_id", "workshop_photo", "bank_account"}, true)

		search := newCustomer(t).get(t, fmt.Sprintf("/api/v1/workshops?lat=%f&lng=%f&search=Daniel", pickup.Lat, pickup.Lng))
		require.Equal(t, http.StatusOK, search.status, search.text)
		assert.Contains(t, search.text, company, "the approved workshop shows up in the customer search")
	})

	t.Run("each provider sees only its own files", func(t *testing.T) {
		var files, owners int
		require.NoError(t, env.owner.QueryRow(ctx, `
			SELECT count(*), count(DISTINCT provider_id) FROM provider_document_files
			WHERE url NOT LIKE 'documents/' || provider_id::text || '/%' AND url LIKE 'documents/%'`).Scan(&files, &owners))
		assert.Zero(t, files, "every key sits under the folder of its provider")
	})
}

func TestWithTheProductionAdaptersTheTeamReviewsTheCardAndTheSelfie(t *testing.T) {
	ctx := context.Background()
	driver := kycProvider(t, "Eva Produção", "taxi")
	driver.sendPhotos(t, "id_card", "", "front", "back")

	deps := env.api.container.Verification.Usecase.Deps
	deps.OCR = verificationadapters.NewOCRProvider(verificationadapters.ModeManual, verificationadapters.FakeSettings{})
	deps.Liveness = verificationadapters.NewLivenessProvider(verificationadapters.ModeManual, verificationadapters.FakeSettings{})
	deps.Quality = verificationadapters.NewQualityChecker(verificationadapters.ModeManual, verificationadapters.FakeSettings{})
	production := verificationusecase.NewUsecase(deps)

	var documentID uuid.UUID
	require.NoError(t, env.owner.QueryRow(ctx, `
		UPDATE provider_documents SET details = jsonb_set(details, '{extraction}', '{"status":"processing","attempts":1}')
		WHERE provider_id = $1 AND document_type = 'id_card' RETURNING id`, driver.id).Scan(&documentID))
	require.NoError(t, production.ReadIdentity(ctx, documentID))

	extraction := driver.get(t, verificationPath+"/identity-card/extraction")
	assert.Equal(t, "failed", extraction.str("status"))
	assert.Equal(t, "unavailable", extraction.str("failure"))
	assert.Equal(t, true, extraction.body["manual_entry_allowed"], "without OCR the app goes straight to the manual form")
	manual := ocrIdentity("provider")
	manual["source"] = "manual"
	confirmed := driver.do(t, http.MethodPut, verificationPath+"/identity-card/data", manual, nil)
	require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
	assert.Equal(t, "needs_review", confirmed.str("status"))

	session, err := production.StartLiveness(ctx, driver.id, verificationdtos.SubjectInput{})
	require.NoError(t, err)
	photo := driver.sendFile(t, "selfie", "main", "", "image/jpeg", photoJPEG)
	selfie, err := production.SubmitSelfie(ctx, driver.id, verificationdtos.SelfieInput{SessionID: session.SessionID, UploadID: uuid.MustParse(photo.str("id"))})
	require.NoError(t, err)
	assert.Equal(t, "needs_review", selfie.Status, "without a liveness vendor the selfie goes to the team")
	assert.Equal(t, "needs_review", itemStatus(t, driver, "selfie"))
	assertProgress(t, driver, 2, 8)
}
