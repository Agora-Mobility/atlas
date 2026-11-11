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
	ServiceID      uuid.UUID            `json:"service_id" binding:"required"`
	Pickup         PlaceInput           `json:"pickup" binding:"required"`
	Destination    *PlaceInput          `json:"destination"`
	Stops          []PlaceInput         `json:"stops" binding:"max=20,dive"`
	ContactPhone   string               `json:"contact_phone" binding:"max=20"`
	Passenger      *PassengerInput      `json:"passenger"`
	Details        json.RawMessage      `json:"details" swaggertype:"object"`
	ScheduledFor   *time.Time           `json:"scheduled_for"`
	FareEstimateID *uuid.UUID           `json:"fare_estimate_id"`
	PaymentMethod  string               `json:"payment_method" binding:"omitempty,oneof=cash gpo reference wallet" example:"cash"`
	VoucherCode    string               `json:"voucher_code" binding:"max=30" example:"TAXI10"`
	Towing         *TowingInput         `json:"towing"`
	Removal        *RemovalBookingInput `json:"removal"`
}

type RemovalBookingInput struct {
	WindowID            uuid.UUID `json:"window_id" binding:"required"`
	PropertyCode        string    `json:"property_code" binding:"required,max=60" example:"home"`
	PickupFloor         int       `json:"pickup_floor" binding:"min=-5,max=200"`
	PickupElevator      bool      `json:"pickup_elevator"`
	DestinationFloor    int       `json:"destination_floor" binding:"min=-5,max=200"`
	DestinationElevator bool      `json:"destination_elevator"`
	ContactName         string    `json:"contact_name" binding:"max=120"`
	Notes               string    `json:"notes" binding:"max=1000"`
}

type RemovalOptions struct {
	Items          []RemovalOption `json:"items"`
	Vehicles       []RemovalOption `json:"vehicles"`
	Extras         []RemovalOption `json:"extras"`
	Windows        []RemovalOption `json:"windows"`
	Properties     []RemovalOption `json:"properties"`
	HelperCents    int64           `json:"helper_cents"`
	MaxHelpers     int             `json:"max_helpers"`
	InsuranceCents int64           `json:"insurance_cents"`
	Currency       string          `json:"currency"`
}

type RemovalOption struct {
	ID          uuid.UUID  `json:"id"`
	Code        string     `json:"code"`
	Label       string     `json:"label"`
	Group       string     `json:"group,omitempty"`
	AmountCents int64      `json:"amount_cents,omitempty"`
	PromoCents  *int64     `json:"promo_amount_cents,omitempty"`
	PromoUntil  *time.Time `json:"promo_until,omitempty"`
	VolumeDm3   int64      `json:"volume_dm3,omitempty"`
	MaxVolumeM3 float64    `json:"max_volume_m3,omitempty"`
}

type RemovalDayView struct {
	Date    string          `json:"date"`
	Status  string          `json:"status" enums:"free,full"`
	Windows []RemovalOption `json:"windows,omitempty"`
}

type RemovalCalendar struct {
	Days []RemovalDayView `json:"days"`
}

type TeamMemberInput struct {
	Name     string `json:"name" binding:"required,max=120" example:"Paulo Domingos"`
	Role     string `json:"role" binding:"required,oneof=lead helper" enums:"lead,helper"`
	Verified bool   `json:"verified"`
}

type TeamInput struct {
	Members []TeamMemberInput `json:"members" binding:"max=20,dive"`
}

type TeamMemberView struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Verified bool   `json:"verified"`
}

type TeamView struct {
	Items []TeamMemberView `json:"items"`
}

type PassengerInput struct {
	Name  string `json:"name" binding:"required,max=80"`
	Phone string `json:"phone" binding:"required,max=20"`
}

type TowingInput struct {
	IncidentID uuid.UUID `json:"incident_id" binding:"required"`
	Drivable   *bool     `json:"drivable" binding:"required"`
}

type CompleteInput struct {
	Code string `json:"code" binding:"max=10" example:"4821"`
}

