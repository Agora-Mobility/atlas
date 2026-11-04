package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type LedgerAccountType string

const (
	AccountProvider LedgerAccountType = "provider"
	AccountWallet   LedgerAccountType = "wallet"
	AccountPlatform LedgerAccountType = "platform"
	AccountExternal LedgerAccountType = "external"
	AccountWorkshop LedgerAccountType = "workshop"
)

type LedgerKind string

const (
	LedgerFarePayment     LedgerKind = "fare_payment"
	LedgerCashPayment     LedgerKind = "cash_payment"
	LedgerTip             LedgerKind = "tip"
	LedgerCancellationFee LedgerKind = "cancellation_fee"
	LedgerRefund          LedgerKind = "refund"
	LedgerSettlement      LedgerKind = "settlement"
	LedgerWalletCredit    LedgerKind = "wallet_credit"
	LedgerGatewayFee      LedgerKind = "gateway_fee"
	LedgerReferralReward  LedgerKind = "referral_reward"
)

const (
	SettlementPayout  = "payout"
	SettlementCollect = "collect"
)

var (
	ErrLedgerNotBalanced   = errors.New("a ledger transaction has to add up to zero")
	ErrWalletInsufficient  = errors.New("the wallet does not have enough balance")
	ErrSettlementTooLarge  = errors.New("the settlement is above the balance of the provider")
	ErrGatewayFeeRecorded  = errors.New("the gateway fee of this payment is already recorded")
	ErrGatewayFeeNotPaid   = errors.New("only a paid digital payment has a gateway fee")
	ErrInvalidLedgerAmount = errors.New("the amount is not valid")
)

type LedgerLeg struct {
	AccountType LedgerAccountType
	AccountID   *uuid.UUID
	AmountCents int64
}

type LedgerTransaction struct {
	ID             uuid.UUID
	IdempotencyKey string
	Kind           LedgerKind
	RequestID      *uuid.UUID
	PaymentID      *uuid.UUID
	Note           string
	ActorID        *uuid.UUID
	Currency       string
	Legs           []LedgerLeg
	CreatedAt      time.Time
}

func (t *LedgerTransaction) Validate() error {
	sum := int64(0)
	for _, leg := range t.Legs {
		sum += leg.AmountCents
	}
	if sum != 0 || len(t.Legs) < 2 {
		return ErrLedgerNotBalanced
	}
	return nil
}

func nonZeroLegs(legs ...LedgerLeg) []LedgerLeg {
	out := make([]LedgerLeg, 0, len(legs))
	for _, leg := range legs {
		if leg.AmountCents != 0 {
			out = append(out, leg)
		}
	}
	return out
}

type Settlement struct {
	FareCents       int64
	CommissionCents int64
	DiscountCents   int64
	PaidCents       int64
	Method          PaymentMethod
	ProviderID      *uuid.UUID
	PayeeType       LedgerAccountType
	CustomerID      uuid.UUID
}

func (s Settlement) payee() LedgerAccountType {
	if s.PayeeType != "" {
		return s.PayeeType
	}
	return AccountProvider
}

func (s Settlement) payerLeg() LedgerLeg {
	if s.Method == PaymentWallet {
		customer := s.CustomerID
		return LedgerLeg{AccountType: AccountWallet, AccountID: &customer, AmountCents: -s.PaidCents}
	}
	return LedgerLeg{AccountType: AccountExternal, AmountCents: -s.PaidCents}
}

// Legs splits a payment. The provider earns the fare minus the commission, and
// the platform pays for the discount. With cash the driver already holds the
// money, so the provider account takes the difference, which is a debt whenever
// the commission is higher than the discount.
func (s Settlement) Legs() []LedgerLeg {
	platform := s.CommissionCents - s.DiscountCents
	if s.ProviderID == nil {
		return nonZeroLegs(s.payerLeg(), LedgerLeg{AccountType: AccountPlatform, AmountCents: s.PaidCents})
	}
	net := s.FareCents - s.CommissionCents
	if s.Method == PaymentCash {
		return nonZeroLegs(LedgerLeg{AccountType: s.payee(), AccountID: s.ProviderID, AmountCents: net - s.PaidCents}, LedgerLeg{AccountType: AccountPlatform, AmountCents: platform})
	}
	return nonZeroLegs(s.payerLeg(), LedgerLeg{AccountType: s.payee(), AccountID: s.ProviderID, AmountCents: net}, LedgerLeg{AccountType: AccountPlatform, AmountCents: platform})
}

func TipLegs(method PaymentMethod, customerID, providerID uuid.UUID, cents int64) []LedgerLeg {
	return TipLegsTo(AccountProvider, method, customerID, providerID, cents)
}

func TipLegsTo(payee LedgerAccountType, method PaymentMethod, customerID, payeeID uuid.UUID, cents int64) []LedgerLeg {
	payer := Settlement{Method: method, CustomerID: customerID, PaidCents: cents}.payerLeg()
	return []LedgerLeg{payer, {AccountType: payee, AccountID: &payeeID, AmountCents: cents}}
}

// RefundLegs takes the money back from the provider and the platform in the
// proportion they received it.
func RefundLegs(toWallet bool, customerID uuid.UUID, providerID *uuid.UUID, cents, paidCents, commissionCents int64) []LedgerLeg {
	return RefundLegsOf(AccountProvider, toWallet, customerID, providerID, cents, paidCents, commissionCents)
}

func RefundLegsOf(payee LedgerAccountType, toWallet bool, customerID uuid.UUID, providerID *uuid.UUID, cents, paidCents, commissionCents int64) []LedgerLeg {
	target := LedgerLeg{AccountType: AccountExternal, AmountCents: cents}
	if toWallet {
		target = LedgerLeg{AccountType: AccountWallet, AccountID: &customerID, AmountCents: cents}
	}
	if providerID == nil || paidCents <= 0 {
		return nonZeroLegs(target, LedgerLeg{AccountType: AccountPlatform, AmountCents: -cents})
	}
	fromPlatform := roundedDivide(cents*commissionCents, paidCents)
	return nonZeroLegs(target, LedgerLeg{AccountType: payee, AccountID: providerID, AmountCents: -(cents - fromPlatform)}, LedgerLeg{AccountType: AccountPlatform, AmountCents: -fromPlatform})
}

func SettlementLegs(direction string, providerID uuid.UUID, balance, cents int64) ([]LedgerLeg, error) {
	return SettlementLegsOf(AccountProvider, direction, providerID, balance, cents)
}

func SettlementLegsOf(account LedgerAccountType, direction string, providerID uuid.UUID, balance, cents int64) ([]LedgerLeg, error) {
	if cents <= 0 || cents > MaxFeeCents {
		return nil, ErrInvalidLedgerAmount
	}
	switch direction {
	case SettlementPayout:
		if cents > balance {
			return nil, ErrSettlementTooLarge
		}
		return []LedgerLeg{{AccountType: account, AccountID: &providerID, AmountCents: -cents}, {AccountType: AccountExternal, AmountCents: cents}}, nil
	case SettlementCollect:
		if cents > -balance {
			return nil, ErrSettlementTooLarge
		}
		return []LedgerLeg{{AccountType: account, AccountID: &providerID, AmountCents: cents}, {AccountType: AccountExternal, AmountCents: -cents}}, nil
	}
	return nil, ErrInvalidLedgerAmount
}
