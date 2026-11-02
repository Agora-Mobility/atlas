package payments

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/payments/dtos"
)

var (
	ErrRequestNotFound = errors.New("request not found")
	ErrPaymentNotFound = errors.New("payment not found")
	ErrRefundDeclined  = errors.New("the gateway did not accept the refund")
)

const (
	sourceAPI       = "api"
	sourceWebhook   = "webhook"
	sourceReconcile = "reconcile"
	sourceProvider  = "provider"
	sourceAdmin     = "admin"

	auditEntityPayment = "payment"
)

type Settings struct {
	GPOWindow              time.Duration
	ReferenceTTL           time.Duration
	ReconcileInterval      time.Duration
	ReconcileBatch         int
	GPOCheckInterval       time.Duration
	ReferenceCheckInterval time.Duration
	Currency               string
}

type Deps struct {
	Payments repository.IPaymentRepository
	Requests repository.IRequestRepository
	Users    repository.IUserRepository
	Outbox   repository.IOutboxRepository
	Audit    repository.IAuditLogRepository
	Tx       repository.ITransactionManager
	Gateway  services.IPaymentGateway
	Ledger   repository.ILedgerRepository
	Rewards  RewardHook
	Prepaid  PrepaidHook
	Settings repository.IReferenceRepository
}

type RewardHook interface {
	OnFirstPaidRequest(ctx context.Context, request *entities.ServiceRequest, now time.Time) error
}

// PrepaidHook is told about the reference that holds a booking: it confirms when
// paid and frees the slot when it expires.
type PrepaidHook interface {
	OnPrepaid(ctx context.Context, request *entities.ServiceRequest, now time.Time) error
	OnPrepayExpired(ctx context.Context, request *entities.ServiceRequest, now time.Time) error
}

type Usecase struct {
	Deps
	settings Settings
	now      func() time.Time
}

func NewUsecase(deps Deps, settings Settings) *Usecase {
	return &Usecase{Deps: deps, settings: settings, now: func() time.Time { return time.Now().UTC() }}
}

func toView(p *entities.Payment) dtos.PaymentView {
	view := dtos.PaymentView{
		ID: p.ID, RequestID: p.RequestID, Kind: string(p.Kind), Method: string(p.Method), Status: string(p.Status), AmountCents: p.AmountCents, RefundedCents: p.RefundedCents, Currency: p.Currency,
		Code: p.MerchantID, PayerPhone: p.PayerPhoneMasked, FailureMessage: p.FailureMessage, ExpiresAt: p.ExpiresAt, PaidAt: p.PaidAt, CreatedAt: p.CreatedAt,
	}
	if p.ReferenceNumber != "" {
		view.Reference = &dtos.ReferenceView{Number: p.ReferenceNumber, Entity: p.ReferenceEntity, DueAt: p.ExpiresAt}
	}
	return view
}

func (u *Usecase) publish(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, method entities.PaymentMethod, status string, now time.Time) error {
	event := events.PaymentChangedV1{
		RequestID: request.ID, CustomerID: request.CustomerID, ProviderID: request.ProviderID, Method: string(method), Status: status, Currency: request.Fare.Currency, OccurredAt: now,
	}
	if payment != nil {
		event.PaymentID, event.AmountCents, event.Currency = &payment.ID, payment.AmountCents, payment.Currency
	}
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Outbox.Save(ctx, envelope)
}

func (u *Usecase) record(ctx context.Context, payment *entities.Payment, source, detail string, now time.Time) error {
	return u.Payments.AddEvent(ctx, payment.ID, repository.PaymentEvent{Source: source, Status: string(payment.Status), Detail: detail, OccurredAt: now})
}

func (u *Usecase) Methods(ctx context.Context, category string, onlyEnabled bool) (*dtos.MethodList, error) {
	methods, err := u.Payments.ListMethods(ctx, category, onlyEnabled)
	if err != nil {
		return nil, err
	}
	list := &dtos.MethodList{Items: make([]dtos.MethodView, 0, len(methods))}
	for i := range methods {
		method := &methods[i]
		enabled := method.IsEnabled
		if method.Method.UsesGateway() && !u.Gateway.Enabled() {
			if onlyEnabled {
				continue
			}
			enabled = false
		}
		list.Items = append(list.Items, dtos.MethodView{CategoryID: method.CategoryID, CategorySlug: method.CategorySlug, CategoryName: method.CategoryName, Method: string(method.Method), IsEnabled: enabled})
	}
	return list, nil
}