type VoucherPreviewInput struct {
	Code           string    `json:"code" binding:"required,max=30" example:"TAXI10"`
	FareEstimateID uuid.UUID `json:"fare_estimate_id" binding:"required"`
}

type VoucherPreview struct {
	Code          string `json:"code"`
	FareCents     int64  `json:"fare_cents"`
	DiscountCents int64  `json:"discount_cents"`
	TotalCents    int64  `json:"total_cents"`
	Currency      string `json:"currency"`
}

type FareEstimateInput struct {
	Category     string        `json:"category" binding:"required" example:"taxi"`
	Pickup       PlaceInput    `json:"pickup" binding:"required"`
	Destination  *PlaceInput   `json:"destination"`
	Stops        []PlaceInput  `json:"stops" binding:"max=20,dive"`
	ScheduledFor *time.Time    `json:"scheduled_for"`
	Removal      *RemovalInput `json:"removal"`
}

type RemovalItemInput struct {
	ItemID uuid.UUID `json:"item_id" binding:"required"`
	Qty    int       `json:"qty" binding:"required,min=1,max=500" example:"2"`
}

type RemovalInput struct {
	Items   []RemovalItemInput `json:"items" binding:"max=200,dive"`
	Helpers int                `json:"helpers" binding:"min=0,max=50" example:"2"`
	Extras  []string           `json:"extras" binding:"max=20,dive,max=60" example:"packing"`
}

type FareEstimateView struct {
	FareEstimateID  uuid.UUID              `json:"fare_estimate_id"`
	ServiceID       uuid.UUID              `json:"service_id"`
	ServiceName     string                 `json:"service_name"`
	ServiceSlug     string                 `json:"service_slug"`
	TotalCents      int64                  `json:"total_cents"`
	Currency        string                 `json:"currency"`
	DistanceM       int                    `json:"distance_m"`
	DurationS       int                    `json:"duration_s"`
	RouteSource     string                 `json:"route_source"`
	ExpiresInS      int                    `json:"expires_in_s"`
	VehicleTypeID   *uuid.UUID             `json:"vehicle_type_id,omitempty"`
	VehicleType     string                 `json:"vehicle_type,omitempty"`
	IncludedKm      *int64                 `json:"included_km,omitempty"`
	Removal         *entities.RemovalQuote `json:"removal,omitempty"`
	DemandFactorBps int                    `json:"demand_factor_bps" example:"10000"`
	HighDemand      bool                   `json:"high_demand"`
}

type FareEstimateList struct {
	Items []FareEstimateView `json:"items"`
	// Route is the trip through the stops, the same for every quoted service. Absent without a destination;
	// without Google (source fallback) it has no geometry and the app keeps the straight line.
	Route *RouteView `json:"route,omitempty"`
}

type ProviderSummary struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	VehiclePlate       string    `json:"vehicle_plate,omitempty"`
	VehicleDescription string    `json:"vehicle_description,omitempty"`
	Rating             float64   `json:"rating"`
	RatingCount        int       `json:"rating_count"`
	CompletedRequests  int       `json:"completed_requests"`
}

