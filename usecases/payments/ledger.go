package payments

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/payments/dtos"
)

var ErrProviderNotFound = errors.New("provider not found")

func (u *Usecase) post(ctx context.Context, key string, kind entities.LedgerKind, legs []entities.LedgerLeg, currency string, requestID, paymentID, actorID *uuid.UUID, note string, now time.Time) (bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return false, err
	}
	return u.Ledger.Post(ctx, &entities.LedgerTransaction{
		ID: id, IdempotencyKey: key, Kind: kind, RequestID: requestID, PaymentID: paymentID, ActorID: actorID, Currency: currency, Note: note, Legs: legs, CreatedAt: now,
	})
}

func settlementOf(request *entities.ServiceRequest, payment *entities.Payment) entities.Settlement {
	settlement := entities.Settlement{PaidCents: payment.AmountCents, Method: payment.Method, CustomerID: request.CustomerID}
	if request.ProviderID == nil {
		return settlement
	}
	settlement.ProviderID = request.ProviderID
	fare, commission := int64(0), int64(0)
	if request.Fare.Cents != nil {
		fare = *request.Fare.Cents
	}
	if request.Fare.CommissionCents != nil {
		commission = *request.Fare.CommissionCents
	}
	if payment.Kind == entities.PaymentKindCancellationFee {
		if fare > 0 {
			settlement.CommissionCents = payment.AmountCents * commission / fare
		}
		settlement.FareCents = payment.AmountCents
		return settlement
	}
	settlement.FareCents, settlement.CommissionCents, settlement.DiscountCents = fare, commission, request.DiscountCents
	return settlement
}

func (u *Usecase) postPayment(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, now time.Time) error {
	kind := entities.LedgerFarePayment
	var legs []entities.LedgerLeg
	switch payment.Kind {
	case entities.PaymentKindTip:
		if request.ProviderID == nil {
			return entities.ErrPaymentNotPayable
		}
		kind, legs = entities.LedgerTip, entities.TipLegs(payment.Method, request.CustomerID, *request.ProviderID, payment.AmountCents)
	case entities.PaymentKindCancellationFee:
		kind, legs = entities.LedgerCancellationFee, settlementOf(request, payment).Legs()
	case entities.PaymentKindFare:
		if payment.Method == entities.PaymentCash {
			kind = entities.LedgerCashPayment
		}
		legs = settlementOf(request, payment).Legs()
	}
	_, err := u.post(ctx, "payment:"+payment.ID.String(), kind, legs, payment.Currency, &request.ID, &payment.ID, nil, "", now)
	if err != nil {
		return err
	}
	if u.Rewards != nil && payment.Kind == entities.PaymentKindFare {
		return u.Rewards.OnFirstPaidRequest(ctx, request, now)
	}
	return nil
}

func (u *Usecase) creditDuplicate(ctx context.Context, payment *entities.Payment, now time.Time) error {
	customer := payment.CustomerID
	legs := []entities.LedgerLeg{{AccountType: entities.AccountExternal, AmountCents: -payment.AmountCents}, {AccountType: entities.AccountWallet, AccountID: &customer, AmountCents: payment.AmountCents}}
	_, err := u.post(ctx, "payment:"+payment.ID.String(), entities.LedgerWalletCredit, legs, payment.Currency, &payment.RequestID, &payment.ID, nil, "Pagamento em duplicado", now)
	return err
}

func (u *Usecase) payFromWallet(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, now time.Time) error {
	if err := u.Ledger.LockAccount(ctx, entities.AccountWallet, request.CustomerID); err != nil {
		return err
	}
	balance, err := u.Ledger.Balance(ctx, entities.AccountWallet, request.CustomerID)
	if err != nil {
		return err
	}
	if balance < payment.AmountCents {
		return entities.ErrWalletInsufficient
	}
	payment.MarkPaid(now)
	if err := u.Payments.Create(ctx, payment); err != nil {
		return err
	}
	return u.persistChange(ctx, payment, sourceAPI, "paid from the wallet", now)
}

func (u *Usecase) postRefund(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, refundID uuid.UUID, cents int64, now time.Time) error {
	commission := int64(0)
	if payment.Kind == entities.PaymentKindFare {
		commission = settlementOf(request, payment).CommissionCents - request.DiscountCents
	}
	provider := request.ProviderID
	legs := entities.RefundLegs(payment.RefundsToWallet(), request.CustomerID, provider, cents, payment.AmountCents, commission)
	_, err := u.post(ctx, "refund:"+refundID.String(), entities.LedgerRefund, legs, payment.Currency, &request.ID, &payment.ID, nil, "", now)
	return err
}