func (u *Usecase) SetMethod(ctx context.Context, actor entities.Actor, in dtos.SetMethodInput) (*dtos.MethodList, error) {
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		changed, err := u.Payments.SetMethod(ctx, in.CategoryID, entities.PaymentMethod(in.Method), in.IsEnabled)
		if err != nil {
			return err
		}
		if !changed {
			return entities.ErrPaymentMethodUnavailable
		}
		entry, err := entities.NewAuditEntry(actor, "payment_method.set", "category", in.CategoryID.String(), nil, in, "", logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		return u.Audit.Record(ctx, entry)
	})
	if err != nil {
		return nil, err
	}
	return u.Methods(ctx, "", false)
}

func (u *Usecase) ChangeMethod(ctx context.Context, customerID, requestID uuid.UUID, in dtos.ChangeMethodInput) (*dtos.ChangeMethodInput, error) {
	method := entities.PaymentMethod(in.Method)
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.Status == entities.RequestCancelled || request.Status == entities.RequestExpired || request.PaymentStatus != entities.RequestUnpaid {
			return entities.ErrPaymentMethodLocked
		}
		if request.Status == entities.RequestCompleted && method == entities.PaymentCash {
			return entities.ErrPaymentMethodLocked
		}
		booked := request.WorkshopID != nil || request.RemovalDay != nil
		if request.Prepay || (booked && method == entities.PaymentReference && request.Status != entities.RequestCompleted) {
			return entities.ErrPaymentMethodLocked
		}
		enabled, err := u.Payments.IsMethodEnabled(ctx, request.CategoryID, method)
		if err != nil {
			return err
		}
		if !enabled || (method.UsesGateway() && !u.Gateway.Enabled()) {
			return entities.ErrPaymentMethodUnavailable
		}
		if err := u.Payments.SetRequestPayment(ctx, requestID, method, request.PaymentStatus); err != nil {
			return err
		}
		return u.publish(ctx, request, nil, method, string(request.PaymentStatus), u.now())
	})
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (u *Usecase) payerPhone(ctx context.Context, customerID uuid.UUID, requested string) (string, error) {
	if requested != "" {
		return entities.NormalizePayerPhone(requested)
	}
	user, err := u.Users.FindByID(ctx, customerID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrRequestNotFound
	}
	return entities.NormalizePayerPhone(user.Phone)
}

func (u *Usecase) Pay(ctx context.Context, customerID, requestID uuid.UUID, in dtos.PayInput) (*dtos.PaymentView, bool, error) {
	method := entities.PaymentMethod(in.Method)
	if method.UsesGateway() && !u.Gateway.Enabled() {
		return nil, false, services.ErrPaymentsUnavailable
	}
	phone := ""
	if method == entities.PaymentGPO {
		var err error
		if phone, err = u.payerPhone(ctx, customerID, in.PhoneNumber); err != nil {
			return nil, false, err
		}
	}
	var payment *entities.Payment
	var request *entities.ServiceRequest
	created := false
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if request, err = u.Requests.FindForUpdate(ctx, requestID); err != nil {
			return err
		}
		if request == nil || !request.IsCustomer(customerID) {
			return ErrRequestNotFound
		}
		if request.AmountDueCents() <= 0 {
			return entities.ErrPaymentNotPayable
		}
		if request.PaymentStatus == entities.RequestPaid || request.PaymentStatus == entities.RequestFullyRefunded {
			return entities.ErrPaymentAlreadyPaid
		}
		if request.Prepay && request.Status == entities.RequestScheduled && method != entities.PaymentReference {
			return entities.ErrPaymentNotPayable
		}
		kind := request.DueKind()
		pending, err := u.Payments.PendingOfRequest(ctx, requestID, kind)
		if err != nil {
			return err
		}
		if pending != nil {
			if pending.Method != method {
				return entities.ErrPaymentPendingElsewhere
			}
			payment = pending
			return nil
		}
		enabled, err := u.Payments.IsMethodEnabled(ctx, request.CategoryID, method)
		if err != nil {
			return err
		}
		if !enabled {
			return entities.ErrPaymentMethodUnavailable
		}
		now := u.now()
		if payment, err = entities.NewPayment(entities.NewPaymentInput{Request: request, Method: method, Phone: phone, Now: now}); err != nil {
			return err
		}
		if method == entities.PaymentWallet {
			return u.payFromWallet(ctx, request, payment, now)
		}
		expires := now.Add(u.windowOf(method))
		payment.ExpiresAt = &expires
		payment.CheckAgainAt(now.Add(u.checkEveryOf(method)), now)
		created = true
		if err := u.Payments.Create(ctx, payment); err != nil {
			return err
		}
		if err := u.record(ctx, payment, sourceAPI, "created", now); err != nil {
			return err
		}
		if err := u.Payments.SetRequestPayment(ctx, requestID, method, entities.RequestPaymentWait); err != nil {
			return err
		}
		return u.publish(ctx, request, payment, method, string(payment.Status), now)
	})
	if err != nil {
		return nil, false, err
	}
	if !created {
		view := toView(payment)
		return &view, method == entities.PaymentWallet, nil
	}
	if err := u.charge(ctx, payment, phone); err != nil {
		return nil, false, err
	}
	fresh, err := u.Payments.Find(ctx, payment.ID)
	if err != nil {
		return nil, false, err
	}
	view := toView(fresh)
	return &view, true, nil
}

