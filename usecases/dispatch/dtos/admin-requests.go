package dtos

import (
	"time"

	"github.com/google/uuid"
)

type AdminRequestFilter struct {
	Statuses       []string
	Category       string
	ServiceID      *uuid.UUID
	ZoneID         *uuid.UUID
	OrganizationID *uuid.UUID
	ProviderID     *uuid.UUID
	From           *time.Time
	To             *time.Time
	RouteReview    bool
}

type AdminRequestView struct {
	ID               uuid.UUID  `json:"id"`
	Status           string     `json:"status"`
	Version          int        `json:"version"`
	Category         string     `json:"category"`
	CategoryName     string     `json:"category_name"`
	ServiceID        uuid.UUID  `json:"service_id"`
	ServiceName      string     `json:"service_name"`
	CustomerID       uuid.UUID  `json:"customer_id"`
	CustomerName     string     `json:"customer_name"`
	ProviderID       *uuid.UUID `json:"provider_id,omitempty"`
	ProviderName     string     `json:"provider_name,omitempty"`
	OrganizationID   *uuid.UUID `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name,omitempty"`
	ZoneID           uuid.UUID  `json:"zone_id"`
	ZoneName         string     `json:"zone_name"`
	FareCents        *int64     `json:"fare_cents,omitempty"`
	Currency         string     `json:"currency"`
	NeedsOperator    bool       `json:"needs_operator"`
	ScheduledFor     *time.Time `json:"scheduled_for,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	PickupLine       string     `json:"pickup_line,omitempty"`
	PickupLat        float64    `json:"pickup_lat"`
	PickupLng        float64    `json:"pickup_lng"`
	DestinationLine  string     `json:"destination_line,omitempty"`
	DeviationBps     *int       `json:"distance_deviation_bps,omitempty"`
	RouteReview      bool       `json:"route_review"`
}

type AdminRequestList struct {
	Items []AdminRequestView `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

type RequestSummaryGroup struct {
	Key       string           `json:"key" example:"towing"`
	Label     string           `json:"label" example:"Reboque"`
	Currency  string           `json:"currency" example:"AOA"`
	Total     int64            `json:"total"`
	ByStatus  map[string]int64 `json:"by_status"`
	FareCents int64            `json:"completed_fare_cents"`
}

type RequestSummary struct {
	GroupBy string                `json:"group_by" example:"category"`
	Total   int64                 `json:"total"`
	Groups  []RequestSummaryGroup `json:"groups"`
}

type RequestHistoryEntry struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Version    int        `json:"version"`
	ActorType  string     `json:"actor_type"`
	ActorID    *uuid.UUID `json:"actor_id,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	OccurredAt time.Time  `json:"occurred_at"`
}

type AdminRequestDetail struct {
	Request RequestView           `json:"request"`
	History []RequestHistoryEntry `json:"history"`
	Trip    *TripMetricsView      `json:"trip,omitempty"`
}

type TripMetricsView struct {
	EstimatedDistanceM   int       `json:"estimated_distance_m"`
	EstimatedDurationS   int       `json:"estimated_duration_s"`
	ActualDistanceM      int       `json:"actual_distance_m"`
	ActualDurationS      int       `json:"actual_duration_s"`
	DistanceDeviationBps int       `json:"distance_deviation_bps" example:"3500"`
	DurationDeviationBps int       `json:"duration_deviation_bps" example:"-1200"`
	Samples              int       `json:"gps_samples"`
	RouteChanged         bool      `json:"route_changed"`
	NeedsReview          bool      `json:"needs_review"`
	RecordedAt           time.Time `json:"recorded_at"`
}
