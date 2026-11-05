//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	verificationPath = "/api/v1/providers/me/verification"
	baiIBAN          = "AO06 0040 0000 1234 5678 1014 2"
	bfaIBAN          = "AO06000600001234567810173"
	ocrName          = "Domingos Mateus Kiala"
)

var (
	pdfBytes  = append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{'x'}, 512)...)
	photoJPEG = encodedPhoto()
)

func encodedPhoto() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := range 64 {
		img.Set(x, x%48, color.RGBA{R: 180, G: 40, A: 255})
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func photoWith(marker string) []byte {
	return append(append([]byte{}, photoJPEG...), []byte(marker)...)
}

func day(offset int) string {
	return time.Now().UTC().AddDate(0, 0, offset).Format(time.DateOnly)
}

func errorData(r result) map[string]any {
	failure, _ := r.body["error"].(map[string]any)
	data, _ := failure["data"].(map[string]any)
	return data
}

func errorDetails(r result) map[string]any {
	failure, _ := r.body["error"].(map[string]any)
	details, _ := failure["details"].(map[string]any)
	return details
}

func verifyingProvider(t *testing.T, slug string) *person {
	t.Helper()
	provider := signUpProvider(t, "Prestador "+uuid.NewString()[:6])
	profile := provider.do(t, http.MethodPut, "/api/v1/providers/me/profile", profileOf(serviceBySlug(t, slug), "Domingos Kiala"), nil)
	require.Equal(t, http.StatusOK, profile.status, profile.text)
	return provider
}

func (p *person) startUpload(t *testing.T, documentType, slot, subject, contentType string, size int) result {
	t.Helper()
	return p.post(t, verificationPath+"/uploads", map[string]any{"document_type": documentType, "slot": slot, "subject": subject, "content_type": contentType, "size": size})
}

func putToStorage(t *testing.T, started result, contentType string, content []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, started.str("upload_url"), bytes.NewReader(content))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
}

func (p *person) processed(t *testing.T, uploadID string) result {
	t.Helper()
	var last result
	require.Eventually(t, func() bool {
		last = p.get(t, verificationPath+"/uploads/"+uploadID)
		return last.str("status") == "accepted" || last.str("status") == "quality_failed"
	}, 15*time.Second, 50*time.Millisecond, "the file was never processed")
	return last
}

func (p *person) sendFile(t *testing.T, documentType, slot, subject, contentType string, content []byte) result {
	t.Helper()
	started := p.startUpload(t, documentType, slot, subject, contentType, len(content))
	require.Equal(t, http.StatusCreated, started.status, started.text)
	putToStorage(t, started, contentType, content)
	confirmed := p.post(t, verificationPath+"/uploads/"+started.str("upload_id")+"/confirm", nil)
	require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
	return p.processed(t, started.str("upload_id"))
}

func (p *person) sendPhotos(t *testing.T, documentType, subject string, slots ...string) {
	t.Helper()
	for _, slot := range slots {
		require.Equal(t, "accepted", p.sendFile(t, documentType, slot, subject, "image/jpeg", photoJPEG).str("status"), documentType+"/"+slot)
	}
}

func (p *person) readIdentity(t *testing.T, subject string) result {
	t.Helper()
	started := p.post(t, verificationPath+"/identity-card/extraction", map[string]string{"subject": subject})
	require.Equal(t, http.StatusAccepted, started.status, started.text)
	var last result
	require.Eventually(t, func() bool {
		last = p.get(t, verificationPath+"/identity-card/extraction?subject="+subject)
		return last.str("status") == "completed" || last.str("status") == "failed"
	}, 15*time.Second, 50*time.Millisecond)
	return last
}

func ocrIdentity(subject string) map[string]any {
	return map[string]any{
		"subject": subject, "source": "ocr", "full_name": ocrName, "number": "005487123LA041", "birth_date": "1988-03-14", "issued_at": "2022-01-10", "expires_at": "2032-01-10",
	}
}

func (p *person) confirmIdentity(t *testing.T, subject string) {
	t.Helper()
	p.sendPhotos(t, "id_card", subject, "front", "back")
	require.Equal(t, "completed", p.readIdentity(t, subject).str("status"))
	confirmed := p.do(t, http.MethodPut, verificationPath+"/identity-card/data", ocrIdentity(subject), nil)
	require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
}

func (p *person) passSelfie(t *testing.T, subject string) {
	t.Helper()
	session := p.post(t, verificationPath+"/selfie/session", map[string]string{"subject": subject})
	require.Equal(t, http.StatusCreated, session.status, session.text)
	assert.Equal(t, "BLINK", session.str("challenge"))
	photo := p.sendFile(t, "selfie", "main", subject, "image/jpeg", photoJPEG)
	selfie := p.post(t, verificationPath+"/selfie", map[string]string{"subject": subject, "session_id": session.str("session_id"), "upload_id": photo.str("id")})
	require.Equal(t, http.StatusOK, selfie.status, selfie.text)
	assert.Equal(t, "accepted", selfie.str("status"))
}

