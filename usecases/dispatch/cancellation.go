package dispatch

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

type CancelFeeChangedError struct {
	FeeCents int64
	Currency string
}

func (e *CancelFeeChangedError) Error() string {
	return "the cancellation fee changed"
}

type cancelOrder struct {
	actor       entities.RequestActor
	allowed     func(*entities.ServiceRequest) bool
	text        string
	reasonID    *uuid.UUID
	scope       entities.RejectionScope
	note        string
	expectedFee *int64
}

func (u *Usecase) CancelByCustomer(ctx context.Context, customerID, requestID uuid.UUID, in dtos.CancelInput) (*dtos.RequestView, error) {
	request, err := u.cancel(ctx, requestID, cancelOrder{
		actor: entities.RequestActor{Type: entities.RequestActorCustomer, ID: &customerID}, reasonID: &in.ReasonID, scope: entities.RejectionScopeCustomerCancel, note: in.Note, expectedFee: in.ExpectedFeeCents,
		allowed: func(r *entities.ServiceRequest) bool { return r.IsCustomer(customerID) },
	})
	if err != nil {
		return nil, err
	}
	return u.customerView(ctx, request)
}

func (u *Usecase) CancelByProvider(ctx context.Context, providerID, requestID uuid.UUID, in dtos.CancelInput) (*dtos.RequestView, error) {
	request, err := u.cancel(ctx, requestID, cancelOrder{
		actor: entities.RequestActor{Type: entities.RequestActorProvider, ID: &providerID}, reasonID: &in.ReasonID, scope: entities.RejectionScopeProviderCancel, note: in.Note,
		allowed: func(r *entities.ServiceRequest) bool { return r.IsProvider(providerID) },
	})
	if err != nil {
		return nil, err
	}
	return u.providerView(ctx, request)
}

func (u *Usecase) cancelFee(ctx context.Context, request *entities.ServiceRequest, now time.Time) (int64, error) {
	rule, err := u.Cancels.Rule(ctx, request.ServiceID)
	if err != nil || rule == nil {
		return 0, err
	}
	return rule.CostFor(request, now).FeeCents, nil
}

func (u *Usecase) resolveReason(ctx context.Context, id uuid.UUID, scope entities.RejectionScope, categoryID uuid.UUID, note string) (string, error) {
	reason, err := u.Cancels.ActiveReason(ctx, id, scope)
	if err != nil {
		return "", err
	}
	if reason == nil || !reason.AppliesTo(categoryID) {
		return "", ErrReasonNotApplicable
	}
	return entities.ComposeRejection([]entities.RejectionReason{*reason}, note)
}

func (u *Usecase) reasonNote(ctx context.Context, id uuid.UUID, scope entities.RejectionScope, categoryID uuid.UUID, note string) (string, error) {
	if _, err := u.resolveReason(ctx, id, scope, categoryID, note); err != nil {
		return "", err
	}
	return strings.TrimSpace(note), nil
}

func toReasonOptions(reasons []entities.RejectionReason) []dtos.ReasonOption {
	out := make([]dtos.ReasonOption, 0, len(reasons))
	for i := range reasons {
		out = append(out, dtos.ReasonOption{ID: reasons[i].ID, Code: reasons[i].Code, Label: reasons[i].Label, RequiresNote: reasons[i].RequiresNote})
	}
	return out
}

func (u *Usecase) ReasonOptions(ctx context.Context, actor, categorySlug string) (*dtos.ReasonOptions, error) {
	scope, known := entities.ReasonScopeFor(actor)
	if !known {
		return nil, ErrUnknownReasonActor
	}
	services, err := u.Config.ListServicesOfCategory(ctx, categorySlug)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, ErrServiceNotOffered
	}
	reasons, err := u.Cancels.ReasonsFor(ctx, scope, services[0].CategoryID)
	if err != nil {
		return nil, err
	}
	return &dtos.ReasonOptions{Items: toReasonOptions(reasons)}, nil
}