func (u *Usecase) windowOf(method entities.PaymentMethod) time.Duration {
	if method == entities.PaymentReference {
		return u.settings.ReferenceTTL
	}
	return u.settings.GPOWindow
}

func (u *Usecase) checkEveryOf(method entities.PaymentMethod) time.Duration {
	if method == entities.PaymentReference {
		return u.settings.ReferenceCheckInterval
	}
	return u.settings.GPOCheckInterval
}

func (u *Usecase) charge(ctx context.Context, payment *entities.Payment, phone string) error {
	charge, err := u.Gateway.CreateCharge(ctx, services.ChargeInput{
		Method: payment.Method, MerchantID: payment.MerchantID, AmountCents: payment.AmountCents, Currency: payment.Currency, Description: "Pedido AGORA " + payment.MerchantID, PhoneNumber: phone,
	})
	var declined *services.PaymentDeclinedError
	if errors.As(err, &declined) {
		return u.close(ctx, payment.ID, sourceAPI, declined.Code, func(p *entities.Payment, now time.Time) {
			p.MarkFailed(declined.Message, now)
		})
	}
	if err != nil {
		logger.FromContext(ctx).Warn("payment charge unconfirmed, the reconciler will look it up", zap.String("payment_id", payment.ID.String()), zap.Error(err))
		return nil
	}
	return u.attach(ctx, payment.ID, charge)
}

func (u *Usecase) attach(ctx context.Context, paymentID uuid.UUID, charge *services.Charge) error {
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		payment, err := u.Payments.FindForUpdate(ctx, paymentID)
		if err != nil {
			return err
		}
		if payment == nil {
			return ErrPaymentNotFound
		}
		now := u.now()
		chargeID := charge.ID
		payment.ProviderChargeID = &chargeID
		payment.ReferenceNumber, payment.ReferenceEntity = charge.ReferenceNumber, charge.ReferenceEntity
		if charge.DueAt != nil {
			payment.ExpiresAt = charge.DueAt
		}
		payment.UpdatedAt = now
		if err := u.Payments.Save(ctx, payment); err != nil {
			return err
		}
		return u.record(ctx, payment, sourceAPI, "charge "+string(charge.Status), now)
	})
	if err != nil {
		return err
	}
	switch charge.Status {
	case services.ChargeFailed:
		return u.close(ctx, paymentID, sourceAPI, "charge failed", func(p *entities.Payment, now time.Time) { p.MarkFailed(charge.Message, now) })
	case services.ChargeSuccess:
		return u.Reconcile(ctx, paymentID, sourceAPI)
	case services.ChargePending:
	}
	return nil
}

func (u *Usecase) close(ctx context.Context, paymentID uuid.UUID, source, detail string, change func(*entities.Payment, time.Time)) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		payment, err := u.Payments.FindForUpdate(ctx, paymentID)
		if err != nil {
			return err
		}
		if payment == nil {
			return ErrPaymentNotFound
		}
		if payment.Status != entities.PaymentPending {
			return nil
		}
		now := u.now()
		change(payment, now)
		return u.persistChange(ctx, payment, source, detail, now)
	})
}

