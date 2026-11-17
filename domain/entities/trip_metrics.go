package entities

import (
	"time"

	"github.com/google/uuid"
)

const minTrackedCoverageBps = 6_000

type TripMetrics struct {
	RequestID            uuid.UUID
	EstimatedDistanceM   int
	EstimatedDurationS   int
	ActualDistanceM      int
	ActualDurationS      int
	DistanceDeviationBps int
	DurationDeviationBps int
	Samples              int
	RouteChanged         bool
	NeedsReview          bool
	RecordedAt           time.Time
}

type TripMetricsInput struct {
	RequestID          uuid.UUID
	EstimatedDistanceM int
	EstimatedDurationS int
	ActualDistanceM    int
	ActualDuration     time.Duration
	Samples            int
	SampleEvery        time.Duration
	RouteChanged       bool
	ReviewAboveBps     int
	RecordedAt         time.Time
}

func NewTripMetrics(in TripMetricsInput) TripMetrics {
	actualSeconds := int(in.ActualDuration.Seconds())
	metrics := TripMetrics{
		RequestID: in.RequestID, EstimatedDistanceM: in.EstimatedDistanceM, EstimatedDurationS: in.EstimatedDurationS,
		ActualDistanceM: in.ActualDistanceM, ActualDurationS: actualSeconds, Samples: in.Samples, RouteChanged: in.RouteChanged, RecordedAt: in.RecordedAt,
		DistanceDeviationBps: deviationBps(in.EstimatedDistanceM, in.ActualDistanceM),
		DurationDeviationBps: deviationBps(in.EstimatedDurationS, actualSeconds),
	}
	metrics.NeedsReview = in.reviewable() && absInt(metrics.DistanceDeviationBps) > in.ReviewAboveBps &&
		(metrics.DistanceDeviationBps > 0 || in.wellTracked())
	return metrics
}

func (in TripMetricsInput) reviewable() bool {
	return !in.RouteChanged && in.ReviewAboveBps > 0 && in.EstimatedDistanceM > 0
}

func (in TripMetricsInput) wellTracked() bool {
	if in.SampleEvery <= 0 || in.ActualDuration <= 0 {
		return false
	}
	covered := time.Duration(in.Samples) * in.SampleEvery
	return covered*bpsDenominator >= in.ActualDuration*minTrackedCoverageBps
}

func deviationBps(estimated, actual int) int {
	if estimated <= 0 {
		return 0
	}
	return int(roundedDivide(int64(actual-estimated)*bpsDenominator, int64(estimated)))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