type GivenRating struct {
	Stars         int         `json:"stars"`
	ComplimentIDs []uuid.UUID `json:"compliment_ids"`
	Comment       string      `json:"comment,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
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

type BreakdownItem struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	AmountCents int64  `json:"amount_cents"`
}

type FareBreakdown struct {
	Items          []BreakdownItem `json:"items"`
	FareCents      int64           `json:"fare_cents"`
	DiscountCents  int64           `json:"discount_cents"`
	VoucherCode    string          `json:"voucher_code,omitempty"`
	CancelFeeCents int64           `json:"cancel_fee_cents"`
	TipCents       int64           `json:"tip_cents"`
	TotalCents     int64           `json:"total_cents"`
	PaidCents      int64           `json:"paid_cents"`
	RefundedCents  int64           `json:"refunded_cents"`
	Currency       string          `json:"currency"`
	Method         string          `json:"method"`
}

type RequestView struct {
	Team            []TeamMemberView `json:"team,omitempty"`
	ID              uuid.UUID        `json:"id"`
	Code            string           `json:"code" example:"TX-48271"`
	ServiceName     string           `json:"service_name,omitempty"`
	Status          string           `json:"status"`
	Version         int              `json:"version"`
	Category        string           `json:"category"`
	ServiceID       uuid.UUID        `json:"service_id"`
	Pickup          PlaceView        `json:"pickup"`
	Destination     *PlaceView       `json:"destination,omitempty"`
	Stops           []PlaceView      `json:"stops"`
	RouteGeometry   string           `json:"route_geometry,omitempty" example:"zmrv@cbdpA..."`
	ContactPhone    string           `json:"contact_phone,omitempty"`
	PassengerName   string           `json:"passenger_name,omitempty"`
	Details         json.RawMessage  `json:"details,omitempty" swaggertype:"object"`
	ScheduledFor    *time.Time       `json:"scheduled_for,omitempty"`
	FareCents       *int64           `json:"fare_cents,omitempty"`
	ProviderNet     *int64           `json:"provider_net_cents,omitempty"`
	CommissionCents *int64           `json:"commission_cents,omitempty"`
	BoostBps        int              `json:"boost_bps,omitempty"`
	Currency        string           `json:"currency"`
	PaymentMethod   string           `json:"payment_method"`
	PaymentStatus   string           `json:"payment_status"`
	Breakdown       *FareBreakdown   `json:"breakdown,omitempty"`
	CanRate         bool             `json:"can_rate"`
	Rating          *GivenRating     `json:"rating,omitempty"`
	PassengerPhone  string           `json:"passenger_phone,omitempty"`
	DiscountCents   int64            `json:"discount_cents"`
	VoucherCode     string           `json:"voucher_code,omitempty"`
	AmountDueCents  int64            `json:"amount_due_cents"`
	CompletionCode  string           `json:"completion_code,omitempty"`
	CanBoost        bool             `json:"can_boost"`
	NeedsOperator   bool             `json:"needs_operator,omitempty"`
	OperatorReason  string           `json:"operator_reason,omitempty"`
	Provider        *ProviderSummary `json:"provider,omitempty"`
	Position        *PositionView    `json:"position,omitempty"`
	Quotes          []QuoteView      `json:"quotes,omitempty"`
	Adjustments     []AdjustmentView `json:"adjustments,omitempty"`
	CancelReason    string           `json:"cancel_reason,omitempty"`
	CancelReasonID  *uuid.UUID       `json:"cancel_reason_id,omitempty"`
	CancelFeeCents  int64            `json:"cancel_fee_cents"`
	CancelledBy     string           `json:"cancelled_by,omitempty"`
	AcceptedAt      *time.Time       `json:"accepted_at,omitempty"`
	ArrivedAt       *time.Time       `json:"arrived_at,omitempty"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
}

type PlaceView struct {
	AddressID uuid.UUID  `json:"address_id"`
	Lat       float64    `json:"lat"`
	Lng       float64    `json:"lng"`
	Reference string     `json:"reference,omitempty"`
	Address   string     `json:"address,omitempty"`
	ArrivedAt *time.Time `json:"arrived_at,omitempty"`
}

func ToPlaceView(p entities.RequestPlace) PlaceView {
	return PlaceView{AddressID: p.AddressID, Lat: p.Point.Lat, Lng: p.Point.Lng, Reference: p.Reference, Address: p.Address, ArrivedAt: p.ArrivedAt}
}

type SendMessageInput struct {
	Body         string     `json:"body" binding:"max=1000" example:"Estou a chegar"`
	QuickReplyID *uuid.UUID `json:"quick_reply_id"`
}

