//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inboxTitles(t *testing.T, who *person) []string {
	t.Helper()
	res := who.get(t, "/api/v1/notifications")
	require.Equal(t, http.StatusOK, res.status, res.text)
	titles := []string{}
	for _, item := range res.body["items"].([]any) {
		titles = append(titles, item.(map[string]any)["title"].(string))
	}
	return titles
}

func TestABookingFillsTheInboxAndRespectsPreferences(t *testing.T) {
	workshop := newShop(t, 3)
	customer, intruder := newCustomer(t), newCustomer(t)
	created := workshop.book(t, customer, workshop.firstSlot(t, customer), "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"Marcação confirmada"}, inboxTitles(t, customer))
	}, 10*time.Second, 50*time.Millisecond)

	list := customer.get(t, "/api/v1/notifications")
	assert.EqualValues(t, 1, list.num("unread"))
	item := list.body["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "booking", item["link_type"])
	assert.Equal(t, created.str("id"), item["link_id"])
	assert.Equal(t, http.StatusNotFound, intruder.post(t, "/api/v1/notifications/"+item["id"].(string)+"/read", nil).status)
	require.Equal(t, http.StatusNoContent, customer.post(t, "/api/v1/notifications/"+item["id"].(string)+"/read", nil).status)
	assert.Zero(t, customer.get(t, "/api/v1/notifications").num("unread"))

	saved := customer.do(t, http.MethodPut, "/api/v1/users/me/preferences", map[string]any{"notify_status": false, "notify_sms": false, "language": "pt", "currency": "AOA"}, nil)
	require.Equal(t, http.StatusOK, saved.status, saved.text)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.do(t, http.MethodPut, "/api/v1/users/me/preferences", map[string]any{"language": "klingon", "currency": "AOA"}, nil).status)

	var phone string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT phone FROM users WHERE id = $1`, customer.id).Scan(&phone))
	before := len(env.sms.messagesTo(phone))
	second := workshop.book(t, customer, workshop.firstSlot(t, customer), "gpo")
	require.Equal(t, http.StatusCreated, second.status, second.text)
	require.Eventually(t, func() bool { return customer.get(t, "/api/v1/notifications").num("total") == 2 }, 10*time.Second, 50*time.Millisecond, "the inbox does not depend on the toggles")
	var skipped int
	require.Eventually(t, func() bool {
		require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id = $1 AND channel = 'sms' AND status = 'skipped'`, customer.id).Scan(&skipped))
		return skipped == 1
	}, 10*time.Second, 50*time.Millisecond)
	assert.Len(t, env.sms.messagesTo(phone), before, "the SMS is not sent")
}