func (u *Usecase) persistChange(ctx context.Context, payment *entities.Payment, source, detail string, now time.Time) error {
	request, err := u.Requests.FindForUpdate(ctx, payment.RequestID)
	if err != nil {
		return err
	}
	if request == nil {
		return ErrRequestNotFound
	}
	if err := u.Payments.Save(ctx, payment); err != nil {
		return err
	}
	if err := u.record(ctx, payment, source, detail, now); err != nil {
		return err
	}
	if payment.Status == entities.PaymentPaid {
		if err := u.postPayment(ctx, request, payment, now); err != nil {
			return err
		}
	}
	if payment.Kind != entities.PaymentKindTip {
		status := entities.RequestUnpaid
		if payment.Status == entities.PaymentPaid {
			status = entities.RequestPaid
		}
		if request.PaymentStatus == entities.RequestPaid || request.PaymentStatus == entities.RequestFullyRefunded {
			status = request.PaymentStatus
		}
		if err := u.Payments.SetRequestPayment(ctx, request.ID, payment.Method, status); err != nil {
			return err
		}
	}
	if err := u.notifyPrepaid(ctx, request, payment, now); err != nil {
		return err
	}
	return u.publish(ctx, request, payment, payment.Method, string(payment.Status), now)
}

func (u *Usecase) notifyPrepaid(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, now time.Time) error {
	if !request.Prepay || payment.Kind != entities.PaymentKindFare {
		return nil
	}
	if request.Status == entities.RequestCancelled && payment.Status == entities.PaymentPaid {
		return u.refundKeepingFee(ctx, request, payment, "Pagamento depois do cancelamento", now)
	}
	if u.Prepaid == nil || request.Status != entities.RequestScheduled {
		return nil
	}
	switch payment.Status {
	case entities.PaymentPaid:
		return u.Prepaid.OnPrepaid(ctx, request, now)
	case entities.PaymentExpired:
		return u.Prepaid.OnPrepayExpired(ctx, request, now)
	case entities.PaymentPending, entities.PaymentFailed:
	}
	return nil
}

func (u *Usecase) lookup(ctx context.Context, payment *entities.Payment) (*services.Charge, error) {
	if payment.ProviderChargeID != nil {
		return u.Gateway.GetCharge(ctx, *payment.ProviderChargeID)
	}
	return u.Gateway.FindChargeByMerchantID(ctx, payment.MerchantID)
}

func (u *Usecase) Reconcile(ctx context.Context, paymentID uuid.UUID, source string) error {
	payment, err := u.Payments.Find(ctx, paymentID)
	if err != nil {
		return err
	}
	if payment == nil {
		return ErrPaymentNotFound
	}
	charge, err := u.lookup(ctx, payment)
	if errors.Is(err, services.ErrChargeNotFound) {
		return u.apply(ctx, paymentID, nil, source)
	}
	if err != nil {
		if deferErr := u.deferCheck(ctx, payment); deferErr != nil {
			return errors.Join(err, deferErr)
		}
		return err
	}
	return u.apply(ctx, paymentID, charge, source)
}

func (u *Usecase) deferCheck(ctx context.Context, payment *entities.Payment) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		locked, err := u.Payments.FindForUpdate(ctx, payment.ID)
		if err != nil || locked == nil || locked.Status != entities.PaymentPending {
			return err
		}
		now := u.now()
		locked.CheckAgainAt(now.Add(u.checkEveryOf(locked.Method)), now)
		return u.Payments.Save(ctx, locked)
	})
}

func (u *Usecase) apply(ctx context.Context, paymentID uuid.UUID, charge *services.Charge, source string) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		payment, err := u.Payments.FindForUpdate(ctx, paymentID)
		if err != nil {
			return err
		}
		if payment == nil {
			return ErrPaymentNotFound
		}
		now := u.now()
		if charge == nil {
			return u.stillWaiting(ctx, payment, source, "charge not found", now)
		}
		if payment.ProviderChargeID == nil && charge.ID != "" {
			chargeID := charge.ID
			payment.ProviderChargeID = &chargeID
		}
		switch charge.Status {
		case services.ChargeSuccess:
			return u.applySuccess(ctx, payment, charge, source, now)
		case services.ChargeFailed:
			if payment.Status != entities.PaymentPending {
				return nil
			}
			payment.MarkFailed(charge.Message, now)
			return u.persistChange(ctx, payment, source, "charge failed", now)
		case services.ChargePending:
		}
		return u.stillWaiting(ctx, payment, source, "charge pending", now)
	})
}

