package entities

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

type CandidateSource string

const (
	CandidateLive     CandidateSource = "live"
	CandidateCoverage CandidateSource = "coverage"
)

type OfferMode string

const (
	OfferModeSequential OfferMode = "sequential"
	OfferModeParallel   OfferMode = "parallel"
	OfferModeQuote      OfferMode = "quote"
)

type Ranking string

const (
	RankingDistanceIdle   Ranking = "distance_idle"
	RankingDistance       Ranking = "distance"
	RankingBaseDistance   Ranking = "base_distance"
	RankingCustomerChoice Ranking = "customer_choice"
)

type ExhaustedAction string

const (
	ExhaustedSuggestRetry  ExhaustedAction = "suggest_retry"
	ExhaustedAlertOperator ExhaustedAction = "alert_operator"
	ExhaustedOperatorQueue ExhaustedAction = "operator_queue"
	ExhaustedExtendOrQueue ExhaustedAction = "extend_or_queue"
)

type PricingMode string

const (
	PricingFixed   PricingMode = "fixed"
	PricingCallOut PricingMode = "call_out"
	PricingQuote   PricingMode = "quote"
)

const (
	ChannelSSE = "sse"
	ChannelFCM = "fcm"
	ChannelSMS = "sms"

	maxSearchCells  = 400
	maxH3Resolution = 15
	maxPolicyStops  = 20

	DefaultMaxStops            = 3
	DefaultScheduleSlotMinutes = 15
	maxSlotMinutes             = 60
)

var (
	ErrPolicyNotFound      = errors.New("dispatch policy not found")
	ErrScheduleNotAllowed  = errors.New("this service cannot be scheduled")
	ErrPolicySearchTooWide = errors.New("the radius needs too many H3 cells at this resolution")
)

type DispatchPolicy struct {
	ID                uuid.UUID
	CategoryID        uuid.UUID
	ZoneID            *uuid.UUID
	CandidateSource   CandidateSource
	H3Resolution      int
	RadiiKm           []float64
	OfferMode         OfferMode
	BatchSize         int
	OfferTTLSeconds   int
	SearchTTLSeconds  int
	AckTimeoutSeconds int
	AckGiveUpSeconds  int
	InitialChannels   []string
	FallbackChannels  []string
	Ranking           Ranking
	TrackingStatuses  []RequestStatus
	OnExhausted       ExhaustedAction
	ReserveProvider   bool
	ScheduleMaxDays   int
	SearchLeadSeconds int
	PricingMode       PricingMode
	AllowBoost        bool
	RankByETA         bool
	SMSOnAccept       bool
	MaxStops          int
	ScheduleSlotMin   int
	RequiresCode      bool
}

func (p *DispatchPolicy) Validate(cellCount func(radiusKm float64, resolution int) int) error {
	if p.H3Resolution < 0 || p.H3Resolution > maxH3Resolution {
		return validationError("h3_resolution", "A resolução H3 não é válida.")
	}
	if len(p.RadiiKm) == 0 {
		return validationError("radii_km", "Indica pelo menos um raio.")
	}
	previous := 0.0
	for _, radius := range p.RadiiKm {
		if radius <= previous {
			return validationError("radii_km", "Os raios têm de ser positivos e crescentes.")
		}
		if p.CandidateSource == CandidateLive && cellCount(radius, p.H3Resolution) > maxSearchCells {
			return customerValidation("radii_km", ErrPolicySearchTooWide)
		}
		previous = radius
	}
	if p.BatchSize <= 0 || p.OfferTTLSeconds <= 0 || p.SearchTTLSeconds < p.OfferTTLSeconds || p.AckTimeoutSeconds <= 0 || p.AckGiveUpSeconds < 0 {
		return validationError("offer_ttl_seconds", "Os prazos da política não são válidos.")
	}
	if p.OfferMode == OfferModeSequential && p.BatchSize != 1 {
		return validationError("batch_size", "O modo sequencial oferece a um prestador de cada vez.")
	}
	if p.CandidateSource == CandidateCoverage && p.H3Resolution != 6 {
		return validationError("h3_resolution", "A cobertura usa sempre a resolução 6.")
	}
	if p.OfferMode == OfferModeQuote && p.PricingMode != PricingQuote {
		return validationError("pricing_mode", "O modo de orçamento exige precificação por orçamento.")
	}
	if p.MaxStops < 0 || p.MaxStops > maxPolicyStops {
		return validationError("max_stops", "O limite de paragens tem de estar entre 0 e 20.")
	}
	if p.ScheduleSlotMin < 1 || p.ScheduleSlotMin > maxSlotMinutes {
		return validationError("schedule_slot_minutes", "O intervalo de marcação tem de estar entre 1 e 60 minutos.")
	}
	for _, channel := range slices.Concat(p.InitialChannels, p.FallbackChannels) {
		if channel != ChannelSSE && channel != ChannelFCM && channel != ChannelSMS {
			return validationError("channels", "Canal desconhecido: "+channel)
		}
	}
	return nil
}

func (p *DispatchPolicy) ValidateSchedule(at, now time.Time) error {
	if p.ScheduleMaxDays == 0 {
		return ErrScheduleNotAllowed
	}
	if !at.After(now) {
		return validationError("scheduled_for", "A data tem de estar no futuro.")
	}
	if at.After(now.AddDate(0, 0, p.ScheduleMaxDays)) {
		return validationError("scheduled_for", "A data está para lá do permitido para este serviço.")
	}
	if p.ScheduleSlotMin > 0 && at.Unix()%int64(p.ScheduleSlotMin*secondsPerMinute) != 0 {
		return validationError("scheduled_for", fmt.Sprintf("Escolhe uma hora em blocos de %d minutos.", p.ScheduleSlotMin))
	}
	return nil
}

func (p *DispatchPolicy) ValidateStops(count int) error {
	if count > p.MaxStops {
		return validationError("stops", fmt.Sprintf("Só podes adicionar até %d paragens.", p.MaxStops))
	}
	return nil
}

func (p *DispatchPolicy) Tracks(status RequestStatus) bool {
	return slices.Contains(p.TrackingStatuses, status)
}

func (p *DispatchPolicy) RadiusAt(round int) float64 {
	if round >= len(p.RadiiKm) {
		return p.RadiiKm[len(p.RadiiKm)-1]
	}
	return p.RadiiKm[round]
}

func (p *DispatchPolicy) IsLastRound(round int) bool {
	return round >= len(p.RadiiKm)-1
}

func (p *DispatchPolicy) HasChannelAtStart(channel string) bool {
	return slices.Contains(p.InitialChannels, channel)
}

type ServiceZone struct {
	ID          uuid.UUID
	CountryCode string
	Name        string
	GeoJSON     string
	Timezone    string
	Currency    string
	IsActive    bool
	Polygons    []GeoPolygon
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (z *ServiceZone) Location() *time.Location {
	location, err := time.LoadLocation(z.Timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

type DispatchService struct {
	ID                  uuid.UUID
	Name                string
	Slug                string
	CategoryID          uuid.UUID
	CategorySlug        string
	RequiresDestination bool
	IsOffered           bool
}