func (u *Usecase) statement(ctx context.Context, accountType entities.LedgerAccountType, id uuid.UUID, page pagination.Params) (*dtos.AccountStatement, error) {
	page = page.Normalize()
	balance, err := u.Ledger.Balance(ctx, accountType, id)
	if err != nil {
		return nil, err
	}
	lines, total, err := u.Ledger.Lines(ctx, accountType, id, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	view := &dtos.AccountStatement{BalanceCents: balance, Currency: u.settings.Currency, Items: make([]dtos.LedgerLine, 0, len(lines)), Total: total, Page: page.Page, Size: page.Size}
	for i := range lines {
		line := &lines[i]
		view.Items = append(view.Items, dtos.LedgerLine{ID: line.ID, TransactionID: line.TransactionID, Kind: string(line.Kind), AmountCents: line.AmountCents, Currency: line.Currency, RequestID: line.RequestID, Note: line.Note, CreatedAt: line.CreatedAt})
	}
	return view, nil
}

func (u *Usecase) Wallet(ctx context.Context, customerID uuid.UUID, page pagination.Params) (*dtos.AccountStatement, error) {
	return u.statement(ctx, entities.AccountWallet, customerID, page)
}

func (u *Usecase) ProviderLedger(ctx context.Context, providerID uuid.UUID, page pagination.Params) (*dtos.AccountStatement, error) {
	return u.statement(ctx, entities.AccountProvider, providerID, page)
}

func (u *Usecase) Settle(ctx context.Context, actor entities.Actor, providerID uuid.UUID, in dtos.SettlementInput) (*dtos.SettlementResult, error) {
	user, err := u.Users.FindByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrProviderNotFound
	}
	result := &dtos.SettlementResult{Currency: u.settings.Currency}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Ledger.LockAccount(ctx, entities.AccountProvider, providerID); err != nil {
			return err
		}
		balance, err := u.Ledger.Balance(ctx, entities.AccountProvider, providerID)
		if err != nil {
			return err
		}
		legs, err := entities.SettlementLegs(in.Direction, providerID, balance, in.AmountCents)
		if err != nil {
			result.BalanceCents = balance
			return err
		}
		applied, err := u.post(ctx, "settlement:"+providerID.String()+":"+in.Reference, entities.LedgerSettlement, legs, u.settings.Currency, nil, nil, actor.ID, in.Note, u.now())
		if err != nil {
			return err
		}
		result.Applied = applied
		if result.BalanceCents, err = u.Ledger.Balance(ctx, entities.AccountProvider, providerID); err != nil {
			return err
		}
		entry, err := entities.NewAuditEntry(actor, "ledger.settlement", "provider", providerID.String(), nil, in, in.Note, logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		return u.Audit.Record(ctx, entry)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (u *Usecase) SetGatewayFee(ctx context.Context, actor entities.Actor, paymentID uuid.UUID, in dtos.GatewayFeeInput) (*dtos.PaymentView, error) {
	var payment *entities.Payment
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if payment, err = u.Payments.FindForUpdate(ctx, paymentID); err != nil {
			return err
		}
		if payment == nil {
			return ErrPaymentNotFound
		}
		if payment.Status != entities.PaymentPaid || !payment.Method.UsesGateway() {
			return entities.ErrGatewayFeeNotPaid
		}
		changed, err := u.Ledger.SetGatewayFee(ctx, paymentID, in.FeeCents)
		if err != nil {
			return err
		}
		if !changed {
			return entities.ErrGatewayFeeRecorded
		}
		if in.FeeCents > 0 {
			legs := []entities.LedgerLeg{{AccountType: entities.AccountPlatform, AmountCents: -in.FeeCents}, {AccountType: entities.AccountExternal, AmountCents: in.FeeCents}}
			if _, err := u.post(ctx, "gateway-fee:"+paymentID.String(), entities.LedgerGatewayFee, legs, payment.Currency, &payment.RequestID, &paymentID, actor.ID, "", u.now()); err != nil {
				return err
			}
		}
		entry, err := entities.NewAuditEntry(actor, "payment.gateway_fee", auditEntityPayment, paymentID.String(), nil, in, "", logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		return u.Audit.Record(ctx, entry)
	})
	if err != nil {
		return nil, err
	}
	view := toView(payment)
	return &view, nil
}

func (u *Usecase) LedgerSummary(ctx context.Context, page pagination.Params) (*dtos.LedgerSummary, error) {
	page = page.Normalize()
	totals, err := u.Ledger.Totals(ctx)
	if err != nil {
		return nil, err
	}
	unbalanced, err := u.Ledger.UnbalancedCount(ctx)
	if err != nil {
		return nil, err
	}
	balances, err := u.Ledger.ProviderBalances(ctx, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	view := &dtos.LedgerSummary{Totals: make([]dtos.LedgerTotal, 0, len(totals)), Unbalanced: unbalanced, Providers: toBalanceViews(balances)}
	for i := range totals {
		view.Totals = append(view.Totals, dtos.LedgerTotal{Account: string(totals[i].AccountType), TotalCents: totals[i].TotalCents})
	}
	return view, nil
}

func toBalanceViews(rows []repository.ProviderBalance) []dtos.ProviderBalanceView {
	out := make([]dtos.ProviderBalanceView, 0, len(rows))
	for i := range rows {
		out = append(out, dtos.ProviderBalanceView{ProviderID: rows[i].ProviderID, Name: rows[i].Name, BalanceCents: rows[i].BalanceCents})
	}
	return out
}

func (u *Usecase) Tip(ctx context.Context, customerID, requestID uuid.UUID, in dtos.TipInput) (*dtos.PaymentView, bool, error) {
	var payment *entities.Payment
	created := false
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestCompleted || request.ProviderID == nil {
			return entities.ErrTipNotAllowed
		}
		paid, err := u.Payments.PaidOfRequest(ctx, requestID, entities.PaymentKindTip)
		if err != nil {
			return err
		}
		if paid != nil {
			return entities.ErrTipAlreadyGiven
		}
		if payment, err = u.Payments.PendingOfRequest(ctx, requestID, entities.PaymentKindTip); err != nil || payment != nil {
			return err
		}
		method := entities.PaymentMethod(in.Method)
		if method == "" {
			method = request.PaymentMethod
			if method == entities.PaymentCash || method == entities.PaymentReference {
				method = entities.PaymentGPO
			}
		}
		maxTip, err := u.maxTipCents(ctx)
		if err != nil {
			return err
		}
		if in.AmountCents > maxTip || (method.UsesGateway() && in.AmountCents < entities.MinGatewayCents) {
			return entities.ErrTipOutOfRange
		}
		if method.UsesGateway() && !u.Gateway.Enabled() {
			return services.ErrPaymentsUnavailable
		}
		now := u.now()
		phone := ""
		if method == entities.PaymentGPO {
			if phone, err = u.payerPhone(ctx, customerID, in.PhoneNumber); err != nil {
				return err
			}
		}
		if payment, err = entities.NewPayment(entities.NewPaymentInput{Request: request, Method: method, Phone: phone, Now: now, TipCents: in.AmountCents}); err != nil {
			return err
		}
		created = true
		in.PhoneNumber = phone
		if method == entities.PaymentWallet {
			return u.payFromWallet(ctx, request, payment, now)
		}
		expires := now.Add(u.windowOf(method))
		payment.ExpiresAt = &expires
		payment.CheckAgainAt(now.Add(u.checkEveryOf(method)), now)
		if err := u.Payments.Create(ctx, payment); err != nil {
			return err
		}
		if err := u.record(ctx, payment, sourceAPI, "tip created", now); err != nil {
			return err
		}
		return u.publish(ctx, request, payment, method, string(payment.Status), now)
	})
	if err != nil {
		return nil, false, err
	}
	if created && payment.Method.UsesGateway() {
		if err := u.charge(ctx, payment, in.PhoneNumber); err != nil {
			return nil, false, err
		}
		if payment, err = u.Payments.Find(ctx, payment.ID); err != nil {
			return nil, false, err
		}
	}
	view := toView(payment)
	return &view, created, nil
}

func (u *Usecase) maxTipCents(ctx context.Context) (int64, error) {
	raw, err := u.Settings.Setting(ctx, entities.SettingMaxTipCents)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}
