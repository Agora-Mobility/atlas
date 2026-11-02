package entities

import (
	"crypto/rand"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PaymentMethod string

const (
	PaymentCash      PaymentMethod = "cash"
	PaymentGPO       PaymentMethod = "gpo"
	PaymentReference PaymentMethod = "reference"
)

type PaymentStatus string

const (
	PaymentPending PaymentStatus = "pending"
	PaymentPaid    PaymentStatus = "paid"
	PaymentFailed  PaymentStatus = "failed"
	PaymentExpired PaymentStatus = "expired"
)

type RequestPaymentStatus string

const (
	RequestUnpaid        RequestPaymentStatus = "unpaid"
	RequestPaymentWait   RequestPaymentStatus = "pending"
	RequestPaid          RequestPaymentStatus = "paid"
	RequestFullyRefunded RequestPaymentStatus = "refunded"
)

const (
	merchantIDLength    = 15
	merchantIDPrefix    = "AG"
	merchantIDAlphabet  = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	merchantAlphabetLen = 32
	MinGatewayCents     = 100
	maskedPhoneVisible  = 3
)

var (
	ErrPaymentMethodUnavailable = errors.New("this payment method is not available for this service")
	ErrPaymentOutstanding       = errors.New("the customer has a completed request to pay")
	ErrPaymentNotPayable        = errors.New("this request cannot be paid now")
	ErrPaymentAlreadyPaid       = errors.New("this request is already paid")
	ErrPaymentPendingElsewhere  = errors.New("this request has a payment waiting for the customer")
	ErrPaymentMethodLocked      = errors.New("the method of this request cannot change now")
	ErrRefundNotSupported       = errors.New("this payment cannot be refunded through the gateway")
	ErrRefundTooLarge           = errors.New("the refund is above what is left of the payment")

	payerPhonePattern = regexp.MustCompile(`^\d{9,15}$`)
)

func IsPaymentMethod(method PaymentMethod) bool {
	return method == PaymentCash || method == PaymentGPO || method == PaymentReference
}

func NewMerchantID() (string, error) {
	random := make([]byte, merchantIDLength-len(merchantIDPrefix))
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	var id strings.Builder
	id.WriteString(merchantIDPrefix)
	for _, value := range random {
		id.WriteByte(merchantIDAlphabet[int(value)%merchantAlphabetLen])
	}
	return id.String(), nil
}

func NormalizePayerPhone(raw string) (string, error) {
	digits := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""), "+")
	if !payerPhonePattern.MatchString(digits) {
		return "", validationError("phone_number", "O número tem de ter entre 9 e 15 dígitos.")
	}
	return digits, nil
}

func MaskPhone(digits string) string {
	if len(digits) <= maskedPhoneVisible {
		return digits
	}
	return strings.Repeat("*", len(digits)-maskedPhoneVisible) + digits[len(digits)-maskedPhoneVisible:]
}

type Payment struct {
	ID               uuid.UUID
	RequestID        uuid.UUID
	CustomerID       uuid.UUID
	Method           PaymentMethod
	Status           PaymentStatus
	AmountCents      int64
	RefundedCents    int64
	Currency         string
	MerchantID       string
	ProviderChargeID *string
	PayerPhoneMasked string
	ReferenceNumber  string
	ReferenceEntity  string
	FailureMessage   string
	ExpiresAt        *time.Time
	NextCheckAt      *time.Time
	PaidAt           *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type NewPaymentInput struct {
	Request *ServiceRequest
	Method  PaymentMethod
	Phone   string
	Now     time.Time
}

func NewPayment(in NewPaymentInput) (*Payment, error) {
	if in.Request.Fare.Cents == nil || *in.Request.Fare.Cents <= 0 {
		return nil, ErrPaymentNotPayable
	}
	amount := *in.Request.Fare.Cents
	if in.Method != PaymentCash && amount < MinGatewayCents {
		return nil, ErrPaymentNotPayable
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	merchantID, err := NewMerchantID()
	if err != nil {
		return nil, err
	}
	return &Payment{
		ID: id, RequestID: in.Request.ID, CustomerID: in.Request.CustomerID, Method: in.Method, Status: PaymentPending, AmountCents: amount,
		Currency: in.Request.Fare.Currency, MerchantID: merchantID, PayerPhoneMasked: MaskPhone(in.Phone), CreatedAt: in.Now, UpdatedAt: in.Now,
	}, nil
}

func (p *Payment) MarkPaid(now time.Time) {
	p.Status = PaymentPaid
	p.PaidAt = &now
	p.FailureMessage = ""
	p.NextCheckAt = nil
	p.UpdatedAt = now
}

func (p *Payment) MarkFailed(message string, now time.Time) {
	p.Status = PaymentFailed
	p.FailureMessage = message
	p.NextCheckAt = nil
	p.UpdatedAt = now
}

func (p *Payment) MarkExpired(now time.Time) {
	p.Status = PaymentExpired
	p.NextCheckAt = nil
	p.UpdatedAt = now
}

func (p *Payment) CheckAgainAt(at, now time.Time) {
	p.NextCheckAt = &at
	p.UpdatedAt = now
}

func (p *Payment) IsPastExpiry(now time.Time) bool {
	return p.ExpiresAt != nil && !now.Before(*p.ExpiresAt)
}

func (p *Payment) RefundableCents() int64 {
	return p.AmountCents - p.RefundedCents
}

func (p *Payment) CheckRefund(cents int64) error {
	if p.Status != PaymentPaid || p.Method != PaymentGPO || p.ProviderChargeID == nil {
		return ErrRefundNotSupported
	}
	if cents <= 0 || cents > p.RefundableCents() {
		return ErrRefundTooLarge
	}
	return nil
}

func (p *Payment) AddRefund(cents int64, now time.Time) {
	p.RefundedCents += cents
	p.UpdatedAt = now
}
