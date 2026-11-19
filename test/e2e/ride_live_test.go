//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fcmWith(token string, match func(map[string]string) bool) func() bool {
	return func() bool {
		for _, message := range env.fcm.forToken(token) {
			if match(message.Data) {
				return true
			}
		}
		return false
	}
}

func apnsWith(token string, match func(apnsSeen) bool) func() bool {
	return func() bool {
		for _, push := range env.apns.forToken(token) {
			if match(push) {
				return true
			}
		}
		return false
	}
}

func TestTheLockScreenFollowsTheRideUntilItEnds(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	path := "/api/v1/requests/" + id + "/push-token"
	android, ios := "fcm-ride-"+uuid.NewString(), "activity-"+uuid.NewString()

	assert.Equal(t, http.StatusNotFound, newCustomer(t).post(t, path, map[string]any{"platform": "ios", "token": ios}).status)
	assert.Equal(t, http.StatusNotFound, driver.post(t, path, map[string]any{"platform": "ios", "token": ios}).status, "only the customer")
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"platform": "web", "token": ios}).status)
	require.Equal(t, http.StatusNoContent, customer.post(t, path, map[string]any{"platform": "android", "token": "old-" + android}).status)
	require.Equal(t, http.StatusNoContent, customer.post(t, path, map[string]any{"platform": "android", "token": android}).status, "a new token replaces the old one")
	require.Equal(t, http.StatusNoContent, customer.post(t, path, map[string]any{"platform": "ios", "token": ios}).status)

	wait := func(condition func() bool, message string) {
		t.Helper()
		require.Eventually(t, condition, 5*time.Second, 20*time.Millisecond, message)
	}
	phase := func(name string) func(map[string]string) bool {
		return func(data map[string]string) bool { return data["type"] == "ride.live" && data["phase"] == name }
	}
	statePhase := func(name string, priority string) func(apnsSeen) bool {
		return func(push apnsSeen) bool {
			return push.Event == "update" && push.State["phase"] == name && push.Priority == priority
		}
	}

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/en-route", nil).status)
	wait(fcmWith(android, phase("en_route")), "Android gets the change of state")
	wait(apnsWith(ios, statePhase("en_route", "10")), "iOS gets the change of state at high priority")
	// The accepted event may still reach the old token: it is processed while the tokens change
	assert.False(t, fcmWith("old-"+android, func(data map[string]string) bool { return data["status"] == "provider_en_route" })(),
		"after the new token, the old one gets nothing")
	first := env.apns.forToken(ios)[0]
	assert.Equal(t, "liveactivity", first.PushType)
	assert.Equal(t, "ao.agora.cliente.push-type.liveactivity", first.Topic)

	moved := driver.post(t, "/api/v1/providers/me/locations", map[string]any{"positions": []any{positionAt(point{-8.8395, 13.2345})}})
	require.Equal(t, http.StatusOK, moved.status, moved.text)
	wait(fcmWith(android, func(data map[string]string) bool {
		return data["type"] == "ride.live" && data["lat"] == "-8.839500" && data["eta_s"] != "" && data["distance_m"] != ""
	}), "the position, the ETA and the distance of the driver")
	wait(apnsWith(ios, func(push apnsSeen) bool { return push.Priority == "5" && push.State["driverLat"] == -8.8395 }), "position updates go at low priority")

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/arrive", nil).status)
	wait(fcmWith(android, phase("arrived")), "the arrival has its own phase")
	wait(apnsWith(ios, statePhase("arrived", "10")), "the arrival lights the lock screen")

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/start", nil).status)
	wait(apnsWith(ios, statePhase("in_trip", "10")), "the trip started")

	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/complete", nil).status)
	wait(apnsWith(ios, func(push apnsSeen) bool {
		return push.Event == "end" && push.Dismiss && push.State["status"] == "completed"
	}), "iOS ends the Live Activity")
	wait(fcmWith(android, func(data map[string]string) bool { return data["event"] == "end" && data["status"] == "completed" }), "Android removes the notification")
	wait(func() bool {
		var left int
		err := env.owner.QueryRow(context.Background(), `SELECT count(*) FROM ride_push_tokens WHERE request_id = $1`, id).Scan(&left)
		return err == nil && left == 0
	}, "the tokens are deleted when the ride ends")

	assert.Equal(t, "RIDE_ENDED", customer.post(t, path, map[string]any{"platform": "ios", "token": ios}).code())
}

func TestCancellingTheRideEndsTheLockScreen(t *testing.T) {
	customer, driver, id := assignedTaxi(t)
	ios := "activity-" + uuid.NewString()
	require.Equal(t, http.StatusNoContent, customer.post(t, "/api/v1/requests/"+id+"/push-token", map[string]any{"platform": "ios", "token": ios}).status)

	reason := cancelReason(t, driver, "provider", "taxi", "vehicle_problem")
	cancelled := driver.post(t, "/api/v1/requests/"+id+"/cancel", map[string]any{"reason_id": reason})
	require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)

	require.Eventually(t, apnsWith(ios, func(push apnsSeen) bool {
		return push.Event == "end" && push.State["phase"] == "cancelled" && push.Dismiss
	}),
		5*time.Second, 20*time.Millisecond, "a cancelled ride leaves the lock screen")
}