func catalogID(t *testing.T, p *person, path, field, value string) string {
	t.Helper()
	list := p.get(t, path)
	require.Equal(t, http.StatusOK, list.status, list.text)
	for _, raw := range list.body["items"].([]any) {
		item := raw.(map[string]any)
		if item[field] == value {
			return item["id"].(string)
		}
	}
	require.Failf(t, "catalog item not found", "%s=%s", field, value)
	return ""
}

func itemStatus(t *testing.T, p *person, key string) string {
	t.Helper()
	view := p.get(t, verificationPath)
	require.Equal(t, http.StatusOK, view.status, view.text)
	for _, rawGroup := range view.body["groups"].([]any) {
		for _, rawItem := range rawGroup.(map[string]any)["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["key"] == key {
				return item["status"].(string)
			}
		}
	}
	return ""
}

func TestADriverGoesThroughTheWholeVerification(t *testing.T) {
	driver := verifyingProvider(t, "taxi")
	admin := staffToken(t)

	t.Run("the step list starts empty and comes from the server", func(t *testing.T) {
		view := driver.get(t, verificationPath)
		require.Equal(t, http.StatusOK, view.status, view.text)
		assert.Equal(t, "driver", view.str("type"))
		assert.Equal(t, "in_progress", view.str("status"))
		assert.Equal(t, "identity_card", view.str("next_item"))
		progress := view.body["progress"].(map[string]any)
		assert.EqualValues(t, 8, progress["total"])
		assert.EqualValues(t, 0, progress["percent"])
		assert.Len(t, view.body["groups"].([]any), 3)
	})

	t.Run("after the identity card the progress is twelve percent", func(t *testing.T) {
		driver.confirmIdentity(t, "provider")
		assert.Equal(t, "completed", itemStatus(t, driver, "identity_card"))
		view := driver.get(t, verificationPath)
		assert.EqualValues(t, 12, view.body["progress"].(map[string]any)["percent"])
		assert.Equal(t, "selfie", view.str("next_item"))
		assert.EqualValues(t, 10, view.num("estimated_minutes_left"))
		var stored string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id_number FROM providers WHERE user_id = $1`, driver.id).Scan(&stored))
		assert.Equal(t, "005487123LA041", stored, "the card number is the reference")
	})

	t.Run("every other step completes", func(t *testing.T) {
		driver.passSelfie(t, "provider")
		driver.sendPhotos(t, "driving_license", "provider", "front", "back")
		license := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": []string{"B"}, "number": "LA-123456", "expires_at": day(800)}, nil)
		require.Equal(t, http.StatusOK, license.status, license.text)

		require.Equal(t, "accepted", driver.sendFile(t, "criminal_record", "main", "", "application/pdf", pdfBytes).str("status"))
		record := driver.do(t, http.MethodPut, verificationPath+"/criminal-record", map[string]any{"certificate_number": "RC-2026-1", "issued_at": day(-10)}, nil)
		require.Equal(t, http.StatusOK, record.status, record.text)

		vehicle := driver.do(t, http.MethodPut, verificationPath+"/vehicle", map[string]any{"plate": "ld452 1af", "brand": "Toyota", "model": "Corolla", "year": 2020, "color": "Branco", "seats": 5}, nil)
		require.Equal(t, http.StatusOK, vehicle.status, vehicle.text)
		driver.sendPhotos(t, "vehicle_photo", "", "front", "rear", "side", "interior", "plate")
		driver.sendPhotos(t, "vehicle_registration", "", "page_1", "page_2")
		require.Equal(t, http.StatusOK, driver.do(t, http.MethodPut, verificationPath+"/vehicle/registration", map[string]string{"owner_name": ocrName}, nil).status)
		driver.sendPhotos(t, "vehicle_insurance", "", "main")
		require.Equal(t, "accepted", driver.sendFile(t, "vehicle_inspection", "main", "", "application/pdf", pdfBytes).str("status"))
		insurance := driver.do(t, http.MethodPut, verificationPath+"/vehicle/insurance", map[string]any{
			"insurer_id": catalogID(t, driver, "/api/v1/providers/catalogs/insurers", "name", "ENSA"), "insurance_expires_at": day(20), "inspection_expires_at": day(200),
		}, nil)
		require.Equal(t, http.StatusOK, insurance.status, insurance.text)
		driver.sendPhotos(t, "bank_account", "", "main")
		bank := driver.do(t, http.MethodPut, verificationPath+"/bank-account", map[string]any{
			"bank_id": catalogID(t, driver, "/api/v1/providers/catalogs/banks?search=0040", "short_name", "BAI"), "iban": baiIBAN, "holder_name": ocrName,
		}, nil)
		require.Equal(t, http.StatusOK, bank.status, bank.text)
		assert.Equal(t, "AO06 •••• 1014 2", bank.str("iban"))
	})

	t.Run("the review shows eight of eight with the IBAN masked", func(t *testing.T) {
		review := driver.get(t, verificationPath+"/review")
		require.Equal(t, http.StatusOK, review.status, review.text)
		assert.Empty(t, review.body["missing_items"])
		documents := review.body["documents"].(map[string]any)
		assert.EqualValues(t, 8, documents["sent"])
		assert.EqualValues(t, 8, documents["total"])
		assert.Contains(t, review.text, "AO06 •••• 1014 2")
		assert.NotContains(t, review.text, "005487123LA041", "the card number is masked")
	})

	t.Run("submitting needs the declaration, locks the edits and can be repeated", func(t *testing.T) {
		refused := driver.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": false})
		assert.Equal(t, http.StatusUnprocessableEntity, refused.status)
		assert.Equal(t, "DECLARATION_REQUIRED", refused.code())

		submitted := driver.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
		require.Equal(t, http.StatusOK, submitted.status, submitted.text)
		assert.Equal(t, "submitted", submitted.str("status"))
		again := driver.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
		assert.Equal(t, http.StatusOK, again.status, "submitting twice is harmless")

		locked := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": []string{"B"}, "number": "LA-1", "expires_at": day(800)}, nil)
		assert.Equal(t, http.StatusConflict, locked.status)
		assert.Equal(t, "VERIFICATION_LOCKED", locked.code())
		assert.Equal(t, "VERIFICATION_LOCKED", driver.startUpload(t, "selfie", "main", "", "image/jpeg", 100).code())
		assert.Equal(t, "pending", driver.get(t, "/api/v1/providers/me/profile").str("status"))
	})

	t.Run("the backoffice sees the files through short links and the view is audited", func(t *testing.T) {
		view := admin.get(t, "/api/v1/admin/providers/"+driver.id.String()+"/verification")
		require.Equal(t, http.StatusOK, view.status, view.text)
		documents := view.body["documents"].([]any)
		require.NotEmpty(t, documents)
		files := documents[0].(map[string]any)["files"].([]any)
		require.NotEmpty(t, files)
		link, err := http.Get(files[0].(map[string]any)["url"].(string))
		require.NoError(t, err)
		_ = link.Body.Close()
		assert.Equal(t, http.StatusOK, link.StatusCode)
		var viewed int
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'provider_document.view' AND after->>'provider_id' = $1`, driver.id.String()).Scan(&viewed))
		assert.Equal(t, 1, viewed)
		assert.Equal(t, http.StatusForbidden, driver.get(t, "/api/v1/admin/providers/"+driver.id.String()+"/verification").status)
	})

	t.Run("approved after every document is reviewed", func(t *testing.T) {
		path := "/api/v1/admin/providers/" + driver.id.String()
		for _, document := range []string{"id_card", "selfie", "driving_license", "criminal_record", "vehicle_photo", "vehicle_registration", "vehicle_insurance", "vehicle_inspection", "bank_account"} {
			reviewed := admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved"})
			require.Equal(t, http.StatusOK, reviewed.status, document+": "+reviewed.text)
		}
		approved := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": serviceBySlug(t, "taxi")}, nil)
		require.Equal(t, http.StatusOK, approved.status, approved.text)
		assert.Equal(t, "approved", driver.get(t, verificationPath).str("status"))
	})

	t.Run("an insurance that expires within thirty days is announced once", func(t *testing.T) {
		require.NoError(t, env.api.container.Verification.Usecase.Housekeep(context.Background()))
		require.NoError(t, env.api.container.Verification.Usecase.Housekeep(context.Background()))
		var notices int
		require.NoError(t, env.owner.QueryRow(context.Background(), `
			SELECT count(*) FROM verification_reminders r JOIN provider_documents d ON d.id = r.document_id
			WHERE r.provider_id = $1 AND r.kind = 'document_expiry' AND d.document_type = 'vehicle_insurance' AND r.sent_at IS NOT NULL`, driver.id).Scan(&notices))
		assert.Equal(t, 1, notices)
	})
}

