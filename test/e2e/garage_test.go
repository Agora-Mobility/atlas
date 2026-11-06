//go:build e2e

package e2e_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vehiclePayload(plate string) map[string]any {
	return map[string]any{"brand": "Toyota", "model": "Hilux", "plate": plate, "year": 2019, "fuel": "diesel"}
}

func uploadPhoto(t *testing.T, owner *person) string {
	t.Helper()
	res := owner.post(t, "/api/v1/media/photos", map[string]any{"content_type": "image/jpeg", "size_bytes": len(jpegBytes)})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	request, err := http.NewRequest(http.MethodPut, res.str("upload_url"), bytes.NewReader(jpegBytes))
	require.NoError(t, err)
	if headers, ok := res.body["headers"].(map[string]any); ok {
		for key, value := range headers {
			request.Header.Set(key, value.(string))
		}
	}
	request.Header.Set("Content-Type", "image/jpeg")
	uploaded, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_ = uploaded.Body.Close()
	require.Less(t, uploaded.StatusCode, 300)
	return res.str("id")
}

func TestCustomerVehiclesBelongToTheirOwner(t *testing.T) {
	customer, intruder := newCustomer(t), newCustomer(t)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, "/api/v1/users/me/vehicles", map[string]any{"brand": "Toyota"}).status)
	wrongYear := vehiclePayload("LD-1")
	wrongYear["year"] = 99_999_999_999
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, "/api/v1/users/me/vehicles", wrongYear).status, "over the column")
	wrongFuel := vehiclePayload("LD-1")
	wrongFuel["fuel"] = "agua"
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, "/api/v1/users/me/vehicles", wrongFuel).status)

	created := customer.post(t, "/api/v1/users/me/vehicles", vehiclePayload("ld-45-21-af"))
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	assert.Equal(t, "LD-45-21-AF", created.str("plate"))
	assert.Equal(t, "PLATE_TAKEN", customer.post(t, "/api/v1/users/me/vehicles", vehiclePayload("LD-45-21-AF")).code())
	assert.Equal(t, http.StatusCreated, intruder.post(t, "/api/v1/users/me/vehicles", vehiclePayload("LD-45-21-AF")).status, "another customer can have the same plate")

	assert.Equal(t, http.StatusNotFound, intruder.do(t, http.MethodPut, "/api/v1/users/me/vehicles/"+id, vehiclePayload("X-1"), nil).status)
	assert.Equal(t, http.StatusNotFound, intruder.do(t, http.MethodDelete, "/api/v1/users/me/vehicles/"+id, nil, nil).status)
	assert.Len(t, customer.get(t, "/api/v1/users/me/vehicles").body["items"], 1)
	updated := customer.do(t, http.MethodPut, "/api/v1/users/me/vehicles/"+id, vehiclePayload("LD-99"), nil)
	require.Equal(t, http.StatusOK, updated.status, updated.text)
	require.Equal(t, http.StatusNoContent, customer.do(t, http.MethodDelete, "/api/v1/users/me/vehicles/"+id, nil, nil).status)
	assert.Empty(t, customer.get(t, "/api/v1/users/me/vehicles").body["items"])
}

func TestPhotosFollowTypeSizeLimitAndOwner(t *testing.T) {
	clearOnlineProviders(t)
	customer, intruder := newCustomer(t), newCustomer(t)
	assert.Equal(t, "PHOTO_TYPE_NOT_ALLOWED", customer.post(t, "/api/v1/media/photos", map[string]any{"content_type": "application/x-msdownload", "size_bytes": 100}).code())
	assert.Equal(t, "PHOTO_TOO_LARGE", customer.post(t, "/api/v1/media/photos", map[string]any{"content_type": "image/jpeg", "size_bytes": 90_000_000}).code())

	created := requestTaxi(t, customer)
	path := "/api/v1/requests/" + created.str("id") + "/photos"
	photo := uploadPhoto(t, customer)
	assert.Equal(t, http.StatusNotFound, intruder.post(t, path, map[string]any{"photo_ids": []string{photo}}).status, "the request of another")
	other := requestTaxi(t, intruder)
	assert.Equal(t, http.StatusNotFound, intruder.post(t, "/api/v1/requests/"+other.str("id")+"/photos", map[string]any{"photo_ids": []string{photo}}).status, "the photo of another")

	announced := customer.post(t, "/api/v1/media/photos", map[string]any{"content_type": "image/jpeg", "size_bytes": 100})
	require.Equal(t, http.StatusCreated, announced.status)
	assert.Equal(t, "PHOTO_NOT_UPLOADED", customer.post(t, path, map[string]any{"photo_ids": []string{announced.str("id")}}).code(), "announced and never uploaded")

	assert.Equal(t, http.StatusNotFound, customer.post(t, "/api/v1/requests/"+created.str("id")+"/photos", map[string]any{"photo_ids": []string{uuid.NewString()}}).status)
	ids := []string{photo}
	for range 4 {
		ids = append(ids, uploadPhoto(t, customer))
	}
	attached := customer.post(t, path, map[string]any{"photo_ids": ids})
	require.Equal(t, http.StatusOK, attached.status, attached.text)
	assert.Len(t, attached.body["items"], 5)

	sixth := customer.post(t, path, map[string]any{"photo_ids": []string{uploadPhoto(t, customer)}})
	assert.Equal(t, "PHOTO_LIMIT", sixth.code(), "the sixth photo is refused: %s", sixth.text)
	assert.Equal(t, http.StatusNotFound, intruder.get(t, path).status)
}
