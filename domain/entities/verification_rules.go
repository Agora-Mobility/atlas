package entities

import (
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	angolanIBANLength = 25
	angolanIBANPrefix = "AO06"
	mod97Remainder    = 1
	maskDot           = "••••"
)

var (
	angolanIBAN    = regexp.MustCompile(`^AO06\d{21}$`)
	angolanPlate   = regexp.MustCompile(`^([A-Z]{2})(\d{2})(\d{2})([A-Z]{2})$`)
	companyTaxID   = regexp.MustCompile(`^\d{10}$`)
	ninetySevenBig = big.NewInt(97)
)

func NormalizeIBAN(raw string) string {
	return strings.ToUpper(strings.Join(strings.Fields(raw), ""))
}

func ValidAngolanIBAN(iban string) bool {
	if len(iban) != angolanIBANLength || !angolanIBAN.MatchString(iban) {
		return false
	}
	return mod97(iban[4:]+iban[:4]) == mod97Remainder
}

func mod97(rearranged string) int64 {
	var digits strings.Builder
	for _, char := range rearranged {
		if unicode.IsLetter(char) {
			digits.WriteString(big.NewInt(int64(char-'A') + 10).String())
			continue
		}
		digits.WriteRune(char)
	}
	number, ok := new(big.Int).SetString(digits.String(), 10)
	if !ok {
		return -1
	}
	return new(big.Int).Mod(number, ninetySevenBig).Int64()
}

func BankCodeOf(iban string) string {
	if len(iban) < 8 {
		return ""
	}
	return iban[4:8]
}

func MaskIBAN(iban string) string {
	if len(iban) != angolanIBANLength {
		return maskDot
	}
	tail := iban[len(iban)-5:]
	return iban[:4] + " " + maskDot + " " + tail[:4] + " " + tail[4:]
}

func NormalizePlate(raw string) (string, bool) {
	compact := strings.ToUpper(strings.NewReplacer("-", "", " ", "", ".", "").Replace(raw))
	parts := angolanPlate.FindStringSubmatch(compact)
	if parts == nil {
		return "", false
	}
	return strings.Join(parts[1:], "-"), true
}

func ValidCompanyTaxID(nif string) bool {
	return companyTaxID.MatchString(nif)
}

func NormalizeIDNumber(raw string) string {
	return strings.ToUpper(strings.Join(strings.Fields(raw), ""))
}

func ValidIDNumber(number string) bool {
	return idCardNumber.MatchString(number)
}

var accents = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

func NormalizeName(name string) string {
	plain, _, err := transform.String(accents, name)
	if err != nil {
		plain = name
	}
	return strings.ToLower(strings.Join(strings.Fields(plain), " "))
}

func SameName(a, b string) bool {
	left, right := NormalizeName(a), NormalizeName(b)
	return left != "" && left == right
}

func DateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func IsFutureDate(date, now time.Time) bool {
	return DateOnly(date).After(DateOnly(now))
}

func IssuedWithinDays(issued, now time.Time, days int) bool {
	issuedOn, today := DateOnly(issued), DateOnly(now)
	return !issuedOn.After(today) && !issuedOn.Before(today.AddDate(0, 0, -days))
}

func IssuedWithinMonths(issued, now time.Time, months int) bool {
	issuedOn, today := DateOnly(issued), DateOnly(now)
	return !issuedOn.After(today) && issuedOn.After(today.AddDate(0, -months, 0))
}

func AgeAt(birth, now time.Time) int {
	birthOn, today := DateOnly(birth), DateOnly(now)
	age := today.Year() - birthOn.Year()
	if today.Month() < birthOn.Month() || (today.Month() == birthOn.Month() && today.Day() < birthOn.Day()) {
		age--
	}
	return age
}
