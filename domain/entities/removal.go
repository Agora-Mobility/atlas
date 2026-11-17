package entities

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	AttrMaxVolume     = "max_volume_dm3"
	AttrBaseMinutes   = "base_minutes"
	AttrMinutesPerM3  = "minutes_per_m3"
	AttrDailyCapacity = "daily_capacity"
	AttrStartMinute   = "start_minute"
	AttrEndMinute     = "end_minute"

	dm3PerM3         = 1000
	maxRemovalQty    = 500
	maxTeamMembers   = 20
	maxFloor         = 200
	TeamRoleLead     = "lead"
	TeamRoleHelper   = "helper"
	maxRemovalsNotes = 1000
)

var (
	ErrRemovalItemUnknown     = errors.New("an item does not exist in the catalog")
	ErrRemovalPropertyUnknown = errors.New("this type of property does not exist in the catalog")
	ErrRemovalExtraUnknown    = errors.New("an extra does not exist in the catalog")
	ErrRemovalHelpers         = errors.New("too many helpers")
	ErrRemovalTooLarge        = errors.New("no vehicle carries this volume")
	ErrRemovalVehicle         = errors.New("this vehicle does not exist in the catalog")
	ErrRemovalDetails         = errors.New("a removal needs the day, the window and the type of property")
	ErrRemovalWindow          = errors.New("the time does not match the window")
	ErrRemovalDayFull         = errors.New("this day is full")
	ErrRemovalTooFar          = errors.New("this day is too far ahead")
	ErrTeamNotAllowed         = errors.New("the team cannot be set on this request")
)

type RemovalLine struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Custom      bool   `json:"custom,omitempty"`
	Size        string `json:"size,omitempty"`
	Qty         int    `json:"qty,omitempty"`
	VolumeDm3   int64  `json:"volume_dm3,omitempty"`
	AmountCents int64  `json:"amount_cents,omitempty"`
}

type RemovalQuote struct {
	VehicleCode     string        `json:"vehicle_code"`
	Vehicle         string        `json:"vehicle"`
	VehicleCents    int64         `json:"vehicle_cents"`
	VolumeDm3       int64         `json:"volume_dm3"`
	VolumeM3        float64       `json:"volume_m3_calculated"`
	Items           []RemovalLine `json:"items"`
	Helpers         int           `json:"helpers"`
	HelpersCents    int64         `json:"helpers_cents"`
	Extras          []RemovalLine `json:"extras"`
	ExtrasCents     int64         `json:"extras_cents"`
	DurationMinutes int64         `json:"duration_minutes"`
	InsuranceCents  int64         `json:"insurance_cents"`
	Recommended     bool          `json:"recommended"`
}

func (q *RemovalQuote) TotalCents() int64 {
	return q.VehicleCents + q.HelpersCents + q.ExtrasCents
}

type RemovalItemChoice struct {
	Item *CatalogItem
	Qty  int
}

func RemovalVolume(choices []RemovalItemChoice) ([]RemovalLine, int64, error) {
	lines := make([]RemovalLine, 0, len(choices))
	total := int64(0)
	for _, choice := range choices {
		if choice.Item == nil || choice.Item.Kind != CatalogRemovalItem || !choice.Item.IsActive {
			return nil, 0, ErrRemovalItemUnknown
		}
		if choice.Qty < 1 || choice.Qty > maxRemovalQty {
			return nil, 0, validationError("removal.items.qty", "A quantidade tem de estar entre 1 e 500.")
		}
		volume := choice.Item.AttrInt(AttrVolumeDm3) * int64(choice.Qty)
		total += volume
		lines = append(lines, RemovalLine{Code: choice.Item.Code, Label: choice.Item.Label, Qty: choice.Qty, VolumeDm3: volume})
	}
	return lines, total, nil
}

const AttrVolumeDm3 = "volume_dm3"

const (
	CustomSizeSmall  = "small"
	CustomSizeMedium = "medium"
	CustomSizeLarge  = "large"
	customItemCode   = "custom"
	maxCustomItems   = 50
	maxCustomName    = 120
)

var CustomSizeSettings = []struct {
	Size    string
	Setting string
}{
	{CustomSizeSmall, SettingCustomSmallDm3},
	{CustomSizeMedium, SettingCustomMediumDm3},
	{CustomSizeLarge, SettingCustomLargeDm3},
}

type RemovalCustomItem struct {
	Name string
	Qty  int
	Size string
}