func TestUploadsAreCheckedAndBelongToTheirOwner(t *testing.T) {
	driver := verifyingProvider(t, "taxi")
	stranger := verifyingProvider(t, "taxi")

	t.Run("type, size and slot are checked before signing", func(t *testing.T) {
		assert.Equal(t, "INVALID_FILE_TYPE", driver.startUpload(t, "id_card", "front", "", "application/pdf", 100).code(), "the card sides are photos")
		assert.Equal(t, "INVALID_FILE_TYPE", driver.startUpload(t, "id_card", "front", "", "image/gif", 100).code())
		assert.Equal(t, "FILE_TOO_LARGE", driver.startUpload(t, "id_card", "front", "", "image/jpeg", 11*1024*1024).code())
		assert.Equal(t, "INVALID_SLOT", driver.startUpload(t, "id_card", "page_1", "", "image/jpeg", 100).code())
		assert.Equal(t, "DOCUMENT_NOT_IN_PLAN", driver.startUpload(t, "workshop_photo", "facade", "", "image/jpeg", 100).code())
		assert.Equal(t, "STEP_NOT_IN_PLAN", driver.do(t, http.MethodPut, verificationPath+"/workshop/tax-id", map[string]string{"nif": "5417123456", "company_name": "X", "company_type": "limited_company"}, nil).code())
	})

	t.Run("the content must match the declared type", func(t *testing.T) {
		started := driver.startUpload(t, "id_card", "front", "", "image/jpeg", len(pdfBytes))
		require.Equal(t, http.StatusCreated, started.status, started.text)
		putToStorage(t, started, "image/jpeg", pdfBytes)
		confirmed := driver.post(t, verificationPath+"/uploads/"+started.str("upload_id")+"/confirm", nil)
		assert.Equal(t, "INVALID_FILE_TYPE", confirmed.code())

		missing := driver.startUpload(t, "id_card", "front", "", "image/jpeg", 100)
		assert.Equal(t, "FILE_NOT_UPLOADED", driver.post(t, verificationPath+"/uploads/"+missing.str("upload_id")+"/confirm", nil).code())
	})

	t.Run("a blurry photo fails the quality check with the reasons", func(t *testing.T) {
		blurry := driver.sendFile(t, "id_card", "front", "", "image/jpeg", photoWith("FAKE:BLURRY FAKE:GLARE"))
		assert.Equal(t, "quality_failed", blurry.str("status"))
		assert.ElementsMatch(t, []any{"BLURRY", "GLARE"}, blurry.body["reasons"])
		quality := blurry.body["quality"].(map[string]any)
		assert.Equal(t, false, quality["sharp"])
		assert.Equal(t, true, quality["text_legible"])
		assert.Equal(t, "in_progress", itemStatus(t, driver, "identity_card"))
	})

	t.Run("taking another photo supersedes the old one, which is kept", func(t *testing.T) {
		first := driver.sendFile(t, "id_card", "front", "", "image/jpeg", photoJPEG)
		second := driver.sendFile(t, "id_card", "front", "", "image/jpeg", photoJPEG)
		assert.Greater(t, second.num("version"), first.num("version"))
		var status string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT status FROM provider_document_files WHERE id = $1`, first.str("id")).Scan(&status))
		assert.Equal(t, "superseded", status)
		var sha string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT sha256 FROM provider_document_files WHERE id = $1`, second.str("id")).Scan(&sha))
		assert.Len(t, sha, 64)
		assert.Contains(t, driver.get(t, verificationPath+"/uploads/"+second.str("id")+"/url").str("url"), "/documents/"+driver.id.String()+"/")
	})

	t.Run("another provider cannot see or use the file", func(t *testing.T) {
		upload := driver.sendFile(t, "selfie", "main", "", "image/jpeg", photoJPEG)
		id := upload.str("id")
		assert.Equal(t, http.StatusNotFound, stranger.get(t, verificationPath+"/uploads/"+id).status)
		assert.Equal(t, http.StatusNotFound, stranger.get(t, verificationPath+"/uploads/"+id+"/url").status)
		assert.Equal(t, http.StatusNotFound, stranger.post(t, verificationPath+"/uploads/"+id+"/confirm", nil).status)
		session := stranger.post(t, verificationPath+"/selfie/session", map[string]string{})
		require.Equal(t, http.StatusCreated, session.status, session.text)
		assert.Equal(t, http.StatusNotFound, stranger.post(t, verificationPath+"/selfie", map[string]string{"session_id": session.str("session_id"), "upload_id": id}).status)
	})

	t.Run("only providers have a verification", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, newCustomer(t).get(t, verificationPath).status)
	})

	t.Run("an orphan upload is dropped after a day", func(t *testing.T) {
		orphan := driver.startUpload(t, "id_card", "back", "", "image/jpeg", 100)
		_, err := env.owner.Exec(context.Background(), `UPDATE provider_document_files SET created_at = now() - interval '25 hours' WHERE id = $1`, orphan.str("upload_id"))
		require.NoError(t, err)
		require.NoError(t, env.api.container.Verification.Usecase.Housekeep(context.Background()))
		assert.Equal(t, http.StatusNotFound, driver.get(t, verificationPath+"/uploads/"+orphan.str("upload_id")).status)
	})
}

