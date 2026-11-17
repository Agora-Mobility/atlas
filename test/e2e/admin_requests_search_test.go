//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheTeamFindsARequestByWhatTheCustomerSays(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	code := customer.get(t, "/api/v1/requests/"+id).str("code")
	require.NotEmpty(t, code)
	var phone, name, plate string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT phone, name FROM users WHERE id = $1`, customer.id).Scan(&phone, &name))
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT plate FROM vehicles WHERE provider_id = $1`, driver.id).Scan(&plate))

	admin := staffToken(t)
	find := func(search string) []string {
		t.Helper()
		res := admin.get(t, "/api/v1/admin/requests?size=100&search="+search)
		require.Equal(t, http.StatusOK, res.status, res.text)
		var ids []string
		for _, item := range res.body["items"].([]any) {
			row := item.(map[string]any)
			if row["id"] == id {
				assert.Equal(t, code, row["code"], "the list carries the code")
			}
			ids = append(ids, row["id"].(string))
		}
		return ids
	}
	assert.Equal(t, []string{id}, find(strings.ToLower(code)), "by code, in any case")
	assert.Contains(t, find(phone[len(phone)-9:]), id, "by phone, without the country code")
	assert.Contains(t, find(strings.ReplaceAll(plate, "-", "")), id, "by plate, without dashes")
	assert.Contains(t, find(strings.Fields(name)[0]), id, "by name")
	assert.Empty(t, find("%25"), "a percent sign is text, not a wildcard")

	requestTaxi(t, newCustomer(t))
	oldest := admin.get(t, "/api/v1/admin/requests?sort=created_at&size=1")
	require.Equal(t, http.StatusOK, oldest.status, oldest.text)
	newest := admin.get(t, "/api/v1/admin/requests?size=1")
	assert.NotEqual(t, oldest.body["items"].([]any)[0].(map[string]any)["id"], newest.body["items"].([]any)[0].(map[string]any)["id"])
	assert.Equal(t, http.StatusBadRequest, admin.get(t, "/api/v1/admin/requests?sort=customer").status)
}