func (u *Usecase) applySuccess(ctx context.Context, payment *entities.Payment, charge *services.Charge, source string, now time.Time) error {
	if payment.Status == entities.PaymentPaid {
		return nil
	}
	if charge.MerchantID != payment.MerchantID || charge.AmountCents != payment.AmountCents {
		logger.FromContext(ctx).Error("charge does not match the payment", zap.String("payment_id", payment.ID.String()), zap.Int64("charge_cents", charge.AmountCents), zap.Int64("payment_cents", payment.AmountCents))
		return u.stillWaiting(ctx, payment, source, "charge does not match", now)
	}
	if payment.Status != entities.PaymentPending {
		logger.FromContext(ctx).Warn("late payment after the deadline", zap.String("payment_id", payment.ID.String()))
	}
	other, err := u.Payments.PaidOfRequest(ctx, payment.RequestID, payment.Kind)
	if err != nil {
		return err
	}
	payment.MarkPaid(now)
	if other != nil {
		logger.FromContext(ctx).Warn("second paid payment for a request, it goes to the wallet", zap.String("payment_id", payment.ID.String()), zap.String("request_id", payment.RequestID.String()))
		if err := u.Payments.Save(ctx, payment); err != nil {
			return err
		}
		if err := u.record(ctx, payment, source, "charge confirmed twice, credited to the wallet", now); err != nil {
			return err
		}
		return u.creditDuplicate(ctx, payment, now)
	}
	return u.persistChange(ctx, payment, source, "charge confirmed", now)
}

func (u *Usecase) stillWaiting(ctx context.Context, payment *entities.Payment, source, detail string, now time.Time) error {
	if payment.Status != entities.PaymentPending {
		return nil
	}
	if payment.IsPastExpiry(now) {
		payment.MarkExpired(now)
		return u.persistChange(ctx, payment, source, detail, now)
	}
	payment.CheckAgainAt(now.Add(u.checkEveryOf(payment.Method)), now)
	return u.Payments.Save(ctx, payment)
}

func (u *Usecase) OnWebhook(ctx context.Context, chargeID, merchantID string) error {
	var payment *entities.Payment
	var err error
	if chargeID != "" {
		if payment, err = u.Payments.FindByProviderCharge(ctx, chargeID); err != nil {
			return err
		}
	}
	if payment == nil && merchantID != "" {
		if payment, err = u.Payments.FindByMerchantID(ctx, merchantID); err != nil {
			return err
		}
	}
	if payment == nil {
		logger.FromContext(ctx).Warn("webhook for an unknown payment", zap.String("charge_id", chargeID))
		return nil
	}
	return u.Reconcile(ctx, payment.ID, sourceWebhook)
}

func (u *Usecase) RunReconciler(ctx context.Context) {
	log := logger.FromContext(ctx)
	ticker := time.NewTicker(u.settings.ReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := u.ExpireUnpaidPrepaid(ctx); err != nil && ctx.Err() == nil {
				log.Warn("unpaid holds not expired", zap.Error(err))
			}
			if !u.Gateway.Enabled() {
				continue
			}
			if err := u.ReconcileDue(ctx); err != nil && ctx.Err() == nil {
				log.Warn("payment reconciliation failed", zap.Error(err))
			}
		}
	}
}

func (u *Usecase) ReconcileDue(ctx context.Context) error {
	ids, err := u.Payments.Due(ctx, u.now(), u.settings.ReconcileBatch)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)
	for _, id := range ids {
		if err := u.Reconcile(ctx, id, sourceReconcile); err != nil {
			log.Warn("payment not reconciled", zap.String("payment_id", id.String()), zap.Error(err))
		}
	}
	return nil
}

