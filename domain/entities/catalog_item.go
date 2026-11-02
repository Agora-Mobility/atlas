package entities

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CatalogKind string

const (
	CatalogCompliment      CatalogKind = "compliment"
	CatalogTipPreset       CatalogKind = "tip_preset"
	CatalogQuickReply      CatalogKind = "quick_reply"
	CatalogTowingVehicle   CatalogKind = "towing_vehicle"
	CatalogTowingIncident  CatalogKind = "towing_incident"
	CatalogRemovalItem     CatalogKind = "removal_item"
	CatalogRemovalVehicle  CatalogKind = "removal_vehicle"
	CatalogRemovalExtra    CatalogKind = "removal_extra"
	CatalogRemovalWindow   CatalogKind = "removal_window"
	CatalogRemovalProperty CatalogKind = "removal_property"
	CatalogHelpTopic       CatalogKind = "help_topic"
	CatalogWorkshopCat     CatalogKind = "workshop_category"

	maxCatalogLabel       = 200
	maxCatalogDescription = 4000
	maxCatalogAttrsBytes  = 4096
)

var ErrCatalogItemNotFound = errors.New("catalog item not found")

func IsCatalogKind(kind string) bool {
	switch CatalogKind(kind) {
	case CatalogCompliment, CatalogTipPreset, CatalogQuickReply, CatalogTowingVehicle, CatalogTowingIncident, CatalogRemovalItem, CatalogRemovalVehicle,
		CatalogRemovalExtra, CatalogRemovalWindow, CatalogRemovalProperty, CatalogHelpTopic, CatalogWorkshopCat:
		return true
	}
	return false
}

type CatalogItem struct {
	ID           uuid.UUID
	Kind         CatalogKind
	CategoryID   *uuid.UUID
	CategorySlug string
	Code         string
	Label        string
	Description  string
	AmountCents  int64
	PromoCents   *int64
	PromoUntil   *time.Time
	Attrs        json.RawMessage
	DisplayOrder int
	IsActive     bool
}

func (i *CatalogItem) Normalize() error {
	if !IsCatalogKind(string(i.Kind)) {
		return validationError("kind", "O tipo não é válido.")
	}
	i.Code = strings.TrimSpace(i.Code)
	i.Label = strings.TrimSpace(i.Label)
	if !reasonCodePattern.MatchString(i.Code) {
		return validationError("code", "O código usa letras minúsculas, números e _.")
	}
	if i.Label == "" || len([]rune(i.Label)) > maxCatalogLabel {
		return validationError("label", "O texto é obrigatório e tem no máximo 200 caracteres.")
	}
	if len([]rune(i.Description)) > maxCatalogDescription {
		return validationError("description", "A descrição tem no máximo 4000 caracteres.")
	}
	if i.AmountCents < 0 || i.AmountCents > MaxFeeCents {
		return validationError("amount_cents", "O valor não é válido.")
	}
	if i.PromoCents != nil && (*i.PromoCents < 0 || *i.PromoCents > i.AmountCents) {
		return validationError("promo_amount_cents", "O preço de promoção não pode passar do preço normal.")
	}
	if i.DisplayOrder < 0 || i.DisplayOrder > maxReasonOrder {
		return validationError("display_order", "A ordem tem de estar entre 0 e 9999.")
	}
	if len(i.Attrs) == 0 {
		i.Attrs = json.RawMessage("{}")
	}
	if len(i.Attrs) > maxCatalogAttrsBytes || !json.Valid(i.Attrs) {
		return validationError("attrs", "Os atributos não são válidos.")
	}
	return i.validateAttrs()
}

