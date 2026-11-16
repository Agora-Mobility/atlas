package entities

import (
	"encoding/json"
	"errors"
	"slices"
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
	maxAttrCodes          = 20

	AttrProperties = "properties"
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
	values, err := i.attrValues()
	if err != nil {
		return validationError("attrs", "Os atributos têm de ser um objeto.")
	}
	for name, limit := range limits[i.Kind] {
		raw, present := values[name]
		if !present {
			return validationError("attrs."+name, "Este atributo é obrigatório.")
		}
		var value json.Number
		if json.Unmarshal(raw, &value) != nil {
			return validationError("attrs."+name, "Este atributo tem de ser um número.")
		}
		number, err := value.Int64()
		if err != nil || number < limit[0] || number > limit[1] {
			return validationError("attrs."+name, "O valor tem de estar entre "+strconv.FormatInt(limit[0], 10)+" e "+strconv.FormatInt(limit[1], 10)+".")
		}
	}
	if i.Kind == CatalogRemovalItem {
		return validateCodeList(values[AttrProperties], "attrs."+AttrProperties)
	}
	return nil
}

func validateCodeList(raw json.RawMessage, field string) error {
	if len(raw) == 0 {
		return nil
	}
	var codes []string
	if json.Unmarshal(raw, &codes) != nil || len(codes) > maxAttrCodes {
		return validationError(field, "Indica uma lista com até 20 códigos.")
	}
	for _, code := range codes {
		if !reasonCodePattern.MatchString(code) {
			return validationError(field, "O código usa letras minúsculas, números e _.")
		}
	}
	return nil
}

func (i *CatalogItem) attrValues() (map[string]json.RawMessage, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(i.Attrs, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (i *CatalogItem) AttrCodes(name string) []string {
	values, err := i.attrValues()
	if err != nil {
		return nil
	}
	var codes []string
	if json.Unmarshal(values[name], &codes) != nil {
		return nil
	}
	return codes
}

func (i *CatalogItem) AppliesToProperty(code string) bool {
	properties := i.AttrCodes(AttrProperties)
	return len(properties) == 0 || slices.Contains(properties, code)
}

func (i *CatalogItem) AttrInt(name string) int64 {
	values, err := i.attrValues()
	if err != nil {
		return 0
	}
	var value json.Number
	if json.Unmarshal(values[name], &value) != nil {
		return 0
	}
	number, err := value.Int64()
	if err != nil {
		return 0
	}
	return number
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
	SettingMaxWorkshopPhotos    = "max_workshop_photos"
	SettingTowingCompletionCode = "towing_code_digits"
	SettingCompletionAttempts   = "completion_code_attempts"
	SettingPrepayHoldMinutes    = "prepay_hold_minutes"
	SettingCustomSmallDm3       = "removal_custom_small_dm3"
	SettingCustomMediumDm3      = "removal_custom_medium_dm3"
	SettingCustomLargeDm3       = "removal_custom_large_dm3"
)

var settingRules = map[string]settingRule{
	SettingMaxTipCents:          {SettingCents, 0, MaxFeeCents},
	SettingMaxPhotos:            {SettingCount, 0, 50},
	SettingMaxPhotosPerBooking:  {SettingCount, 0, 50},
	SettingMaxWorkshopPhotos:    {SettingCount, 0, 50},
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
	SettingCompletionAttempts:   {SettingCount, 1, 100},
	SettingPrepayHoldMinutes:    {SettingCount, 1, 10_080},
	SettingCustomSmallDm3:       {SettingCount, 1, 100_000},
	SettingCustomMediumDm3:      {SettingCount, 1, 100_000},
	SettingCustomLargeDm3:       {SettingCount, 1, 100_000},
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
