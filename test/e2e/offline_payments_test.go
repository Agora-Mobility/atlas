//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/migrations"
)

const (
	offlinePaymentsSQL = "000051_offline_customer_payments.up.sql"
	digitalPaymentsSQL = "000051_offline_customer_payments.down.sql"
)

func applySQL(ctx context.Context, owner *pgxpool.Pool, name string) error {
	statements, err := migrations.FS.ReadFile(name)
	if err != nil {
		return err
	}
	_, err = owner.Exec(ctx, string(statements))
	return err
}

func useOfflinePayments(t *testing.T) {
	t.Helper()
	require.NoError(t, applySQL(context.Background(), env.owner, offlinePaymentsSQL))
	t.Cleanup(func() {
		require.NoError(t, applySQL(context.Background(), env.owner, digitalPaymentsSQL))
	})
}

func TestCustomersPayOutsideTheApp(t *testing.T) {
	customer := newCustomer(t)
	_, unpaid, _ := completedRide(t, customer, "gpo")
	require.Equal(t, "unpaid", requestPaymentStatus(t, customer, unpaid))
	useOfflinePayments(t)

	t.Run("cash is the only method offered", func(t *testing.T) {
		methods := customer.get(t, "/api/v1/payment-methods")
		require.Equal(t, http.StatusOK, methods.status, methods.text)
		items := methods.body["items"].([]any)
		require.NotEmpty(t, items)
		for _, item := range items {
			assert.Equal(t, "cash", item.(map[string]any)["method"])
		}
	})

	t.Run("an unpaid request never blocks the next one", func(t *testing.T) {
		created := requestTaxi(t, customer)
		require.Equal(t, http.StatusCreated, created.status, created.text)
		assert.Equal(t, "cash", created.str("payment_method"))
		cancelled := customer.post(t, "/api/v1/requests/"+created.str("id")+"/cancel", map[string]any{"reason_id": cancelReason(t, customer, "customer", "taxi", "changed_plans")})
		require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
	})

	t.Run("cancelling late costs nothing", func(t *testing.T) {
		late, _, id := assignedTaxi(t)
		_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET accepted_at = now() - interval '121 seconds' WHERE id = $1`, id)
		require.NoError(t, err)
		assert.EqualValues(t, 0, late.get(t, "/api/v1/requests/"+id+"/cancellation").num("fee_cents"))
		cancelled := late.post(t, "/api/v1/requests/"+id+"/cancel", map[string]any{"reason_id": cancelReason(t, late, "customer", "taxi", "changed_plans")})
		require.Equal(t, http.StatusOK, cancelled.status, cancelled.text)
		assert.EqualValues(t, 0, cancelled.num("cancel_fee_cents"))
		assert.Equal(t, http.StatusCreated, requestTaxi(t, late).status, "no fee, no debt")
	})
}