func (i *CatalogItem) validateAttrs() error {
	limits := map[CatalogKind]map[string][2]int64{
		CatalogTowingVehicle:  {"included_km": {0, 10_000}, "extra_km_cents": {0, MaxFeeCents}, "min_capacity_kg": {0, MaxVehicleCapacityKg}},
		CatalogRemovalItem:    {"volume_dm3": {1, 100_000}},
		CatalogRemovalVehicle: {"max_volume_dm3": {1, 1_000_000}, "base_minutes": {1, 10_080}, "minutes_per_m3": {0, 1_000}, "daily_capacity": {0, 10_000}},
		CatalogRemovalWindow:  {"start_minute": {0, 1439}, "end_minute": {1, 1440}},
	}
	wanted, known := limits[i.Kind]
	if !known {
		return nil
	}
	var values map[string]json.Number
	if err := json.Unmarshal(i.Attrs, &values); err != nil {
		return validationError("attrs", "Os atributos têm de ser números.")
	}
	for name, limit := range wanted {
		raw, present := values[name]
		if !present {
			return validationError("attrs."+name, "Este atributo é obrigatório.")
		}
		value, err := raw.Int64()
		if err != nil || value < limit[0] || value > limit[1] {
			return validationError("attrs."+name, "O valor tem de estar entre "+strconv.FormatInt(limit[0], 10)+" e "+strconv.FormatInt(limit[1], 10)+".")
		}
	}
	return nil
}

func (i *CatalogItem) AttrInt(name string) int64 {
	var values map[string]json.Number
	if err := json.Unmarshal(i.Attrs, &values); err != nil {
		return 0
	}
	value, err := values[name].Int64()
	if err != nil {
		return 0
	}
	return value
}

func (i *CatalogItem) PriceAt(now time.Time) int64 {
	if i.PromoCents != nil && i.PromoUntil != nil && now.Before(*i.PromoUntil) {
		return *i.PromoCents
	}
	return i.AmountCents
}

type SettingKind int

const (
	SettingCents SettingKind = iota
	SettingCount
	SettingText
)

type settingRule struct {
	kind     SettingKind
	min, max int64
}

const (
	SettingMaxTipCents          = "max_tip_cents"
	SettingMaxPhotos            = "max_request_photos"
	SettingPhotoMaxBytes        = "photo_max_bytes"
	SettingPhotoTypes           = "photo_content_types"
	SettingReferralRewardCents  = "referral_reward_cents"
	SettingSupportPhone         = "support_phone"
	SettingHelpersPriceCents    = "removal_helper_cents"
	SettingInsuranceCents       = "removal_insurance_cents"
	SettingMaxHelpers           = "removal_max_helpers"
	SettingShareTTLMinutes      = "share_ttl_minutes"
	SettingBookingMaxDays       = "booking_max_days"
	SettingBookingSlotMinutes   = "booking_slot_minutes"
	SettingRemovalMaxDays       = "removal_max_days"
	SettingMaxPhotosPerBooking  = "max_booking_photos"
	SettingTowingCompletionCode = "towing_code_digits"
)

var settingRules = map[string]settingRule{
	SettingMaxTipCents:          {SettingCents, 0, MaxFeeCents},
	SettingMaxPhotos:            {SettingCount, 0, 50},
	SettingMaxPhotosPerBooking:  {SettingCount, 0, 50},
	SettingPhotoMaxBytes:        {SettingCount, 1, 50_000_000},
	SettingPhotoTypes:           {SettingText, 0, 200},
	SettingReferralRewardCents:  {SettingCents, 0, MaxFeeCents},
	SettingSupportPhone:         {SettingText, 0, 32},
	SettingHelpersPriceCents:    {SettingCents, 0, MaxFeeCents},
	SettingInsuranceCents:       {SettingCents, 0, MaxFeeCents},
	SettingMaxHelpers:           {SettingCount, 0, 50},
	SettingShareTTLMinutes:      {SettingCount, 1, 10_080},
	SettingBookingMaxDays:       {SettingCount, 1, 365},
	SettingBookingSlotMinutes:   {SettingCount, 5, 240},
	SettingRemovalMaxDays:       {SettingCount, 1, 365},
	SettingTowingCompletionCode: {SettingCount, 4, 8},
}

func ValidateSetting(key, value string) error {
	rule, known := settingRules[key]
	if !known {
		return validationError("key", "Esta configuração não existe.")
	}
	if rule.kind == SettingText {
		if int64(len([]rune(value))) > rule.max {
			return validationError("value", "O valor é demasiado longo.")
		}
		return nil
	}
	number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || number < rule.min || number > rule.max {
		return validationError("value", "O valor tem de ser um inteiro entre "+strconv.FormatInt(rule.min, 10)+" e "+strconv.FormatInt(rule.max, 10)+".")
	}
	return nil
}
