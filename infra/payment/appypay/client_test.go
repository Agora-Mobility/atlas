package appypay_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/payment/appypay"
)

type fixture struct {
	client *appypay.Client
	tokens atomic.Int64
	last   map[string]any
	accept string
	handle func(w http.ResponseWriter, r *http.Request)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			f.tokens.Add(1)
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), "grant_type=client_credentials")
			assert.Contains(t, string(body), "client_id=id")
			_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":"3599","access_token":"token"}`))
			return
		}
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		f.accept = r.Header.Get("Accept")
		if r.Method == http.MethodPost {
			f.last = map[string]any{}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&f.last))
		}
		f.handle(w, r)
	}))
	t.Cleanup(server.Close)
	f.client = appypay.New(appypay.Settings{
		ClientID: "id", ClientSecret: "secret", BaseURL: server.URL + "/", TokenURL: server.URL + "/token", Resource: "res",
		GPOMethod: "GPO_key", REFMethod: "REF_key", Timeout: 2 * time.Second,
	})
	return f
}

func TestReferenceChargeIsAsyncAndReadsTheReference(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"c1","responseStatus":{"successful":true,"status":"Pending","code":101,"message":"ok","source":"REF","reference":{"referenceNumber":"968481213","dueDate":"2026-11-05T00:07:40.8551941+00:00","entity":"00348","nib":null}}}`))
	}
	charge, err := f.client.CreateCharge(context.Background(), services.ChargeInput{Method: entities.PaymentReference, MerchantID: "AG1", AmountCents: 171_505, Currency: "AOA", Description: "Pedido"})
	require.NoError(t, err)
	assert.Equal(t, services.ChargePending, charge.Status)
	assert.Equal(t, "968481213", charge.ReferenceNumber)
	assert.Equal(t, "00348", charge.ReferenceEntity)
	require.NotNil(t, charge.DueAt)
	assert.Equal(t, 2026, charge.DueAt.Year())
	assert.Equal(t, "application/vnd.appypay.asyncapi+json", f.accept)
	assert.Equal(t, "REF_key", f.last["paymentMethod"])
	assert.Equal(t, "AG1", f.last["merchantTransactionId"])
	assert.InDelta(t, 1715.05, f.last["amount"], 0.0001, "cents become kwanza with two decimals")
	assert.Nil(t, f.last["paymentInfo"], "the gateway generates the reference")
}

func TestGPOChargeSendsThePhone(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"c2","responseStatus":{"successful":true,"code":101,"source":"GPO"}}`))
	}
	charge, err := f.client.CreateCharge(context.Background(), services.ChargeInput{Method: entities.PaymentGPO, MerchantID: "AG2", AmountCents: 10_000, Currency: "AOA", PhoneNumber: "900000000"})
	require.NoError(t, err)
	assert.Equal(t, services.ChargePending, charge.Status, "without a status the charge is still open")
	assert.Equal(t, "GPO_key", f.last["paymentMethod"])
	assert.Equal(t, map[string]any{"phoneNumber": "900000000"}, f.last["paymentInfo"])
}

func TestClientErrorIsADeclineButServerErrorIsNot(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"responseStatus":{"successful":false,"code":722,"message":"Saldo insuficiente"}}`))
	}
	input := services.ChargeInput{Method: entities.PaymentGPO, MerchantID: "AG3", AmountCents: 100, Currency: "AOA"}
	_, err := f.client.CreateCharge(context.Background(), input)
	var declined *services.PaymentDeclinedError
	require.True(t, errors.As(err, &declined))
	assert.Equal(t, "722", declined.Code)
	assert.Equal(t, "Saldo insuficiente", declined.Message)

	f.handle = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }
	_, err = f.client.CreateCharge(context.Background(), input)
	require.Error(t, err)
	assert.False(t, errors.As(err, &declined), "a server error leaves the outcome open")

	_, err = f.client.CreateCharge(context.Background(), services.ChargeInput{Method: entities.PaymentCash})
	assert.Error(t, err)
}

func TestGetChargeReadsThePaymentObject(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/charges/c4", r.URL.Path)
		_, _ = w.Write([]byte(`{"payment":{"id":"c4","merchantTransactionId":"AG4","amount":100.0000,"status":"Success","reference":{"referenceNumber":"1","dueDate":"2026-11-05T00:07:40.8551941","entity":"2"}}}`))
	}
	charge, err := f.client.GetCharge(context.Background(), "c4")
	require.NoError(t, err)
	assert.Equal(t, services.ChargeSuccess, charge.Status)
	assert.EqualValues(t, 10_000, charge.AmountCents)
	assert.Equal(t, "AG4", charge.MerchantID)
	require.NotNil(t, charge.DueAt, "the date without an offset is read too")

	f.handle = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	_, err = f.client.GetCharge(context.Background(), "c4")
	assert.ErrorIs(t, err, services.ErrChargeNotFound)
}

func TestChargeIDsAreComparedInLowerCase(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"payments":[{"id":"4B7D-AB","merchantTransactionId":"AG8","amount":1,"status":"Success"}]}`))
	}
	charge, err := f.client.FindChargeByMerchantID(context.Background(), "AG8")
	require.NoError(t, err)
	assert.Equal(t, "4b7d-ab", charge.ID)
}

func TestFindByMerchantOnlyAcceptsTheExactMatch(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "AG5", r.URL.Query().Get("merchantTransactionId"))
		_, _ = w.Write([]byte(`{"payments":[{"id":"x","merchantTransactionId":"OTHER","amount":1,"status":"Success"}]}`))
	}
	_, err := f.client.FindChargeByMerchantID(context.Background(), "AG5")
	assert.ErrorIs(t, err, services.ErrChargeNotFound)
}

func TestTokenIsReusedAndRenewedAfterRejection(t *testing.T) {
	f := newFixture(t)
	var calls atomic.Int64
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 2 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"payment":{"id":"c6","merchantTransactionId":"AG6","amount":1,"status":"Pending"}}`))
	}
	_, err := f.client.GetCharge(context.Background(), "c6")
	require.NoError(t, err)
	assert.EqualValues(t, 1, f.tokens.Load())
	_, err = f.client.GetCharge(context.Background(), "c6")
	require.NoError(t, err)
	assert.EqualValues(t, 2, f.tokens.Load(), "a rejected token is replaced once")
}

func TestRefund(t *testing.T) {
	f := newFixture(t)
	f.handle = func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/refunds/c7"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r7","responseStatus":{"successful":true,"code":100}}`))
	}
	id, err := f.client.Refund(context.Background(), "c7", 5_000, "Reembolso")
	require.NoError(t, err)
	assert.Equal(t, "r7", id)
	assert.InDelta(t, 50.0, f.last["amount"], 0.0001)

	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"responseStatus":{"code":722,"message":"Não encontrado"}}`))
	}
	_, err = f.client.Refund(context.Background(), "c7", 5_000, "Reembolso")
	var declined *services.PaymentDeclinedError
	assert.True(t, errors.As(err, &declined))
}

func TestUnavailableGatewayRefusesEverything(t *testing.T) {
	gateway := appypay.Unavailable{}
	assert.False(t, gateway.Enabled())
	_, err := gateway.CreateCharge(context.Background(), services.ChargeInput{})
	assert.ErrorIs(t, err, services.ErrPaymentsUnavailable)
}
