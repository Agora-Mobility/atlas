package entities

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

const (
	AttrIncludedKm     = "included_km"
	AttrExtraKmCents   = "extra_km_cents"
	AttrMinCapacityKg  = "min_capacity_kg"
	decimalBase        = 10
	completionCodeDefs = 4
)

var (
	ErrCompletionCodeWrong  = errors.New("the code is wrong")
	ErrCompletionCodeLocked = errors.New("too many wrong codes, an operator has to finish this request")
	ErrTowingDetailsMissing = errors.New("towing needs the incident and whether the vehicle moves")
	ErrUnknownVehicleType   = errors.New("this vehicle type does not exist")
	ErrUnknownIncident      = errors.New("this incident does not exist")
)

// TowingFare charges the fixed price of the type, which includes some
// distance, and each km above that in proportion.
func TowingFare(item *CatalogItem, priceCents int64, distanceMeters int) int64 {
	over := int64(distanceMeters) - item.AttrInt(AttrIncludedKm)*metersPerKm
	if over <= 0 {
		return priceCents
	}
	return priceCents + roundedDivide(over*item.AttrInt(AttrExtraKmCents), metersPerKm)
}

func NewCompletionCode(digits int) (string, error) {
	if digits <= 0 {
		digits = completionCodeDefs
	}
	limit := new(big.Int).Exp(big.NewInt(decimalBase), big.NewInt(int64(digits)), nil)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	return strings.Repeat("0", digits-len(strconv.FormatInt(value.Int64(), decimalBase))) + strconv.FormatInt(value.Int64(), decimalBase), nil
}