func RemovalCustomVolume(items []RemovalCustomItem, volumeOf map[string]int64) ([]RemovalLine, int64, error) {
	if len(items) > maxCustomItems {
		return nil, 0, validationError("removal.custom_items", "Podes juntar até 50 itens à mão.")
	}
	lines := make([]RemovalLine, 0, len(items))
	total := int64(0)
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" || len([]rune(name)) > maxCustomName {
			return nil, 0, validationError("removal.custom_items.name", "O nome é obrigatório e tem no máximo 120 caracteres.")
		}
		if item.Qty < 1 || item.Qty > maxRemovalQty {
			return nil, 0, validationError("removal.custom_items.qty", "A quantidade tem de estar entre 1 e 500.")
		}
		unit, known := volumeOf[item.Size]
		if !known {
			return nil, 0, validationError("removal.custom_items.size", "O tamanho é small, medium ou large.")
		}
		volume := unit * int64(item.Qty)
		total += volume
		lines = append(lines, RemovalLine{Code: customItemCode, Label: name, Custom: true, Size: item.Size, Qty: item.Qty, VolumeDm3: volume})
	}
	return lines, total, nil
}

// RecommendedVehicle is the smallest vehicle that carries the volume.
func RecommendedVehicle(vehicles []CatalogItem, volumeDm3 int64) *CatalogItem {
	var best *CatalogItem
	for i := range vehicles {
		capacity := vehicles[i].AttrInt(AttrMaxVolume)
		if capacity < volumeDm3 {
			continue
		}
		if best == nil || capacity < best.AttrInt(AttrMaxVolume) {
			best = &vehicles[i]
		}
	}
	return best
}

func RemovalDuration(vehicle *CatalogItem, volumeDm3 int64) int64 {
	return vehicle.AttrInt(AttrBaseMinutes) + vehicle.AttrInt(AttrMinutesPerM3)*volumeDm3/dm3PerM3
}

type RemovalPlaces struct {
	PickupFloor         int    `json:"pickup_floor"`
	PickupElevator      bool   `json:"pickup_elevator"`
	DestinationFloor    int    `json:"destination_floor"`
	DestinationElevator bool   `json:"destination_elevator"`
	ContactName         string `json:"contact_name,omitempty"`
	Notes               string `json:"notes,omitempty"`
}

func (p *RemovalPlaces) Normalize() error {
	if p.PickupFloor < -5 || p.PickupFloor > maxFloor || p.DestinationFloor < -5 || p.DestinationFloor > maxFloor {
		return validationError("removal.floor", "O andar não é válido.")
	}
	p.ContactName, p.Notes = strings.TrimSpace(p.ContactName), strings.TrimSpace(p.Notes)
	if len([]rune(p.ContactName)) > maxWorkshopTextLength || len([]rune(p.Notes)) > maxRemovalsNotes {
		return validationError("removal.notes", "O texto é demasiado longo.")
	}
	return nil
}

func WindowStart(window *CatalogItem, local time.Time) bool {
	minute := int64(local.Hour()*60 + local.Minute())
	return window != nil && window.Kind == CatalogRemovalWindow && window.IsActive && minute == window.AttrInt(AttrStartMinute) && local.Second() == 0
}

func WindowStartsAt(window *CatalogItem, day time.Time, location *time.Location) time.Time {
	minutes := time.Duration(window.AttrInt(AttrStartMinute)) * time.Minute
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location).Add(minutes)
}

type TeamMember struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	Name      string
	Role      string
	Verified  bool
	CreatedAt time.Time
}

func NewTeam(requestID uuid.UUID, members []TeamMember, now time.Time) ([]TeamMember, error) {
	if len(members) > maxTeamMembers {
		return nil, validationError("members", "A equipa tem no máximo 20 pessoas.")
	}
	out := make([]TeamMember, 0, len(members))
	for i := range members {
		member := members[i]
		member.Name = strings.TrimSpace(member.Name)
		if member.Name == "" || len([]rune(member.Name)) > maxWorkshopTextLength {
			return nil, validationError("members.name", "O nome é obrigatório e tem no máximo 120 caracteres.")
		}
		if !slices.Contains([]string{TeamRoleLead, TeamRoleHelper}, member.Role) {
			return nil, validationError("members.role", "O papel é lead ou helper.")
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		member.ID, member.RequestID, member.CreatedAt = id, requestID, now
		out = append(out, member)
	}
	return out, nil
}