type MessageView struct {
	ID           uuid.UUID  `json:"id"`
	Sender       string     `json:"sender"`
	Body         string     `json:"body"`
	QuickReplyID *uuid.UUID `json:"quick_reply_id,omitempty"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type MessageList struct {
	Items []MessageView `json:"items"`
}

type QuickReplyView struct {
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
}

type QuickReplyList struct {
	Items []QuickReplyView `json:"items"`
}

type ShareView struct {
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PublicTrip struct {
	Code               string        `json:"code"`
	Status             string        `json:"status"`
	Category           string        `json:"category"`
	ProviderName       string        `json:"provider_name,omitempty"`
	VehiclePlate       string        `json:"vehicle_plate,omitempty"`
	VehicleDescription string        `json:"vehicle_description,omitempty"`
	PickupAddress      string        `json:"pickup_address,omitempty"`
	DestinationAddress string        `json:"destination_address,omitempty"`
	Position           *PositionView `json:"position,omitempty"`
	ETASeconds         *int          `json:"eta_s,omitempty"`
	ExpiresAt          time.Time     `json:"expires_at"`
}

type ETAView struct {
	DurationS int           `json:"duration_s"`
	DistanceM int           `json:"distance_m"`
	Source    string        `json:"source"`
	Target    string        `json:"target"`
	Position  *PositionView `json:"position,omitempty"`
}

type CallView struct {
	ProxyNumber string     `json:"proxy_number"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type SOSInput struct {
	Lat  *float64 `json:"lat" binding:"omitempty,min=-90,max=90"`
	Lng  *float64 `json:"lng" binding:"omitempty,min=-180,max=180"`
	Note string   `json:"note" binding:"max=500"`
}

type SOSView struct {
	ID         uuid.UUID  `json:"id"`
	RequestID  uuid.UUID  `json:"request_id"`
	UserID     uuid.UUID  `json:"user_id"`
	Lat        *float64   `json:"lat,omitempty"`
	Lng        *float64   `json:"lng,omitempty"`
	Note       string     `json:"note,omitempty"`
	Status     string     `json:"status"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type SOSList struct {
	Items []SOSView `json:"items"`
	Total int64     `json:"total"`
	Page  int       `json:"page"`
	Size  int       `json:"size"`
}

type RecentDestination struct {
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Address   string    `json:"address,omitempty"`
	Reference string    `json:"reference,omitempty"`
	UsedAt    time.Time `json:"used_at"`
}

type RecentDestinationList struct {
	Items []RecentDestination `json:"items"`
}

type RepeatInput struct {
	Preview       bool       `json:"preview"`
	PaymentMethod string     `json:"payment_method" binding:"omitempty,oneof=cash gpo reference wallet"`
	ScheduledFor  *time.Time `json:"scheduled_for"`
}

type RepeatView struct {
	ServiceID    uuid.UUID          `json:"service_id"`
	Category     string             `json:"category"`
	Pickup       PlaceInput         `json:"pickup"`
	Destination  *PlaceInput        `json:"destination,omitempty"`
	Stops        []PlaceInput       `json:"stops"`
	ScheduledFor *time.Time         `json:"scheduled_for,omitempty"`
	Estimates    []FareEstimateView `json:"estimates"`
	Request      *RequestView       `json:"request,omitempty"`
}

type RequestList struct {
	Items []RequestView `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

type OfferView struct {
	ID            uuid.UUID       `json:"id"`
	RequestID     uuid.UUID       `json:"request_id"`
	Version       int             `json:"request_version"`
	Status        string          `json:"status"`
	Category      string          `json:"category"`
	ExpiresInS    int             `json:"expires_in_s"`
	Pickup        PlaceView       `json:"pickup"`
	Destination   *PlaceView      `json:"destination,omitempty"`
	Stops         []PlaceView     `json:"stops,omitempty"`
	DistanceM     *int            `json:"distance_m,omitempty"`
	NetCents      *int64          `json:"net_cents,omitempty"`
	Currency      string          `json:"currency"`
	Mode          string          `json:"mode"`
	PaymentMethod string          `json:"payment_method"`
	Details       json.RawMessage `json:"details,omitempty" swaggertype:"object"`
}

type ActiveState struct {
	Request  *RequestView  `json:"request"`
	Requests []RequestView `json:"requests"`
	Offers   []OfferView   `json:"offers"`
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
	Geometry  string `json:"geometry,omitempty" example:"zmrv@cbdpA..."` // Google encoded polyline, precision 5
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
	ReasonID         uuid.UUID `json:"reason_id" binding:"required"`
	Note             string    `json:"note" binding:"max=400" example:"Tive um imprevisto"`
	ExpectedFeeCents *int64    `json:"expected_fee_cents" binding:"omitempty,min=0,max=10000000000"`
}

type RejectOfferInput struct {
	ReasonID uuid.UUID `json:"reason_id" binding:"required"`
	Note     string    `json:"note" binding:"max=400"`
}

type OperatorCancelInput struct {
	Reason string `json:"reason" binding:"required,max=200"`
}

type ReasonOption struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Label        string    `json:"label"`
	RequiresNote bool      `json:"requires_note"`
}

type ReasonOptions struct {
	Items []ReasonOption `json:"items"`
}

type CancellationPreview struct {
	FeeCents  int64          `json:"fee_cents"`
	Currency  string         `json:"currency"`
	FreeUntil *time.Time     `json:"free_until,omitempty"`
	Reasons   []ReasonOption `json:"reasons"`
}

type CancellationRuleInput struct {
	FeeCents               int64 `json:"fee_cents" binding:"min=0,max=10000000000"`
	FreeAfterAssignSeconds int   `json:"free_after_assign_seconds" binding:"min=0,max=604800"`
	ScheduledFreeSeconds   int   `json:"scheduled_free_seconds" binding:"min=0,max=2592000"`
	ChargeWithoutProvider  bool  `json:"charge_without_provider"`
}

type CancellationRuleView struct {
	ServiceID   uuid.UUID `json:"service_id"`
	ServiceSlug string    `json:"service_slug,omitempty"`
	ServiceName string    `json:"service_name,omitempty"`
	CancellationRuleInput
}

type CancellationRuleList struct {
	Items []CancellationRuleView `json:"items"`
}

type CancellationReportRow struct {
	ReasonID uuid.UUID `json:"reason_id"`
	Code     string    `json:"code"`
	Label    string    `json:"label"`
	Category string    `json:"category"`
	Total    int64     `json:"total"`
	FeeCents int64     `json:"fee_cents"`
}

type CancellationReport struct {
	Scope string                  `json:"scope"`
	Items []CancellationReportRow `json:"items"`
}

type ChangeRouteInput struct {
	Destination PlaceInput   `json:"destination" binding:"required"`
	Stops       []PlaceInput `json:"stops" binding:"max=20,dive"`
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
	OfferTTLSeconds   int        `json:"offer_ttl_seconds" binding:"required,min=1,max=2592000"`
	SearchTTLSeconds  int        `json:"search_ttl_seconds" binding:"required,min=1,max=2592000"`
	AckTimeoutSeconds int        `json:"ack_timeout_seconds" binding:"required,min=1,max=86400"`
	AckGiveUpSeconds  int        `json:"ack_giveup_seconds" binding:"min=0,max=86400"`
	InitialChannels   []string   `json:"initial_channels" binding:"required,min=1"`
	FallbackChannels  []string   `json:"fallback_channels"`
	Ranking           string     `json:"ranking" binding:"required,oneof=distance_idle distance base_distance customer_choice"`
	TrackingStatuses  []string   `json:"tracking_statuses"`
	OnExhausted       string     `json:"on_exhausted" binding:"required,oneof=suggest_retry alert_operator operator_queue extend_or_queue"`
	ReserveProvider   bool       `json:"reserve_provider"`
	ScheduleMaxDays   int        `json:"schedule_max_days" binding:"min=0,max=365"`
	SearchLeadSeconds int        `json:"search_lead_seconds" binding:"min=0,max=2592000"`
	PricingMode       string     `json:"pricing_mode" binding:"required,oneof=fixed call_out quote"`
	AllowBoost        bool       `json:"allow_boost"`
	RankByETA         bool       `json:"rank_by_eta"`
	SMSOnAccept       bool       `json:"sms_on_accept"`
	MaxStops          *int       `json:"max_stops" binding:"omitempty,min=0,max=20"`
	ScheduleSlotMin   *int       `json:"schedule_slot_minutes" binding:"omitempty,min=1,max=60"`
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
	StopFeeCents      int64      `json:"stop_fee_cents" binding:"min=0"`
	NightStartHour    int        `json:"night_start_hour" binding:"min=0,max=23"`
	NightEndHour      int        `json:"night_end_hour" binding:"min=0,max=23"`
	FreeWaitSeconds   int        `json:"free_wait_seconds" binding:"min=0,max=86400"`
	CommissionBps     int        `json:"commission_bps" binding:"min=0,max=10000"`
}

type DemandTierInput struct {
	AboveRatioBps int `json:"above_ratio_bps" binding:"min=0" example:"15000"`
	FactorBps     int `json:"factor_bps" binding:"min=10000,max=30000" example:"12000"`
}

type DemandPricingInput struct {
	ZoneID            uuid.UUID         `json:"zone_id" binding:"required"`
	Category          string            `json:"category" binding:"required" example:"taxi"`
	Mode              string            `json:"mode" binding:"required,oneof=off observe on" example:"observe"`
	Tiers             []DemandTierInput `json:"tiers" binding:"max=10,dive"`
	MaxFactorBps      int               `json:"max_factor_bps" binding:"min=10000,max=30000" example:"13000"`
	CommissionOnSurge bool              `json:"commission_on_surge"`
}

type DemandPricingView struct {
	ID         uuid.UUID `json:"id"`
	CategoryID uuid.UUID `json:"category_id"`
	DemandPricingInput
	UpdatedAt time.Time `json:"updated_at"`
}

type DemandPricingList struct {
	Items []DemandPricingView `json:"items"`
}

type DemandAreaView struct {
	Cell      string  `json:"cell"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	RatioBps  int     `json:"ratio_bps" example:"17000"`
	FactorBps int     `json:"factor_bps" example:"12000"`
}

type DemandAreaList struct {
	Resolution int              `json:"resolution"`
	Items      []DemandAreaView `json:"items"`
}

type DemandSnapshotView struct {
	DemandAreaView
	Pending    int       `json:"pending"`
	Free       int       `json:"free"`
	RecordedAt time.Time `json:"recorded_at"`
}

type DemandHistory struct {
	Resolution int                  `json:"resolution"`
	Items      []DemandSnapshotView `json:"items"`
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
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	Cells      []HeatCell `json:"cells"`
}

type DemandQuery struct {
	Position *entities.GeoPoint
}

type DemandCell struct {
	Cell     string       `json:"cell" example:"88754e64d3fffff"`
	Level    int          `json:"level" example:"3" enums:"1,2,3"`
	Boundary [][2]float64 `json:"boundary" swaggertype:"array,array,number"`
}

type DemandView struct {
	Category      string       `json:"category" example:"taxi"`
	WindowMinutes int          `json:"window_minutes" example:"60"`
	RadiusKm      float64      `json:"radius_km" example:"10"`
	ExpiresAt     time.Time    `json:"expires_at"`
	Cells         []DemandCell `json:"cells"`
}

type HeatQuery struct {
	From     *time.Time
	To       *time.Time
	ZoneID   *uuid.UUID
	Category string
}

type FlowQuery struct {
	HeatQuery
	MinRequests int
	Limit       int
}

type HeatPoint struct {
	Cell string  `json:"cell"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

type CellFlow struct {
	Origin      HeatPoint `json:"origin"`
	Destination HeatPoint `json:"destination"`
	Requests    int64     `json:"requests"`
}

type FlowMap struct {
	Resolution  int        `json:"resolution"`
	From        time.Time  `json:"from"`
	To          time.Time  `json:"to"`
	MinRequests int        `json:"min_requests"`
	Total       int64      `json:"total"`
	Hidden      int64      `json:"hidden"`
	Flows       []CellFlow `json:"flows"`
}