func (u *Usecase) ConfirmCash(ctx context.Context, providerID, requestID uuid.UUID) (*dtos.PaymentView, error) {
	var payment *entities.Payment
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsProvider(providerID) {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestCompleted {
			return entities.ErrPaymentNotPayable
		}
		if request.PaymentStatus == entities.RequestPaid {
			if payment, err = u.Payments.PaidOfRequest(ctx, requestID, entities.PaymentKindFare); err != nil {
				return err
			}
			if payment != nil && payment.Method == entities.PaymentCash {
				return nil
			}
			return entities.ErrPaymentAlreadyPaid
		}
		if request.PaymentStatus != entities.RequestUnpaid {
			return entities.ErrPaymentPendingElsewhere
		}
		if request.PaymentMethod != entities.PaymentCash {
			return entities.ErrCashNotExpected
		}
		enabled, err := u.Payments.IsMethodEnabled(ctx, request.CategoryID, entities.PaymentCash)
		if err != nil {
			return err
		}
		if !enabled {
			return entities.ErrPaymentMethodUnavailable
		}
		now := u.now()
		if payment, err = entities.NewPayment(entities.NewPaymentInput{Request: request, Method: entities.PaymentCash, Now: now}); err != nil {
			return err
		}
		payment.MarkPaid(now)
		if err := u.Payments.Create(ctx, payment); err != nil {
			return err
		}
		if err := u.record(ctx, payment, sourceProvider, "cash received", now); err != nil {
			return err
		}
		if err := u.Payments.SetRequestPayment(ctx, requestID, entities.PaymentCash, entities.RequestPaid); err != nil {
			return err
		}
		if err := u.postPayment(ctx, request, payment, now); err != nil {
			return err
		}
		return u.publish(ctx, request, payment, entities.PaymentCash, string(payment.Status), now)
	})
	if err != nil {
		return nil, err
	}
	view := toView(payment)
	return &view, nil
}

func (u *Usecase) ListOfRequest(ctx context.Context, userID, requestID uuid.UUID) (*dtos.PaymentList, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || (!request.IsCustomer(userID) && !request.IsProvider(userID)) {
		return nil, ErrRequestNotFound
	}
	payments, err := u.Payments.ListOfRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	list := &dtos.PaymentList{Items: make([]dtos.PaymentView, 0, len(payments))}
	for i := range payments {
		view := toView(&payments[i])
		if request.IsProvider(userID) {
			view.PayerPhone, view.Reference = "", nil
		}
		list.Items = append(list.Items, view)
	}
	return list, nil
}

func (u *Usecase) Refund(ctx context.Context, actor entities.Actor, paymentID uuid.UUID, in dtos.RefundInput) (*dtos.PaymentView, error) {
	return u.refund(ctx, actor, paymentID, in, false)
}

func (u *Usecase) refund(ctx context.Context, actor entities.Actor, paymentID uuid.UUID, in dtos.RefundInput, keepingFee bool) (*dtos.PaymentView, error) {
	var payment *entities.Payment
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if payment, err = u.Payments.FindForUpdate(ctx, paymentID); err != nil {
			return err
		}
		if payment == nil {
			return ErrPaymentNotFound
		}
		cents := in.AmountCents
		if cents == 0 {
			cents = payment.RefundableCents()
		}
		if err := payment.CheckRefund(cents); err != nil {
			return err
		}
		request, err := u.Requests.FindForUpdate(ctx, payment.RequestID)
		if err != nil {
			return err
		}
		if request == nil {
			return ErrRequestNotFound
		}
		providerRefundID := ""
		if !payment.RefundsToWallet() {
			var declined *services.PaymentDeclinedError
			providerRefundID, err = u.Gateway.Refund(ctx, *payment.ProviderChargeID, cents, "Reembolso "+payment.MerchantID)
			if errors.As(err, &declined) {
				return ErrRefundDeclined
			}
			if err != nil {
				return err
			}
		}
		now := u.now()
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		payment.AddRefund(cents, now)
		if err := u.Payments.Save(ctx, payment); err != nil {
			return err
		}
		if err := u.Payments.AddRefund(ctx, &repository.PaymentRefund{ID: id, PaymentID: payment.ID, AmountCents: cents, Reason: in.Reason, ProviderRefundID: providerRefundID, ActorID: actor.ID, CreatedAt: now}); err != nil {
			return err
		}
		if err := u.record(ctx, payment, sourceAdmin, "refund", now); err != nil {
			return err
		}
		if err := u.postRefund(ctx, request, payment, id, cents, keepingFee, now); err != nil {
			return err
		}
		status := request.PaymentStatus
		if status != entities.RequestUnpaid && (payment.Kind == entities.PaymentKindFare || payment.Kind == entities.PaymentKindCancellationFee) {
			status = entities.RequestPaid
			if payment.RefundableCents() == 0 {
				status = entities.RequestFullyRefunded
			}
			if err := u.Payments.SetRequestPayment(ctx, request.ID, payment.Method, status); err != nil {
				return err
			}
		}
		if err := u.publish(ctx, request, payment, payment.Method, string(status), now); err != nil {
			return err
		}
		entry, err := entities.NewAuditEntry(actor, "payment.refund", auditEntityPayment, payment.ID.String(), nil, map[string]any{"amount_cents": cents, "reason": in.Reason}, in.Reason, logger.CorrelationID(ctx))
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

func (u *Usecase) AdminList(ctx context.Context, filter dtos.AdminFilter, limit, offset int) ([]dtos.AdminPaymentView, int64, error) {
	rows, total, err := u.Payments.AdminList(ctx, repository.PaymentFilter{
		Statuses: filter.Statuses, Method: filter.Method, Category: filter.Category, RequestID: filter.RequestID, From: filter.From, To: filter.To, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]dtos.AdminPaymentView, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, dtos.AdminPaymentView{PaymentView: toView(&row.Payment), Category: row.Category, CustomerID: row.Payment.CustomerID, CustomerName: row.CustomerName, ProviderChargeID: row.Payment.ProviderChargeID})
	}
	return out, total, nil
}

