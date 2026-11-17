//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheTeamReadsWhoChangedWhat(t *testing.T) {
	admin := staffToken(t)
	code := "audit_" + uuid.NewString()[:8]
	created := admin.post(t, "/api/v1/admin/catalog/compliment", map[string]any{"code": code, "label": "Auditado", "is_active": false})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")

	log := admin.get(t, "/api/v1/admin/audit?action=catalog_item.create&entity=catalog_item&entity_id="+id)
	require.Equal(t, http.StatusOK, log.status, log.text)
	items := log.body["items"].([]any)
	require.Len(t, items, 1)
	entry := items[0].(map[string]any)
	actor := entry["actor"].(map[string]any)
	assert.Equal(t, "Admin", actor["name"])
	assert.Equal(t, "staff", actor["type"])
	assert.Contains(t, log.text, code, "the values after the change")
	assert.NotEmpty(t, entry["occurred_at"])
	assert.NotContains(t, log.text, "ip_address")

	byActor := admin.get(t, "/api/v1/admin/audit?size=1&actor_id="+actor["id"].(string))
	require.Equal(t, http.StatusOK, byActor.status, byActor.text)
	assert.Len(t, byActor.body["items"], 1)

	assert.Equal(t, http.StatusUnprocessableEntity, admin.get(t, "/api/v1/admin/audit?from=ontem").status)
	assert.Equal(t, http.StatusForbidden, newCustomer(t).get(t, "/api/v1/admin/audit").status)
}
