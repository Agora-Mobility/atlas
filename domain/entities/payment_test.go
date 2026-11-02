package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var paymentNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func payableRequest(cents int64) *entities.ServiceRequest {
	return &entities.ServiceRequest{ID: uuid.New(), CustomerID: uuid.New(), Fare: entities.RequestFare{Cents: &cents, Currency: "AOA"}}
}

func TestMerchantIDFitsTheGateway(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		id, err := entities.NewMerchantID()
		require.NoError(t, err)
		assert.Regexp(t, `^[a-zA-Z0-9]{15}$`, id)
		assert.False(t, seen[id])
		seen[id] = true
	}
}

func TestPayerPhoneIsNormalizedAndMasked(t *testing.T) {
	phone, err := entities.NormalizePayerPhone("+244 923 000 111")
	require.NoError(t, err)
	assert.Equal(t, "244923000111", phone)
	assert.Equal(t, "*********111", entities.MaskPhone(phone))
	for _, invalid := range []string{"", "12345678", "abc923000111", "1234567890123456"} {
		_, err := entities.NormalizePayerPhone(invalid)
		assert.Error(t, err, invalid)
	}
}

func TestNewPaymentTakesTheAmountFromTheRequest(t *testing.T) {
	request := payableRequest(171_500)
	payment, err := entities.NewPayment(entities.NewPaymentInput{Request: request, Method: entities.PaymentGPO, Phone: "923000111", Now: paymentNow})
	require.NoError(t, err)
	assert.EqualValues(t, 171_500, payment.AmountCents)
	assert.Equal(t, entities.PaymentPending, payment.Status)
	assert.Equal(t, "******111", payment.PayerPhoneMasked)

	_, err = entities.NewPayment(entities.NewPaymentInput{Request: &entities.ServiceRequest{}, Method: entities.PaymentGPO, Now: paymentNow})
	assert.ErrorIs(t, err, entities.ErrPaymentNotPayable, "without a fare there is nothing to charge")
	_, err = entities.NewPayment(entities.NewPaymentInput{Request: payableRequest(99), Method: entities.PaymentGPO, Now: paymentNow})
	assert.ErrorIs(t, err, entities.ErrPaymentNotPayable, "the gateway minimum is 1 AOA")
	_, err = entities.NewPayment(entities.NewPaymentInput{Request: payableRequest(99), Method: entities.PaymentCash, Now: paymentNow})
	assert.NoError(t, err)
}

func TestRefundRules(t *testing.T) {
	charge := "charge"
	payment := &entities.Payment{Method: entities.PaymentGPO, Status: entities.PaymentPaid, AmountCents: 1000, ProviderChargeID: &charge}
	assert.NoError(t, payment.CheckRefund(400))
	assert.ErrorIs(t, payment.CheckRefund(1001), entities.ErrRefundTooLarge)
	assert.ErrorIs(t, payment.CheckRefund(0), entities.ErrRefundTooLarge)
	payment.AddRefund(400, paymentNow)
	assert.EqualValues(t, 600, payment.RefundableCents())
	assert.ErrorIs(t, payment.CheckRefund(601), entities.ErrRefundTooLarge)

	for _, other := range []*entities.Payment{
		{Method: entities.PaymentReference, Status: entities.PaymentPaid, AmountCents: 1000, ProviderChargeID: &charge},
		{Method: entities.PaymentCash, Status: entities.PaymentPaid, AmountCents: 1000},
		{Method: entities.PaymentGPO, Status: entities.PaymentPending, AmountCents: 1000, ProviderChargeID: &charge},
	} {
		assert.ErrorIs(t, other.CheckRefund(100), entities.ErrRefundNotSupported)
	}
}

func TestPaymentExpiry(t *testing.T) {
	payment := &entities.Payment{}
	assert.False(t, payment.IsPastExpiry(paymentNow))
	due := paymentNow.Add(time.Minute)
	payment.ExpiresAt = &due
	assert.False(t, payment.IsPastExpiry(paymentNow))
	assert.True(t, payment.IsPastExpiry(due))
}