func (u *Usecase) PreviewCancellation(ctx context.Context, userID, requestID uuid.UUID) (*dtos.CancellationPreview, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || (!request.IsCustomer(userID) && !request.IsProvider(userID)) {
		return nil, ErrRequestNotFound
	}
	scope := entities.RejectionScopeProviderCancel
	view := &dtos.CancellationPreview{Currency: request.Fare.Currency}
	if request.IsCustomer(userID) {
		scope = entities.RejectionScopeCustomerCancel
		rule, err := u.Cancels.Rule(ctx, request.ServiceID)
		if err != nil {
			return nil, err
		}
		if rule != nil {
			cost := rule.CostFor(request, u.now())
			view.FeeCents, view.FreeUntil = cost.FeeCents, cost.FreeUntil
		}
	}
	reasons, err := u.Cancels.ReasonsFor(ctx, scope, request.CategoryID)
	if err != nil {
		return nil, err
	}
	view.Reasons = toReasonOptions(reasons)
	return view, nil
}

func toRuleView(rule *entities.CancellationRule) dtos.CancellationRuleView {
	return dtos.CancellationRuleView{ServiceID: rule.ServiceID, ServiceSlug: rule.ServiceSlug, ServiceName: rule.ServiceName, CancellationRuleInput: dtos.CancellationRuleInput{
		FeeCents: rule.FeeCents, FreeAfterAssignSeconds: rule.FreeAfterAssignSeconds, ScheduledFreeSeconds: rule.ScheduledFreeSeconds, ChargeWithoutProvider: rule.ChargeWithoutProvider,
	}}
}

func (u *Usecase) ListCancellationRules(ctx context.Context) (*dtos.CancellationRuleList, error) {
	rules, err := u.Cancels.Rules(ctx)
	if err != nil {
		return nil, err
	}
	list := &dtos.CancellationRuleList{Items: make([]dtos.CancellationRuleView, 0, len(rules))}
	for i := range rules {
		list.Items = append(list.Items, toRuleView(&rules[i]))
	}
	return list, nil
}

func (u *Usecase) SetCancellationRule(ctx context.Context, actor entities.Actor, serviceID uuid.UUID, in dtos.CancellationRuleInput) (*dtos.CancellationRuleView, error) {
	service, err := u.Config.FindService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, ErrServiceNotOffered
	}
	rule := &entities.CancellationRule{ServiceID: serviceID, FeeCents: in.FeeCents, FreeAfterAssignSeconds: in.FreeAfterAssignSeconds, ScheduledFreeSeconds: in.ScheduledFreeSeconds, ChargeWithoutProvider: in.ChargeWithoutProvider}
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	view := toRuleView(rule)
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Cancels.SaveRule(ctx, rule); err != nil {
			return err
		}
		return u.audit(ctx, actor, "cancellation_rule.set", "service", serviceID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (u *Usecase) CancellationReport(ctx context.Context, scope, category string, from, to *time.Time) (*dtos.CancellationReport, error) {
	reasonScope := entities.RejectionScope(scope)
	if reasonScope != entities.RejectionScopeCustomerCancel && reasonScope != entities.RejectionScopeProviderCancel && reasonScope != entities.RejectionScopeOfferReject {
		return nil, ErrReportScope
	}
	rows, err := u.Cancels.Report(ctx, repository.CancellationReportFilter{Scope: reasonScope, Category: category, From: from, To: to})
	if err != nil {
		return nil, err
	}
	view := &dtos.CancellationReport{Scope: scope, Items: make([]dtos.CancellationReportRow, 0, len(rows))}
	for i := range rows {
		row := &rows[i]
		view.Items = append(view.Items, dtos.CancellationReportRow{ReasonID: row.ReasonID, Code: row.Code, Label: row.Label, Category: row.Category, Total: row.Total, FeeCents: row.FeeCents})
	}
	return view, nil
}