func TestIdentityCardReadingAndItsErrors(t *testing.T) {
	t.Run("the reading needs both sides", func(t *testing.T) {
		driver := verifyingProvider(t, "taxi")
		assert.Equal(t, "DOCUMENT_FILES_MISSING", driver.post(t, verificationPath+"/identity-card/extraction", nil).code())
	})

	t.Run("manual entry only after three failed readings, and then it needs review", func(t *testing.T) {
		driver := verifyingProvider(t, "taxi")
		unreadable := photoWith("FAKE:OCR_FAIL")
		require.Equal(t, "accepted", driver.sendFile(t, "id_card", "front", "", "image/jpeg", unreadable).str("status"))
		driver.sendPhotos(t, "id_card", "", "back")
		manual := ocrIdentity("provider")
		manual["source"] = "manual"
		assert.Equal(t, "MANUAL_ENTRY_NOT_ALLOWED", driver.do(t, http.MethodPut, verificationPath+"/identity-card/data", manual, nil).code())
		for attempt := 1; attempt <= 3; attempt++ {
			read := driver.readIdentity(t, "provider")
			assert.Equal(t, "failed", read.str("status"))
			assert.EqualValues(t, attempt, read.num("attempts"))
		}
		blocked := driver.post(t, verificationPath+"/identity-card/extraction", nil)
		assert.Equal(t, "EXTRACTION_MAX_ATTEMPTS", blocked.code())
		assert.EqualValues(t, 3, errorData(blocked)["attempts"])
		assert.Equal(t, true, driver.get(t, verificationPath+"/identity-card/extraction").body["manual_entry_allowed"])

		confirmed := driver.do(t, http.MethodPut, verificationPath+"/identity-card/data", manual, nil)
		require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
		assert.Equal(t, "needs_review", confirmed.str("status"))
	})

	t.Run("fields edited after the reading go to review", func(t *testing.T) {
		driver := verifyingProvider(t, "taxi")
		driver.sendPhotos(t, "id_card", "", "front", "back")
		require.Equal(t, "completed", driver.readIdentity(t, "provider").str("status"))
		edited := ocrIdentity("provider")
		edited["full_name"] = "Domingos M. Kiala"
		confirmed := driver.do(t, http.MethodPut, verificationPath+"/identity-card/data", edited, nil)
		require.Equal(t, http.StatusOK, confirmed.status, confirmed.text)
		assert.Equal(t, "needs_review", confirmed.str("status"))
		var fields string
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT details->>'manually_edited_fields' FROM provider_documents WHERE provider_id = $1 AND document_type = 'id_card'`, driver.id).Scan(&fields))
		assert.Equal(t, `["full_name"]`, fields)
	})

	t.Run("format, dates and an expired card", func(t *testing.T) {
		driver := verifyingProvider(t, "taxi")
		driver.sendPhotos(t, "id_card", "", "front", "back")
		require.Equal(t, "completed", driver.readIdentity(t, "provider").str("status"))
		put := func(change map[string]any) result {
			data := ocrIdentity("provider")
			for key, value := range change {
				data[key] = value
			}
			return driver.do(t, http.MethodPut, verificationPath+"/identity-card/data", data, nil)
		}
		badNumber := put(map[string]any{"number": "12345"})
		assert.Equal(t, "VALIDATION_ERROR", badNumber.code())
		assert.Contains(t, errorDetails(badNumber), "number")
		assert.Contains(t, errorDetails(put(map[string]any{"expires_at": "2021-01-01"})), "expires_at", "valid until after issued")
		assert.Equal(t, "PROVIDER_UNDER_AGE", put(map[string]any{"birth_date": day(-365 * 10)}).code())

		expired := put(map[string]any{"issued_at": "2014-01-10", "expires_at": "2024-01-10"})
		assert.Equal(t, "IDENTITY_CARD_EXPIRED", expired.code())
		assert.Equal(t, "2024-01-10", errorData(expired)["expires_at"])

		require.Equal(t, "accepted", driver.sendFile(t, "id_card", "renewal_receipt", "", "application/pdf", pdfBytes).str("status"))
		withReceipt := put(map[string]any{"issued_at": "2014-01-10", "expires_at": "2024-01-10"})
		require.Equal(t, http.StatusOK, withReceipt.status, withReceipt.text)
		assert.Equal(t, "needs_review", withReceipt.str("status"))
	})
}

func TestSelfieIsLivenessOnlyWithThreeAttempts(t *testing.T) {
	driver := verifyingProvider(t, "taxi")
	session := driver.post(t, verificationPath+"/selfie/session", nil)
	require.Equal(t, http.StatusCreated, session.status, session.text)
	failing := driver.sendFile(t, "selfie", "main", "", "image/jpeg", photoWith("FAKE:LIVENESS_FAIL"))
	send := func() result {
		return driver.post(t, verificationPath+"/selfie", map[string]string{"session_id": session.str("session_id"), "upload_id": failing.str("id")})
	}
	for attempt := 1; attempt <= 2; attempt++ {
		failed := send()
		assert.Equal(t, "LIVENESS_FAILED", failed.code())
		assert.EqualValues(t, attempt, errorData(failed)["attempts"])
		assert.EqualValues(t, 3, errorData(failed)["max_attempts"])
	}
	third := send()
	require.Equal(t, http.StatusOK, third.status, third.text)
	assert.Equal(t, "needs_review", third.str("status"))
	assert.Equal(t, "needs_review", itemStatus(t, driver, "selfie"), "after three failures it counts as sent for manual review")
	assert.Equal(t, "LIVENESS_SESSION_INVALID", driver.post(t, verificationPath+"/selfie", map[string]string{"session_id": "other", "upload_id": failing.str("id")}).code())
}

func TestDriverStepRules(t *testing.T) {
	driver := verifyingProvider(t, "taxi")

	t.Run("driving licence category and validity", func(t *testing.T) {
		wrong := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": []string{"A"}, "number": "LA-1", "expires_at": day(300)}, nil)
		assert.Equal(t, "DRIVING_CATEGORY_REQUIRED", wrong.code())
		assert.Equal(t, []any{"B"}, errorData(wrong)["required_any_of"])
		expired := driver.do(t, http.MethodPut, verificationPath+"/driving-license", map[string]any{"categories": []string{"B"}, "number": "LA-1", "expires_at": day(-1)}, nil)
		assert.Equal(t, "DOCUMENT_EXPIRED", expired.code())
		assert.Contains(t, errorDetails(expired), "expires_at")
	})

	t.Run("criminal record of at most ninety days and a reminder for tomorrow", func(t *testing.T) {
		old := driver.do(t, http.MethodPut, verificationPath+"/criminal-record", map[string]any{"certificate_number": "RC-1", "issued_at": day(-91)}, nil)
		assert.Equal(t, "CERTIFICATE_TOO_OLD", old.code())
		future := driver.do(t, http.MethodPut, verificationPath+"/criminal-record", map[string]any{"certificate_number": "RC-1", "issued_at": day(1)}, nil)
		assert.Contains(t, errorDetails(future), "issued_at")

		reminder := driver.post(t, verificationPath+"/criminal-record/reminder", nil)
		require.Equal(t, http.StatusOK, reminder.status, reminder.text)
		remindAt, err := time.Parse(time.RFC3339Nano, reminder.str("remind_at"))
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(24*time.Hour), remindAt, time.Minute)
		_, err = env.owner.Exec(context.Background(), `UPDATE verification_reminders SET due_at = now() - interval '1 minute' WHERE provider_id = $1`, driver.id)
		require.NoError(t, err)
		require.NoError(t, env.api.container.Verification.Usecase.Housekeep(context.Background()))
		var sent bool
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM verification_reminders WHERE provider_id = $1 AND kind = 'criminal_record'`, driver.id).Scan(&sent))
		assert.True(t, sent)
	})

	t.Run("plate format", func(t *testing.T) {
		bad := driver.do(t, http.MethodPut, verificationPath+"/vehicle", map[string]any{"plate": "LD-45-21", "brand": "Toyota", "model": "Corolla", "year": 2020, "color": "Branco", "seats": 5}, nil)
		assert.Equal(t, "INVALID_PLATE", bad.code())
		assert.Contains(t, errorDetails(bad), "plate")
	})

	t.Run("a vehicle of someone else needs the owner documents", func(t *testing.T) {
		driver.confirmIdentity(t, "provider")
		require.Equal(t, http.StatusOK, driver.do(t, http.MethodPut, verificationPath+"/vehicle", map[string]any{"plate": "LD-45-21-AF", "brand": "Toyota", "model": "Corolla", "year": 2020, "color": "Branco", "seats": 5}, nil).status)
		driver.sendPhotos(t, "vehicle_registration", "", "page_1", "page_2")
		other := driver.do(t, http.MethodPut, verificationPath+"/vehicle/registration", map[string]string{"owner_name": "Maria Kiala"}, nil)
		require.Equal(t, http.StatusOK, other.status, other.text)
		assert.Equal(t, true, other.body["owner_documents_required"])
		assert.Equal(t, "in_progress", other.str("status"))
		driver.sendPhotos(t, "vehicle_owner_declaration", "", "main")
		driver.sendPhotos(t, "id_card", "vehicle_owner", "front", "back")
		assert.Equal(t, "needs_review", itemStatus(t, driver, "vehicle_registration"))
	})

	t.Run("IBAN, bank and holder", func(t *testing.T) {
		bai := catalogID(t, driver, "/api/v1/providers/catalogs/banks", "short_name", "BAI")
		put := func(iban, holder string) result {
			return driver.do(t, http.MethodPut, verificationPath+"/bank-account", map[string]any{"bank_id": bai, "iban": iban, "holder_name": holder}, nil)
		}
		invalid := put("AO06004000001234567810143", ocrName)
		assert.Equal(t, "INVALID_IBAN", invalid.code())
		assert.Contains(t, errorDetails(invalid), "iban")

		mismatch := put(bfaIBAN, ocrName)
		assert.Equal(t, "IBAN_BANK_MISMATCH", mismatch.code())
		assert.Equal(t, "BFA", errorData(mismatch)["detected_bank"].(map[string]any)["short_name"])

		holder := put(baiIBAN, "Outra Pessoa")
		assert.Equal(t, "HOLDER_NAME_MISMATCH", holder.code())
		assert.Contains(t, errorDetails(holder), "holder_name")
		assert.Equal(t, http.StatusOK, put(baiIBAN, "DOMINGOS MATEUS KIALA").status, "the name is compared without case")
	})

	t.Run("submitting with steps missing lists them", func(t *testing.T) {
		incomplete := driver.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
		assert.Equal(t, "VERIFICATION_INCOMPLETE", incomplete.code())
		missing := errorData(incomplete)["missing_items"].([]any)
		assert.Contains(t, missing, "selfie")
		assert.Contains(t, missing, "driving_license")
		assert.NotContains(t, missing, "identity_card")
	})
}

