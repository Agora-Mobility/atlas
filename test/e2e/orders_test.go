//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARequestHasAReadableCodeByCategory(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	taxi := requestTaxi(t, customer)
	assert.Regexp(t, regexp.MustCompile(`^TX-\d{5,}$`), taxi.str("code"))
	tow := requestCategory(t, customer, "towing", map[string]any{"destination": target, "fare_estimate_id": mustEstimate(t, customer, "towing"), "towing": towingDetails(t, customer)})
	assert.Regexp(t, regexp.MustCompile(`^RB-\d{5,}$`), tow.str("code"))
	assert.NotEmpty(t, tow.str("service_name"))
	assert.NotEqual(t, strings.Split(taxi.str("code"), "-")[1], strings.Split(tow.str("code"), "-")[1])

	active := customer.get(t, "/api/v1/requests/active")
	require.Equal(t, http.StatusOK, active.status, active.text)
	assert.Len(t, active.body["requests"], 2, "several active requests are listed")
}

func mustEstimate(t *testing.T, customer *person, category string) string {
	t.Helper()
	id, _ := estimateFor(t, customer, category)
	return id
}

func TestTheReceiptHasTheSameValuesOfTheRequest(t *testing.T) {
	customer, driver, id, fare := completedTaxi(t, "gpo")
	payExpress(t, customer, id)
	intruder := newCustomer(t)
	assert.Equal(t, http.StatusNotFound, intruder.get(t, "/api/v1/requests/"+id+"/receipt").status)
	assert.Equal(t, http.StatusNotFound, driver.get(t, "/api/v1/requests/"+id+"/receipt").status, "the receipt is of the customer")

	receipt := customer.get(t, "/api/v1/requests/"+id+"/receipt")
	require.Equal(t, http.StatusOK, receipt.status, receipt.text)
	assert.True(t, strings.HasPrefix(receipt.text, "%PDF-1.4"))
	view := customer.get(t, "/api/v1/requests/"+id)
	assert.Contains(t, receipt.text, view.str("code"))
	assert.Contains(t, receipt.text, formatKz(fare), "the total of the request")
	assert.Contains(t, receipt.text, "gpo")
}

func formatKz(cents int64) string {
	whole := strconv.FormatInt(cents/100, 10)
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(' ')
		}
		grouped.WriteRune(digit)
	}
	decimals := strconv.FormatInt(cents%100+100, 10)[1:]
	return grouped.String() + "," + decimals + " Kz"
}

func TestAReceiptOnlyExistsAfterCompletion(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	created := requestTaxi(t, customer)
	assert.Equal(t, "RECEIPT_NOT_READY", customer.get(t, "/api/v1/requests/"+created.str("id")+"/receipt").code())
}

func TestRepeatPreparesNewPricesWithoutRequesting(t *testing.T) {
	customer, driver, id, _ := completedTaxi(t, "cash")
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil).status)
	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, "/api/v1/requests/"+id+"/repeat", nil).status)
	assert.Equal(t, http.StatusForbidden, driver.post(t, "/api/v1/requests/"+id+"/repeat", nil).status)

	var before int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM service_requests WHERE customer_id = $1`, customer.id).Scan(&before))
	repeat := customer.post(t, "/api/v1/requests/"+id+"/repeat", nil)
	require.Equal(t, http.StatusCreated, repeat.status, repeat.text)
	var after int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM service_requests WHERE customer_id = $1`, customer.id).Scan(&after))
	assert.Equal(t, before, after, "nothing is requested yet")

	item := repeat.body["estimates"].([]any)[0].(map[string]any)
	again := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": repeat.str("service_id"), "fare_estimate_id": item["fare_estimate_id"], "pickup": repeat.body["pickup"], "destination": repeat.body["destination"]}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, http.StatusCreated, again.status, "the prepared data works: %s", again.text)
}