func TestAReferralPaysOnceOnlyAfterTheFriendPays(t *testing.T) {
	ctx := context.Background()
	friend, invited, driver := newCustomer(t), newCustomer(t), newProvider(t, "taxi", point{-8.84, 13.235})
	code := friend.get(t, "/api/v1/users/me/referral")
	require.Equal(t, http.StatusOK, code.status, code.text)
	assert.EqualValues(t, 100_000, code.num("reward_cents"))
	assert.Equal(t, "REFERRAL_OWN_CODE", friend.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).code())
	assert.Equal(t, "REFERRAL_NOT_FOUND", invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": "NAOEXISTE"}).code())
	require.Equal(t, http.StatusNoContent, invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).status)
	assert.Equal(t, "REFERRAL_ALREADY_USED", invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).code())
	assert.Zero(t, friend.get(t, "/api/v1/wallet").num("balance_cents"))

	driver.listen(t, "")
	created := requestTaxi(t, invited)
	_, err := env.owner.Exec(ctx, `UPDATE service_requests SET provider_id = $1, status = 'completed', completed_at = now(), payment_method = 'wallet', next_action_at = NULL WHERE id = $2`, driver.id, created.str("id"))
	require.NoError(t, err)
	_, err = env.owner.Exec(ctx, `WITH t AS (INSERT INTO ledger_transactions (id, idempotency_key, kind, currency) VALUES (gen_random_uuid(), 'seed-' || $1::text, 'wallet_credit', 'AOA') RETURNING id),
		a AS (INSERT INTO ledger_entries (transaction_id, account_type, account_id, amount_cents) SELECT id, 'wallet', $1::uuid, 100000000 FROM t)
		INSERT INTO ledger_entries (transaction_id, account_type, amount_cents) SELECT id, 'external', -100000000 FROM t`, invited.id)
	require.NoError(t, err)
	assert.Zero(t, friend.get(t, "/api/v1/wallet").num("balance_cents"), "nothing yet: completed but not paid")
	paid := invited.post(t, "/api/v1/requests/"+created.str("id")+"/payments", map[string]string{"method": "wallet"})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, paid.status, paid.text)
	assert.EqualValues(t, 100_000, friend.get(t, "/api/v1/wallet").num("balance_cents"))
	var rewards int
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT count(*) FROM ledger_transactions WHERE kind = 'referral_reward' AND request_id = $1`, created.str("id")).Scan(&rewards))
	assert.Equal(t, 1, rewards)
	assert.Contains(t, []string{"REFERRAL_ALREADY_USED", "REFERRAL_TOO_LATE"}, invited.post(t, "/api/v1/users/me/referral/redeem", map[string]string{"code": code.str("code")}).code())
}

func TestHelpSearchesAndShowsTheRecentRequest(t *testing.T) {
	customer := newCustomer(t)
	found := customer.get(t, "/api/v1/help?search=reembolso")
	require.Equal(t, http.StatusOK, found.status, found.text)
	assert.Len(t, found.body["topics"], 1)
	assert.NotEmpty(t, found.str("support_phone"))
	assert.Nil(t, found.body["recent_request"])
	created := requestTaxi(t, customer)
	assert.Equal(t, created.str("id"), customer.get(t, "/api/v1/help").body["recent_request"].(map[string]any)["id"])
}

func TestAvatarMustBeAPhotoOfTheUser(t *testing.T) {
	owner, other := newCustomer(t), newCustomer(t)
	assert.Empty(t, owner.get(t, "/api/v1/users/me/avatar").str("url"))
	photo := uploadPhoto(t, owner)
	assert.Equal(t, http.StatusNotFound, other.do(t, http.MethodPut, "/api/v1/users/me/avatar", map[string]string{"photo_id": photo}, nil).status)
	set := owner.do(t, http.MethodPut, "/api/v1/users/me/avatar", map[string]string{"photo_id": photo}, nil)
	require.Equal(t, http.StatusOK, set.status, set.text)
	assert.NotEmpty(t, set.str("url"))
}

func TestTheDriverKeepsTheNavigationAppTheyChose(t *testing.T) {
	driver := newProvider(t, "taxi", point{-8.84, 13.235})
	path := "/api/v1/users/me/preferences"
	assert.Equal(t, "", driver.get(t, path).str("navigation_app"), "nothing chosen yet")

	saved := driver.do(t, http.MethodPut, path, map[string]any{"notify_status": true, "language": "pt", "currency": "AOA", "navigation_app": "waze"}, nil)
	require.Equal(t, http.StatusOK, saved.status, saved.text)
	assert.Equal(t, "waze", saved.str("navigation_app"))
	assert.Equal(t, "waze", driver.get(t, path).str("navigation_app"))

	kept := driver.do(t, http.MethodPut, path, map[string]any{"notify_status": false, "language": "pt", "currency": "AOA"}, nil)
	require.Equal(t, http.StatusOK, kept.status, kept.text)
	assert.Equal(t, "waze", kept.str("navigation_app"), "an app that does not send it keeps the choice")

	assert.Equal(t, http.StatusUnprocessableEntity, driver.do(t, http.MethodPut, path, map[string]any{"language": "pt", "currency": "AOA", "navigation_app": "bussola"}, nil).status)

	cleared := driver.do(t, http.MethodPut, path, map[string]any{"language": "pt", "currency": "AOA", "navigation_app": ""}, nil)
	require.Equal(t, http.StatusOK, cleared.status, cleared.text)
	assert.Equal(t, "", cleared.str("navigation_app"))
}
