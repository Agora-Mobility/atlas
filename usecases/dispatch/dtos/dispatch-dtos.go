package dtos

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type PlaceInput struct {
	Lat       float64 `json:"lat" binding:"required" example:"-8.84"`
	Lng       float64 `json:"lng" binding:"required" example:"13.23"`
	Reference string  `json:"reference" binding:"max=140" example:"Junto à farmácia, portão azul"`
	Address   string  `json:"address" binding:"max=200"`
}

func (p PlaceInput) ToPlace() entities.RequestPlace {
	return entities.RequestPlace{Point: entities.GeoPoint{Lat: p.Lat, Lng: p.Lng}, Reference: p.Reference, Address: p.Address}
}

type CreateRequestInput struct {
	ServiceID      uuid.UUID       `json:"service_id" binding:"required"`
	Pickup         PlaceInput      `json:"pickup" binding:"required"`
	Destination    *PlaceInput     `json:"destination"`
	Stops          []PlaceInput    `json:"stops" binding:"max=3,dive"`
	ContactPhone   string          `json:"contact_phone" binding:"max=20"`
	Passenger      *PassengerInput `json:"passenger"`
	Details        json.RawMessage `json:"details" swaggertype:"object"`
	ScheduledFor   *time.Time      `json:"scheduled_for"`
	FareEstimateID *uuid.UUID      `json:"fare_estimate_id"`
}

type PassengerInput struct {
	Name  string `json:"name" binding:"required,max=80"`
	Phone string `json:"phone" binding:"required,max=20"`
}

type FareEstimateInput struct {
	Category    string       `json:"category" binding:"required" example:"taxi"`
	Pickup      PlaceInput   `json:"pickup" binding:"required"`
	Destination *PlaceInput  `json:"destination"`
	Stops       []PlaceInput `json:"stops" binding:"max=3,dive"`
}

type FareEstimateView struct {
	FareEstimateID uuid.UUID `json:"fare_estimate_id"`
	ServiceID      uuid.UUID `json:"service_id"`
	ServiceName    string    `json:"service_name"`
	ServiceSlug    string    `json:"service_slug"`
	TotalCents     int64     `json:"total_cents"`
	Currency       string    `json:"currency"`
	DistanceM      int       `json:"distance_m"`
	DurationS      int       `json:"duration_s"`
	RouteSource    string    `json:"route_source"`
	ExpiresInS     int       `json:"expires_in_s"`
}

type FareEstimateList struct {
	Items []FareEstimateView `json:"items"`
}

type ProviderSummary struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	VehiclePlate       string    `json:"vehicle_plate,omitempty"`
	VehicleDescription string    `json:"vehicle_description,omitempty"`
}

type PositionView struct {
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	AgeS int     `json:"age_s"`
}

type QuoteView struct {
	OfferID     uuid.UUID        `json:"offer_id"`
	Provider    *ProviderSummary `json:"provider,omitempty"`
	AmountCents int64            `json:"amount_cents"`
	Note        string           `json:"note,omitempty"`
	ValidForS   int              `json:"valid_for_s"`
	DistanceM   *int             `json:"distance_m,omitempty"`
}

type AdjustmentView struct {
	Reason      string    `json:"reason"`
	AmountCents int64     `json:"amount_cents"`
	CreatedAt   time.Time `json:"created_at"`
}

type RequestView struct {
	ID              uuid.UUID        `json:"id"`
	Status          string           `json:"status"`
	Version         int              `json:"version"`
	Category        string           `json:"category"`
	ServiceID       uuid.UUID        `json:"service_id"`
	Pickup          PlaceView        `json:"pickup"`
	Destination     *PlaceView       `json:"destination,omitempty"`
	Stops           []PlaceView      `json:"stops"`
	ContactPhone    string           `json:"contact_phone,omitempty"`
	PassengerName   string           `json:"passenger_name,omitempty"`
	Details         json.RawMessage  `json:"details,omitempty" swaggertype:"object"`
	ScheduledFor    *time.Time       `json:"scheduled_for,omitempty"`
	FareCents       *int64           `json:"fare_cents,omitempty"`
	ProviderNet     *int64           `json:"provider_net_cents,omitempty"`
	CommissionCents *int64           `json:"commission_cents,omitempty"`
	BoostBps        int              `json:"boost_bps,omitempty"`
	Currency        string           `json:"currency"`
	CanBoost        bool             `json:"can_boost"`
	NeedsOperator   bool             `json:"needs_operator,omitempty"`
	OperatorReason  string           `json:"operator_reason,omitempty"`
	Provider        *ProviderSummary `json:"provider,omitempty"`
	Position        *PositionView    `json:"position,omitempty"`
	Quotes          []QuoteView      `json:"quotes,omitempty"`
	Adjustments     []AdjustmentView `json:"adjustments,omitempty"`
	CancelReason    string           `json:"cancel_reason,omitempty"`
	CancelledBy     string           `json:"cancelled_by,omitempty"`
	AcceptedAt      *time.Time       `json:"accepted_at,omitempty"`
	ArrivedAt       *time.Time       `json:"arrived_at,omitempty"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
}

type PlaceView struct {
	AddressID uuid.UUID `json:"address_id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Reference string    `json:"reference,omitempty"`
	Address   string    `json:"address,omitempty"`
}

