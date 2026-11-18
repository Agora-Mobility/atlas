package dispatch

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

var ErrInvalidGrouping = errors.New("group_by must be category, service, status, zone or organization")

const noPartnerLabel = "AGORA"

type summaryKey func(row *repository.RequestSummaryRow) (key, label string)

var summaryKeys = map[string]summaryKey{
	"category": func(row *repository.RequestSummaryRow) (string, string) { return row.Category, row.CategoryName },
	"service": func(row *repository.RequestSummaryRow) (string, string) {
		return row.ServiceID.String(), row.ServiceName
	},
	"status": func(row *repository.RequestSummaryRow) (string, string) { return row.Status, row.Status },
	"zone":   func(row *repository.RequestSummaryRow) (string, string) { return row.ZoneID.String(), row.ZoneName },
	"organization": func(row *repository.RequestSummaryRow) (string, string) {
		if row.OrganizationID == nil {
			return "", noPartnerLabel
		}
		return row.OrganizationID.String(), row.OrganizationName
	},
}

func toRepositoryFilter(filter *dtos.AdminRequestFilter) repository.AdminRequestFilter {
	return repository.AdminRequestFilter{
		Statuses: filter.Statuses, Category: filter.Category, ServiceID: filter.ServiceID, ZoneID: filter.ZoneID, OrganizationID: filter.OrganizationID,
		ProviderID: filter.ProviderID, CustomerID: filter.CustomerID, From: filter.From, To: filter.To, RouteReview: filter.RouteReview, Search: filter.Search, Sort: filter.Sort,
	}
}

func (u *Usecase) ListAdminRequests(ctx context.Context, filter *dtos.AdminRequestFilter, page pagination.Params) (*dtos.AdminRequestList, error) {
	page = page.Normalize()
	query := toRepositoryFilter(filter)
	query.Limit, query.Offset = int(page.Limit()), int(page.Offset())
	rows, total, err := u.Requests.AdminList(ctx, query)
	if err != nil {
		return nil, err
	}
	list := &dtos.AdminRequestList{Items: make([]dtos.AdminRequestView, 0, len(rows)), Total: total, Page: page.Page, Size: page.Size}
	for i := range rows {
		row := &rows[i]
		list.Items = append(list.Items, dtos.AdminRequestView{
			ID: row.ID, Code: row.Code, Status: row.Status, Version: row.Version, Category: row.Category, CategoryName: row.CategoryName, ServiceID: row.ServiceID, ServiceName: row.ServiceName,
			CustomerID: row.CustomerID, CustomerName: row.CustomerName, ProviderID: row.ProviderID, ProviderName: row.ProviderName, OrganizationID: row.OrganizationID,
			OrganizationName: row.OrganizationName, ZoneID: row.ZoneID, ZoneName: row.ZoneName, FareCents: row.FareCents, Currency: row.Currency, NeedsOperator: row.NeedsOperator,
			ScheduledFor: row.ScheduledFor, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt, PickupLine: row.PickupLine, PickupLat: row.PickupLat, PickupLng: row.PickupLng,
			DestinationLine: row.DestinationLine, DeviationBps: row.DeviationBps, RouteReview: row.RouteReview,
		})
	}
	return list, nil
}

func (u *Usecase) SummarizeRequests(ctx context.Context, groupBy string, filter *dtos.AdminRequestFilter) (*dtos.RequestSummary, error) {
	if groupBy == "" {
		groupBy = "category"
	}
	keyOf, known := summaryKeys[groupBy]
	if !known {
		return nil, ErrInvalidGrouping
	}
	rows, err := u.Requests.Summary(ctx, toRepositoryFilter(filter))
	if err != nil {
		return nil, err
	}
	type groupID struct{ key, currency string }
	groups := map[groupID]*dtos.RequestSummaryGroup{}
	summary := &dtos.RequestSummary{GroupBy: groupBy, Groups: []dtos.RequestSummaryGroup{}}
	for i := range rows {
		row := &rows[i]
		key, label := keyOf(row)
		id := groupID{key: key, currency: row.Currency}
		group, seen := groups[id]
		if !seen {
			group = &dtos.RequestSummaryGroup{Key: key, Label: label, Currency: row.Currency, ByStatus: map[string]int64{}}
			groups[id] = group
		}
		group.Total += row.Total
		group.ByStatus[row.Status] += row.Total
		group.FareCents += row.FareCents
		summary.Total += row.Total
	}
	for _, group := range groups {
		summary.Groups = append(summary.Groups, *group)
	}
	sort.Slice(summary.Groups, func(a, b int) bool {
		if summary.Groups[a].Total != summary.Groups[b].Total {
			return summary.Groups[a].Total > summary.Groups[b].Total
		}
		return summary.Groups[a].Label < summary.Groups[b].Label
	})
	return summary, nil
}

func (u *Usecase) GetAdminRequest(ctx context.Context, requestID uuid.UUID) (*dtos.AdminRequestDetail, error) {
	view, err := u.GetRequestAsOperator(ctx, requestID)
	if err != nil {
		return nil, err
	}
	entries, err := u.Requests.History(ctx, requestID)
	if err != nil {
		return nil, err
	}
	detail := &dtos.AdminRequestDetail{Request: *view, History: make([]dtos.RequestHistoryEntry, 0, len(entries))}
	metrics, err := u.Pricing.FindTripMetrics(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if metrics != nil {
		detail.Trip = &dtos.TripMetricsView{
			EstimatedDistanceM: metrics.EstimatedDistanceM, EstimatedDurationS: metrics.EstimatedDurationS, ActualDistanceM: metrics.ActualDistanceM,
			ActualDurationS: metrics.ActualDurationS, DistanceDeviationBps: metrics.DistanceDeviationBps, DurationDeviationBps: metrics.DurationDeviationBps,
			Samples: metrics.Samples, RouteChanged: metrics.RouteChanged, NeedsReview: metrics.NeedsReview, RecordedAt: metrics.RecordedAt,
		}
	}
	for i := range entries {
		entry := &entries[i]
		detail.History = append(detail.History, dtos.RequestHistoryEntry{
			From: entry.From, To: entry.To, Version: entry.Version, ActorType: entry.ActorType, ActorID: entry.ActorID, Reason: entry.Reason, OccurredAt: entry.OccurredAt,
		})
	}
	return detail, nil
}

func (u *Usecase) GetPartnerRequest(ctx context.Context, organizationID, requestID uuid.UUID) (*dtos.AdminRequestDetail, error) {
	owner, err := u.Requests.OrganizationOf(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if owner == nil || *owner != organizationID {
		return nil, ErrRequestNotFound
	}
	detail, err := u.GetAdminRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	detail.Request.ContactPhone = ""
	return detail, nil
}
