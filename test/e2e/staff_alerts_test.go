//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheBackofficeHearsSOSAndNewTickets(t *testing.T) {
	admin := staffToken(t)
	alerts := admin.listenTo(t, "/admin/events", "", 0)
	customer, _, id := assignedTaxi(t)

	sos := customer.post(t, "/api/v1/requests/"+id+"/sos", map[string]any{"note": "Preciso de ajuda"})
	require.Equal(t, http.StatusCreated, sos.status, sos.text)
	alert := alerts.waitFor(t, "staff.request_alert", func(event sseEvent) bool { return event.Data["request_id"] == id })
	assert.Equal(t, "sos", alert.Data["reason"])
	assert.Equal(t, "taxi", alert.Data["category"])

	ticket := customer.post(t, "/api/v1/requests/"+id+"/tickets", map[string]any{"subject": "Motorista", "message": "O motorista foi rude."})
	require.Equal(t, http.StatusCreated, ticket.status, ticket.text)
	opened := alerts.waitFor(t, "staff.ticket", func(event sseEvent) bool { return event.Data["ticket_id"] == ticket.str("id") })
	assert.Equal(t, "Motorista", opened.Data["subject"])
	assert.Equal(t, id, opened.Data["request_id"])

	forbidden, err := http.NewRequest(http.MethodGet, customer.api+"/admin/events", http.NoBody)
	require.NoError(t, err)
	forbidden.Header.Set("X-Client-Id", customer.client)
	forbidden.Header.Set("Authorization", "Bearer "+customer.token)
	res, err := http.DefaultClient.Do(forbidden)
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
}