func (u *Usecase) AdminGet(ctx context.Context, paymentID uuid.UUID) (*dtos.AdminPaymentDetail, error) {
	payment, err := u.Payments.Find(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if payment == nil {
		return nil, ErrPaymentNotFound
	}
	history, err := u.Payments.Events(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	refunds, err := u.Payments.Refunds(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	detail := &dtos.AdminPaymentDetail{Payment: toView(payment), ProviderChargeID: payment.ProviderChargeID, Events: make([]dtos.EventView, 0, len(history)), Refunds: make([]dtos.RefundView, 0, len(refunds))}
	for i := range history {
		event := &history[i]
		detail.Events = append(detail.Events, dtos.EventView{Source: event.Source, Status: event.Status, Detail: event.Detail, OccurredAt: event.OccurredAt})
	}
	for i := range refunds {
		refund := &refunds[i]
		detail.Refunds = append(detail.Refunds, dtos.RefundView{ID: refund.ID, AmountCents: refund.AmountCents, Reason: refund.Reason, ActorID: refund.ActorID, CreatedAt: refund.CreatedAt})
	}
	return detail, nil
}

// RefundPrepaid gives back what a customer paid ahead for a booking that was
// cancelled, keeping the cancellation fee if there is one. A reference goes to the
// wallet, because the gateway cannot refund it.
func (u *Usecase) RefundPrepaid(ctx context.Context, requestID uuid.UUID, reason string) error {
	paid, err := u.Payments.PaidOfRequest(ctx, requestID, entities.PaymentKindFare)
	if err != nil || paid == nil {
		return err
	}
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return err
	}
	if request == nil {
		return ErrRequestNotFound
	}
	return u.refundKeepingFee(ctx, request, paid, reason, u.now())
}

func (u *Usecase) refundKeepingFee(ctx context.Context, request *entities.ServiceRequest, payment *entities.Payment, reason string, now time.Time) error {
	kept := min(request.CancelFeeCents, payment.RefundableCents())
	if cents := payment.RefundableCents() - kept; cents > 0 {
		if _, err := u.refund(ctx, entities.SystemActor(), payment.ID, dtos.RefundInput{AmountCents: cents, Reason: reason}, true); err != nil {
			return err
		}
	}
	return u.creditKeptFee(ctx, request, payment, kept, now)
}

// ExpireUnpaidPrepaid ends a booking whose customer never asked for the reference,
// or whose attempt failed, so that an unpaid hold cannot keep a place for ever.
func (u *Usecase) ExpireUnpaidPrepaid(ctx context.Context) error {
	if u.Prepaid == nil {
		return nil
	}
	raw, err := u.Settings.Setting(ctx, entities.SettingPrepayHoldMinutes)
	if err != nil {
		return err
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil {
		return err
	}
	now := u.now()
	ids, err := u.Payments.UnpaidPrepaid(ctx, now.Add(-time.Duration(minutes)*time.Minute), u.settings.ReconcileBatch)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		failures = append(failures, u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
			request, err := u.Requests.FindForUpdate(ctx, id)
			if err != nil || request == nil || !request.Prepay || request.Status != entities.RequestScheduled || request.PaymentStatus != entities.RequestUnpaid {
				return err
			}
			pending, err := u.Payments.PendingOfRequest(ctx, id, entities.PaymentKindFare)
			if err != nil || pending != nil {
				return err
			}
			return u.Prepaid.OnPrepayExpired(ctx, request, now)
		}))
	}
	return errors.Join(failures...)
}
