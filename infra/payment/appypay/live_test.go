//go:build appypaylive

package appypay_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/payment/appypay"
)

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	require.NotEmpty(t, value, key)
	return value
}

func liveClient(t *testing.T) *appypay.Client {
	t.Helper()
	return appypay.New(appypay.Settings{
		ClientID: requiredEnv(t, "APPYPAY_CLIENT_ID"), ClientSecret: requiredEnv(t, "APPYPAY_CLIENT_SECRET"), BaseURL: requiredEnv(t, "APPYPAY_BASE_URL"),
		TokenURL: requiredEnv(t, "APPYPAY_TOKEN_URL"), Resource: requiredEnv(t, "APPYPAY_RESOURCE"), GPOMethod: requiredEnv(t, "APPYPAY_GPO_METHOD"),
		REFMethod: requiredEnv(t, "APPYPAY_REF_METHOD"), Timeout: 100 * time.Second,
	})
}

func merchantID() string {
	id, err := entities.NewMerchantID()
	if err != nil {
		panic(fmt.Sprintf("merchant id: %v", err))
	}
	return id
}

func TestLiveGPOIsConfirmedAndRefunded(t *testing.T) {
	client, ctx, id := liveClient(t), context.Background(), merchantID()
	created, err := client.CreateCharge(ctx, services.ChargeInput{Method: entities.PaymentGPO, MerchantID: id, AmountCents: 10_000, Currency: "AOA", Description: "Teste Atlas", PhoneNumber: "900000000"})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	t.Logf("create status %s", created.Status)

	var charge *services.Charge
	require.Eventually(t, func() bool {
		charge, err = client.GetCharge(ctx, created.ID)
		return err == nil && charge.Status != services.ChargePending
	}, 90*time.Second, 2*time.Second)
	assert.Equal(t, services.ChargeSuccess, charge.Status)
	assert.Equal(t, id, charge.MerchantID)
	assert.EqualValues(t, 10_000, charge.AmountCents)

	found, err := client.FindChargeByMerchantID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)

	refund, err := client.Refund(ctx, created.ID, 5_000, "Teste Atlas")
	require.NoError(t, err)
	t.Logf("refund %s", refund)
}

func TestLiveRejectedGPOFails(t *testing.T) {
	client, ctx := liveClient(t), context.Background()
	created, err := client.CreateCharge(ctx, services.ChargeInput{Method: entities.PaymentGPO, MerchantID: merchantID(), AmountCents: 10_000, Currency: "AOA", Description: "Teste Atlas", PhoneNumber: "900000003"})
	if err != nil {
		t.Logf("declined at creation: %v", err)
		return
	}
	require.Eventually(t, func() bool {
		charge, err := client.GetCharge(ctx, created.ID)
		return err == nil && charge.Status == services.ChargeFailed
	}, 90*time.Second, 2*time.Second)
}

func TestLiveReferenceComesWithNumberAndEntity(t *testing.T) {
	client, ctx, id := liveClient(t), context.Background(), merchantID()
	created, err := client.CreateCharge(ctx, services.ChargeInput{Method: entities.PaymentReference, MerchantID: id, AmountCents: 10_000, Currency: "AOA", Description: "Teste Atlas"})
	require.NoError(t, err)
	assert.Equal(t, services.ChargePending, created.Status)
	assert.NotEmpty(t, created.ReferenceNumber)
	assert.NotEmpty(t, created.ReferenceEntity)
	require.NotNil(t, created.DueAt)
	charge, err := client.GetCharge(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, services.ChargePending, charge.Status)
	assert.Equal(t, id, charge.MerchantID)
}