func ToPlaceView(p entities.RequestPlace) PlaceView {
	return PlaceView{AddressID: p.AddressID, Lat: p.Point.Lat, Lng: p.Point.Lng, Reference: p.Reference, Address: p.Address}
}

type RequestList struct {
	Items []RequestView `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

type OfferView struct {
	ID          uuid.UUID       `json:"id"`
	RequestID   uuid.UUID       `json:"request_id"`
	Version     int             `json:"request_version"`
	Status      string          `json:"status"`
	Category    string          `json:"category"`
	ExpiresInS  int             `json:"expires_in_s"`
	Pickup      PlaceView       `json:"pickup"`
	Destination *PlaceView      `json:"destination,omitempty"`
	Stops       []PlaceView     `json:"stops,omitempty"`
	DistanceM   *int            `json:"distance_m,omitempty"`
	NetCents    *int64          `json:"net_cents,omitempty"`
	Currency    string          `json:"currency"`
	Mode        string          `json:"mode"`
	Details     json.RawMessage `json:"details,omitempty" swaggertype:"object"`
}

type ActiveState struct {
	Request *RequestView `json:"request"`
	Offers  []OfferView  `json:"offers"`
}

type DeliveredInput struct {
	Channel string `json:"channel" binding:"required,oneof=sse fcm sms"`
}

type AcceptInput struct {
	Version int `json:"version" binding:"required,min=1"`
}

type AcceptResult struct {
	Request RequestView `json:"request"`
	Route   *RouteView  `json:"route,omitempty"`
}

type RouteView struct {
	DistanceM int    `json:"distance_m"`
	DurationS int    `json:"duration_s"`
	Geometry  string `json:"geometry,omitempty"`
	Source    string `json:"source"`
}

type QuoteInput struct {
	AmountCents int64  `json:"amount_cents" binding:"required,min=1"`
	Note        string `json:"note" binding:"max=300"`
}

type SelectQuoteInput struct {
	OfferID uuid.UUID `json:"offer_id" binding:"required"`
	Version int       `json:"version" binding:"required,min=1"`
}

type BoostInput struct {
	Percent int `json:"percent" binding:"required,oneof=10 20"`
}

type CancelInput struct {
	Reason string `json:"reason" binding:"required,max=200"`
}

type ChangeRouteInput struct {
	Destination PlaceInput   `json:"destination" binding:"required"`
	Stops       []PlaceInput `json:"stops" binding:"max=3,dive"`
}

type OnlineInput struct {
	Position *PositionInput `json:"position"`
}

type PositionInput struct {
	Lat       float64  `json:"lat" binding:"required"`
	Lng       float64  `json:"lng" binding:"required"`
	Heading   *float64 `json:"heading"`
	AccuracyM float64  `json:"accuracy_m" binding:"min=0"`
	AgeS      int      `json:"age_s" binding:"min=0,max=86400"`
}

type LocationsInput struct {
	Positions   []PositionInput `json:"positions" binding:"required,min=1,max=100,dive"`
	BatteryLow  bool            `json:"battery_low"`
	NetworkWeak bool            `json:"network_weak"`
}

type CadenceView struct {
	Online    bool   `json:"online"`
	Phase     string `json:"phase"`
	SendGPS   bool   `json:"send_gps"`
	IntervalS int    `json:"interval_s"`
	DistanceM int    `json:"distance_m"`
	Accepted  int    `json:"accepted"`
	Rejected  int    `json:"rejected"`
}

type CoverageInput struct {
	Lat      float64 `json:"lat" binding:"required"`
	Lng      float64 `json:"lng" binding:"required"`
	RadiusKm float64 `json:"radius_km" binding:"min=0,max=300"`
}

type CoverageView struct {
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	RadiusKm   float64 `json:"radius_km"`
	Categories int     `json:"categories"`
	Cells      int     `json:"cells"`
}

type NearbyCell struct {
	Cell  string  `json:"cell"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Count int64   `json:"count"`
}

type NearbyView struct {
	Available bool         `json:"available"`
	Cells     []NearbyCell `json:"cells"`
}

type DeviceInput struct {
	Token    string `json:"token" binding:"required,max=4096"`
	Platform string `json:"platform" binding:"required,oneof=android ios"`
}