func TestAWorkshopIsVerifiedAndAppearsAfterApproval(t *testing.T) {
	workshop := verifyingProvider(t, "maintenance")
	admin := staffToken(t)
	nif := fmt.Sprintf("54%08d", uuid.New().ID()%100_000_000)

	view := workshop.get(t, verificationPath)
	require.Equal(t, http.StatusOK, view.status, view.text)
	assert.Equal(t, "workshop", view.str("type"))
	assert.EqualValues(t, 8, view.body["progress"].(map[string]any)["total"])
	assert.Len(t, view.body["groups"].([]any), 4)
	assert.Equal(t, "business_license", view.str("next_item"))

	require.Equal(t, "accepted", workshop.sendFile(t, "business_license", "main", "", "application/pdf", pdfBytes).str("status"))
	license := workshop.do(t, http.MethodPut, verificationPath+"/workshop/business-license", map[string]any{"number": "ALV-1", "expires_at": day(400), "company_name": "Auto Kiala, Lda"}, nil)
	require.Equal(t, http.StatusOK, license.status, license.text)

	require.Equal(t, "accepted", workshop.sendFile(t, "commercial_certificate", "main", "", "application/pdf", pdfBytes).str("status"))
	assert.Equal(t, "CERTIFICATE_TOO_OLD", workshop.do(t, http.MethodPut, verificationPath+"/workshop/commercial-certificate", map[string]any{"code": "C-1", "issued_at": day(-200)}, nil).code())
	require.Equal(t, http.StatusOK, workshop.do(t, http.MethodPut, verificationPath+"/workshop/commercial-certificate", map[string]any{"code": "C-1", "issued_at": day(-30)}, nil).status)

	assert.Equal(t, "INVALID_NIF", workshop.do(t, http.MethodPut, verificationPath+"/workshop/tax-id", map[string]any{"nif": "123", "company_name": "Auto Kiala, Lda", "company_type": "limited_company"}, nil).code())
	taxID := workshop.do(t, http.MethodPut, verificationPath+"/workshop/tax-id", map[string]any{"nif": nif, "company_name": "Auto Kiala Lda", "company_type": "limited_company"}, nil)
	require.Equal(t, http.StatusOK, taxID.status, taxID.text)
	assert.Equal(t, "needs_review", taxID.str("status"), "a company name that differs from the licence goes to review")

	workshop.confirmIdentity(t, "legal_representative")
	workshop.passSelfie(t, "legal_representative")
	assert.Equal(t, "completed", itemStatus(t, workshop, "legal_representative"))

	workshop.sendPhotos(t, "workshop_photo", "", "facade", "work_area", "equipment")
	assert.Equal(t, "in_progress", itemStatus(t, workshop, "workshop_photos"))
	workshop.sendPhotos(t, "workshop_photo", "", "reception", "extra", "extra")
	assert.Equal(t, "completed", itemStatus(t, workshop, "workshop_photos"))

	hours := []map[string]any{{"day": 0, "closed": true}, {"day": 1, "open": "08:00", "close": "18:00"}, {"day": 6, "open": "08:00", "close": "13:00"}}
	badHours := workshop.do(t, http.MethodPut, verificationPath+"/workshop/location", map[string]any{"address": "Rua Rainha Ginga, 45", "lat": -8.8147, "lng": 13.2302, "opening_hours": []map[string]any{{"day": 1, "open": "18:00", "close": "08:00"}}}, nil)
	assert.Contains(t, errorDetails(badHours), "opening_hours")
	location := workshop.do(t, http.MethodPut, verificationPath+"/workshop/location", map[string]any{"address": "Rua Rainha Ginga, 45", "reference": "Ao lado da farmácia", "lat": -8.8147, "lng": 13.2302, "opening_hours": hours}, nil)
	require.Equal(t, http.StatusOK, location.status, location.text)

	oil := catalogID(t, workshop, "/api/v1/providers/catalogs/workshop-services", "code", "oil")
	assert.Contains(t, errorDetails(workshop.do(t, http.MethodPut, verificationPath+"/workshop/services", map[string]any{"services": []map[string]any{{"service_id": uuid.NewString(), "base_price": 1000}}}, nil)), "services[0].service_id")
	services := workshop.do(t, http.MethodPut, verificationPath+"/workshop/services", map[string]any{"services": []map[string]any{{"service_id": oil, "base_price": 15000}}}, nil)
	require.Equal(t, http.StatusOK, services.status, services.text)

	workshop.sendPhotos(t, "bank_account", "", "main")
	bai := catalogID(t, workshop, "/api/v1/providers/catalogs/banks", "short_name", "BAI")
	personal := workshop.do(t, http.MethodPut, verificationPath+"/workshop/bank-account", map[string]any{"bank_id": bai, "iban": baiIBAN, "holder_name": ocrName}, nil)
	assert.Equal(t, "HOLDER_NAME_MISMATCH", personal.code(), "a personal account is refused")
	company := workshop.do(t, http.MethodPut, verificationPath+"/workshop/bank-account", map[string]any{"bank_id": bai, "iban": baiIBAN, "holder_name": "auto kiala lda"}, nil)
	require.Equal(t, http.StatusOK, company.status, company.text)
	assert.Equal(t, "company", company.str("owner_type"))
	assert.Equal(t, "STEP_NOT_IN_PLAN", workshop.do(t, http.MethodPut, verificationPath+"/bank-account", map[string]any{"bank_id": bai, "iban": baiIBAN, "holder_name": "auto kiala lda"}, nil).code())

	review := workshop.get(t, verificationPath+"/review")
	require.Equal(t, http.StatusOK, review.status, review.text)
	assert.Empty(t, review.body["missing_items"])
	assert.Contains(t, review.str("declaration"), "represento a empresa")

	submitted := workshop.post(t, verificationPath+"/submit", map[string]bool{"declaration_accepted": true})
	require.Equal(t, http.StatusOK, submitted.status, submitted.text)

	path := "/api/v1/admin/providers/" + workshop.id.String()
	for _, document := range []string{"business_license", "commercial_certificate", "tax_id", "workshop_photo", "bank_account"} {
		require.Equal(t, http.StatusOK, admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved"}).status, document)
	}
	early := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": serviceBySlug(t, "maintenance")}, nil)
	assert.Equal(t, "APPROVAL_REQUIREMENTS_NOT_MET", early.code(), "the representative documents need a review too")
	for _, document := range []string{"id_card", "selfie"} {
		reviewed := admin.post(t, path+"/documents/"+document+"/review", map[string]string{"status": "approved", "subject": "legal_representative"})
		require.Equal(t, http.StatusOK, reviewed.status, reviewed.text)
	}
	approved := admin.do(t, http.MethodPut, path, map[string]any{"status": "approved", "service_id": serviceBySlug(t, "maintenance")}, nil)
	require.Equal(t, http.StatusOK, approved.status, approved.text)

	var name string
	var hoursCount, servicesCount int
	var priceCents int64
	require.NoError(t, env.owner.QueryRow(context.Background(), `
		SELECT w.name, (SELECT count(*) FROM workshop_hours h WHERE h.workshop_id = w.id), (SELECT count(*) FROM workshop_services s WHERE s.workshop_id = w.id),
		       (SELECT price_cents FROM workshop_services s WHERE s.workshop_id = w.id LIMIT 1)
		FROM workshops w JOIN organizations o ON o.id = w.organization_id WHERE o.nif = $1 AND w.is_active`, nif).Scan(&name, &hoursCount, &servicesCount, &priceCents))
	assert.Equal(t, "Auto Kiala Lda", name)
	assert.Equal(t, 2, hoursCount)
	assert.Equal(t, 1, servicesCount)
	assert.EqualValues(t, 1_500_000, priceCents)
}
