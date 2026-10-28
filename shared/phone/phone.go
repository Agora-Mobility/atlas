package phone

import (
	"strings"

	"github.com/nyaruka/phonenumbers"
)

const DefaultRegion = "AO"

func Normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	number, err := phonenumbers.Parse(trimmed, DefaultRegion)
	if err != nil || !phonenumbers.IsValidNumber(number) {
		return trimmed
	}
	return phonenumbers.Format(number, phonenumbers.E164)
}

func IsValid(value string) bool {
	number, err := phonenumbers.Parse(value, DefaultRegion)
	if err != nil || !phonenumbers.IsValidNumber(number) {
		return false
	}
	return phonenumbers.Format(number, phonenumbers.E164) == value
}
