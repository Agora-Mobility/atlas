package entities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

func amounts(legs []entities.LedgerLeg) map[entities.LedgerAccountType]int64 {
	out := map[entities.LedgerAccountType]int64{}
	for _, leg := range legs {
		out[leg.AccountType] += leg.AmountCents
	}
	return out
}

func balanced(t *testing.T, legs []entities.LedgerLeg) {
	t.Helper()
	sum := int64(0)
	for _, leg := range legs {
		sum += leg.AmountCents
	}
	assert.Zero(t, sum)
}

func TestSettlementSplitsAPayment(t *testing.T) {
	provider := uuid.New()
	digital := entities.Settlement{FareCents: 100_000, CommissionCents: 20_000, PaidCents: 100_000, Method: entities.PaymentGPO, ProviderID: &provider}.Legs()
	balanced(t, digital)
	assert.Equal(t, map[entities.LedgerAccountType]int64{entities.AccountExternal: -100_000, entities.AccountProvider: 80_000, entities.AccountPlatform: 20_000}, amounts(digital))

	cash := entities.Settlement{FareCents: 100_000, CommissionCents: 20_000, PaidCents: 100_000, Method: entities.PaymentCash, ProviderID: &provider}.Legs()
	balanced(t, cash)
	assert.EqualValues(t, -20_000, amounts(cash)[entities.AccountProvider], "the driver holds the cash, so the commission is his debt")
	assert.EqualValues(t, 20_000, amounts(cash)[entities.AccountPlatform])

	voucher := entities.Settlement{FareCents: 100_000, CommissionCents: 20_000, DiscountCents: 10_000, PaidCents: 90_000, Method: entities.PaymentGPO, ProviderID: &provider}.Legs()
	balanced(t, voucher)
	assert.EqualValues(t, 80_000, amounts(voucher)[entities.AccountProvider], "the voucher never reduces what the driver earns")
	assert.EqualValues(t, 10_000, amounts(voucher)[entities.AccountPlatform], "AGORA pays for it")

	cashVoucher := entities.Settlement{FareCents: 100_000, CommissionCents: 20_000, DiscountCents: 30_000, PaidCents: 70_000, Method: entities.PaymentCash, ProviderID: &provider}.Legs()
	balanced(t, cashVoucher)
	assert.EqualValues(t, 10_000, amounts(cashVoucher)[entities.AccountProvider], "AGORA owes the driver what the cash did not cover")

	wallet := entities.Settlement{FareCents: 100_000, CommissionCents: 20_000, PaidCents: 100_000, Method: entities.PaymentWallet, ProviderID: &provider, CustomerID: uuid.New()}.Legs()
	balanced(t, wallet)
	assert.EqualValues(t, -100_000, amounts(wallet)[entities.AccountWallet])

	noProvider := entities.Settlement{PaidCents: 5_000, Method: entities.PaymentGPO}.Legs()
	balanced(t, noProvider)
	assert.EqualValues(t, 5_000, amounts(noProvider)[entities.AccountPlatform])
}

func TestRefundTakesItBackInProportion(t *testing.T) {
	provider := uuid.New()
	legs := entities.RefundLegs(false, uuid.New(), &provider, 50_000, 100_000, 20_000)
	balanced(t, legs)
	assert.Equal(t, map[entities.LedgerAccountType]int64{entities.AccountExternal: 50_000, entities.AccountProvider: -40_000, entities.AccountPlatform: -10_000}, amounts(legs))

	wallet := entities.RefundLegs(true, uuid.New(), &provider, 50_000, 100_000, 20_000)
	balanced(t, wallet)
	assert.EqualValues(t, 50_000, amounts(wallet)[entities.AccountWallet], "a reference comes back to the wallet")
}

func TestSettlementCannotExceedTheBalance(t *testing.T) {
	provider := uuid.New()
	_, err := entities.SettlementLegs(entities.SettlementPayout, provider, 100, 101)
	assert.ErrorIs(t, err, entities.ErrSettlementTooLarge)
	_, err = entities.SettlementLegs(entities.SettlementCollect, provider, -100, 101)
	assert.ErrorIs(t, err, entities.ErrSettlementTooLarge)
	_, err = entities.SettlementLegs(entities.SettlementCollect, provider, 100, 1)
	assert.ErrorIs(t, err, entities.ErrSettlementTooLarge, "there is no debt to collect")
	_, err = entities.SettlementLegs(entities.SettlementPayout, provider, 100, 0)
	assert.ErrorIs(t, err, entities.ErrInvalidLedgerAmount)
	legs, err := entities.SettlementLegs(entities.SettlementPayout, provider, 100, 100)
	require.NoError(t, err)
	balanced(t, legs)
}

func TestAnUnbalancedTransactionIsRefused(t *testing.T) {
	transaction := entities.LedgerTransaction{Legs: []entities.LedgerLeg{{AccountType: entities.AccountPlatform, AmountCents: 10}, {AccountType: entities.AccountExternal, AmountCents: -9}}}
	assert.ErrorIs(t, transaction.Validate(), entities.ErrLedgerNotBalanced)
}