func TestStopTimesAndRecentDestinations(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	option := taxiQuote(t, customer, map[string]any{"stops": pointsOf(stopA, stopB)})
	created := askFor(t, customer, option, map[string]any{"stops": pointsOf(stopA, stopB)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, stream.waitFor(t, "request.offer", nil)).status)
	assert.Equal(t, "INVALID_TRANSITION", driver.post(t, "/api/v1/requests/"+id+"/stops/0/arrive", nil).code(), "before the trip starts")
	for _, step := range []string{"en-route", "arrive", "start"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status)
	}
	assert.Equal(t, http.StatusNotFound, newProvider(t, "taxi", point{-8.85, 13.24}).post(t, "/api/v1/requests/"+id+"/stops/0/arrive", nil).status, "another driver")
	assert.Equal(t, "STOP_NOT_FOUND", driver.post(t, "/api/v1/requests/"+id+"/stops/9/arrive", nil).code())
	arrived := driver.post(t, "/api/v1/requests/"+id+"/stops/0/arrive", nil)
	require.Equal(t, http.StatusOK, arrived.status, arrived.text)
	stops := customer.get(t, "/api/v1/requests/"+id).body["stops"].([]any)
	assert.NotEmpty(t, stops[0].(map[string]any)["arrived_at"], "the hour of the stop")
	assert.Nil(t, stops[1].(map[string]any)["arrived_at"])

	recent := customer.get(t, "/api/v1/users/me/recent-destinations")
	require.Equal(t, http.StatusOK, recent.status, recent.text)
	assert.Len(t, recent.body["items"], 1)
	second := taxiQuote(t, customer, nil)
	require.Equal(t, http.StatusCreated, askFor(t, customer, second, nil).status)
	recent = customer.get(t, "/api/v1/users/me/recent-destinations")
	assert.Len(t, recent.body["items"], 1, "the same destination twice appears once")
	assert.Equal(t, http.StatusBadRequest, customer.get(t, "/api/v1/users/me/recent-destinations?limit=99999999999").status)
	assert.Empty(t, newCustomer(t).get(t, "/api/v1/users/me/recent-destinations").body["items"], "only the destinations of this customer")
}

func TestTicketsBelongToTheirAuthor(t *testing.T) {
	admin := staffToken(t)
	customer, driver, id, _ := completedTaxi(t, "cash")
	path := "/api/v1/requests/" + id + "/tickets"
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"subject": "Sem mensagem"}).status)
	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, path, map[string]any{"subject": "x", "message": "y"}).status, "a ticket on the request of another")
	opened := customer.post(t, path, map[string]any{"subject": "Fui cobrado a mais", "message": "O valor não bate certo"})
	require.Equal(t, http.StatusCreated, opened.status, opened.text)
	ticket := opened.str("id")
	assert.Regexp(t, regexp.MustCompile(`^TK-\d+$`), opened.str("code"))
	assert.Equal(t, id, opened.str("request_id"))
	assert.Equal(t, http.StatusCreated, driver.post(t, path, map[string]any{"subject": "Cliente", "message": "Problema"}).status, "the provider of the request too")

	intruder := newCustomer(t)
	assert.Equal(t, http.StatusNotFound, intruder.get(t, "/api/v1/support/tickets/"+ticket).status)
	assert.Equal(t, http.StatusNotFound, intruder.post(t, "/api/v1/support/tickets/"+ticket+"/messages", map[string]any{"message": "olá"}).status)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/admin/support/tickets").status)

	answered := admin.post(t, "/api/v1/admin/support/tickets/"+ticket+"/messages", map[string]any{"message": "Vamos verificar"})
	require.Equal(t, http.StatusOK, answered.status, answered.text)
	assert.Equal(t, "answered", answered.str("status"))
	mine := customer.get(t, "/api/v1/support/tickets/"+ticket)
	assert.Len(t, mine.body["messages"], 2)
	assert.EqualValues(t, 1, customer.get(t, "/api/v1/support/tickets").num("total"))

	require.Equal(t, http.StatusOK, admin.post(t, "/api/v1/admin/support/tickets/"+ticket+"/close", nil).status)
	assert.Equal(t, "TICKET_CLOSED", customer.post(t, "/api/v1/support/tickets/"+ticket+"/messages", map[string]any{"message": "olá"}).code())
}