type ZoneInput struct {
	CountryCode string          `json:"country_code" binding:"required,len=2"`
	Name        string          `json:"name" binding:"required,max=80"`
	Timezone    string          `json:"timezone" binding:"required,max=64"`
	Currency    string          `json:"currency" binding:"required,len=3"`
	IsActive    *bool           `json:"is_active"`
	Geometry    json.RawMessage `json:"geometry" binding:"required" swaggertype:"object"`
}

type ZoneView struct {
	ID          uuid.UUID       `json:"id"`
	CountryCode string          `json:"country_code"`
	Name        string          `json:"name"`
	Timezone    string          `json:"timezone"`
	Currency    string          `json:"currency"`
	IsActive    bool            `json:"is_active"`
	Geometry    json.RawMessage `json:"geometry" swaggertype:"object"`
}

type ZoneList struct {
	Items []ZoneView `json:"items"`
}

type PolicyInput struct {
	CategoryID        uuid.UUID  `json:"category_id"`
	ZoneID            *uuid.UUID `json:"zone_id"`
	CandidateSource   string     `json:"candidate_source" binding:"required,oneof=live coverage"`
	H3Resolution      int        `json:"h3_resolution" binding:"min=0,max=15"`
	RadiiKm           []float64  `json:"radii_km" binding:"required,min=1,max=6"`
	OfferMode         string     `json:"offer_mode" binding:"required,oneof=sequential parallel quote"`
	BatchSize         int        `json:"batch_size" binding:"required,min=1,max=50"`
	OfferTTLSeconds   int        `json:"offer_ttl_seconds" binding:"required,min=1"`
	SearchTTLSeconds  int        `json:"search_ttl_seconds" binding:"required,min=1"`
	AckTimeoutSeconds int        `json:"ack_timeout_seconds" binding:"required,min=1"`
	AckGiveUpSeconds  int        `json:"ack_giveup_seconds" binding:"min=0"`
	InitialChannels   []string   `json:"initial_channels" binding:"required,min=1"`
	FallbackChannels  []string   `json:"fallback_channels"`
	Ranking           string     `json:"ranking" binding:"required,oneof=distance_idle distance base_distance customer_choice"`
	TrackingStatuses  []string   `json:"tracking_statuses"`
	OnExhausted       string     `json:"on_exhausted" binding:"required,oneof=suggest_retry alert_operator operator_queue extend_or_queue"`
	ReserveProvider   bool       `json:"reserve_provider"`
	ScheduleMaxDays   int        `json:"schedule_max_days" binding:"min=0,max=365"`
	SearchLeadSeconds int        `json:"search_lead_seconds" binding:"min=0"`
	PricingMode       string     `json:"pricing_mode" binding:"required,oneof=fixed call_out quote"`
	AllowBoost        bool       `json:"allow_boost"`
	RankByETA         bool       `json:"rank_by_eta"`
	SMSOnAccept       bool       `json:"sms_on_accept"`
}

type PolicyList struct {
	Items []PolicyView `json:"items"`
}

type PolicyView struct {
	ID uuid.UUID `json:"id"`
	PolicyInput
}

type TariffInput struct {
	ServiceID         uuid.UUID  `json:"service_id" binding:"required"`
	ZoneID            *uuid.UUID `json:"zone_id"`
	Currency          string     `json:"currency" binding:"required,len=3"`
	BaseFeeCents      int64      `json:"base_fee_cents" binding:"min=0"`
	PerKmFeeCents     int64      `json:"per_km_fee_cents" binding:"min=0"`
	PerMinuteFeeCents int64      `json:"per_minute_fee_cents" binding:"min=0"`
	MinimumFeeCents   int64      `json:"minimum_fee_cents" binding:"min=0"`
	ZoneFeeCents      int64      `json:"zone_fee_cents" binding:"min=0"`
	NightFeeCents     int64      `json:"night_fee_cents" binding:"min=0"`
	NightStartHour    int        `json:"night_start_hour" binding:"min=0,max=23"`
	NightEndHour      int        `json:"night_end_hour" binding:"min=0,max=23"`
	FreeWaitSeconds   int        `json:"free_wait_seconds" binding:"min=0"`
	CommissionBps     int        `json:"commission_bps" binding:"min=0,max=10000"`
}

type TariffView struct {
	ID uuid.UUID `json:"id"`
	TariffInput
	ValidFrom time.Time `json:"valid_from"`
}

type TariffList struct {
	Items []TariffView `json:"items"`
}

type AssignInput struct {
	ProviderID uuid.UUID `json:"provider_id" binding:"required"`
}

type HeatCell struct {
	Cell   string  `json:"cell"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Supply int64   `json:"supply"`
	Demand int64   `json:"demand"`
}

type HeatMap struct {
	Resolution int        `json:"resolution"`
	Cells      []HeatCell `json:"cells"`
}
