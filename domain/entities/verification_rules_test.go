package entities_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

const validBAIIBAN = "AO06004000001234567810142"

func TestAngolanIBANPassesMod97(t *testing.T) {
	assert.True(t, entities.ValidAngolanIBAN(validBAIIBAN))
	assert.True(t, entities.ValidAngolanIBAN("AO06000600001234567810173"))
	assert.Equal(t, validBAIIBAN, entities.NormalizeIBAN(" ao06 0040 0000 1234 5678 1014 2 "))
}

func TestAngolanIBANRejectsWhatIsNotOne(t *testing.T) {
	cases := map[string]string{
		"wrong check digits":  "AO06004000001234567810143",
		"too short":           "AO0600400000123456781014",
		"too long":            "AO060040000012345678101420",
		"another country":     "PT50000201231234567890154",
		"letters in the bban": "AO0600400000123456781014A",
		"empty":               "",
	}
	for name, iban := range cases {
		assert.False(t, entities.ValidAngolanIBAN(iban), name)
	}
}

func TestBankCodeIsDigitsFiveToEight(t *testing.T) {
	assert.Equal(t, "0040", entities.BankCodeOf(validBAIIBAN))
	assert.Equal(t, "", entities.BankCodeOf("AO06"))
}

func TestIBANIsMaskedInAnswers(t *testing.T) {
	assert.Equal(t, "AO06 •••• 1014 2", entities.MaskIBAN(validBAIIBAN))
	assert.Equal(t, "••••", entities.MaskIBAN("short"))
}

func TestPlateIsNormalizedWithHyphens(t *testing.T) {
	for _, raw := range []string{"LD-45-21-AF", "ld452 1af", "LD452 1AF", "ld-45-21-af"} {
		plate, ok := entities.NormalizePlate(raw)
		assert.True(t, ok, raw)
		assert.Equal(t, "LD-45-21-AF", plate, raw)
	}
	for _, raw := range []string{"LD-45-21", "1D-45-21-AF", "LD-4A-21-AF", "LDA-45-21-AF", ""} {
		_, ok := entities.NormalizePlate(raw)
		assert.False(t, ok, raw)
	}
}

func TestCompanyTaxIDHasTenDigits(t *testing.T) {
	assert.True(t, entities.ValidCompanyTaxID("5417123456"))
	assert.False(t, entities.ValidCompanyTaxID("541712345"))
	assert.False(t, entities.ValidCompanyTaxID("54171234567"))
	assert.False(t, entities.ValidCompanyTaxID("5417 123456"))
	assert.False(t, entities.ValidCompanyTaxID("541712345A"))
}

func TestIDCardNumber(t *testing.T) {
	assert.True(t, entities.ValidIDNumber(entities.NormalizeIDNumber(" 005487123la041 ")))
	assert.False(t, entities.ValidIDNumber("005487123L041"))
	assert.False(t, entities.ValidIDNumber("00548712LA0411"))
}

func TestNamesAreComparedWithoutAccentsCaseOrExtraSpaces(t *testing.T) {
	assert.True(t, entities.SameName("João  Sebastião   Kiala", "joao sebastiao KIALA"))
	assert.True(t, entities.SameName(" Conceição ", "conceicao"))
	assert.False(t, entities.SameName("João Kiala", "João Kiala Neto"))
	assert.False(t, entities.SameName("", ""))
}

func TestCriminalRecordWindowOfNinetyDays(t *testing.T) {
	now := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	assert.True(t, entities.IssuedWithinDays(now, now, 90))
	assert.True(t, entities.IssuedWithinDays(now.AddDate(0, 0, -90), now, 90))
	assert.False(t, entities.IssuedWithinDays(now.AddDate(0, 0, -91), now, 90))
	assert.False(t, entities.IssuedWithinDays(now.AddDate(0, 0, 1), now, 90), "not in the future")
}

func TestCommercialCertificateWindowOfSixMonths(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	assert.True(t, entities.IssuedWithinMonths(time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC), now, 6))
	assert.False(t, entities.IssuedWithinMonths(time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), now, 6))
	assert.False(t, entities.IssuedWithinMonths(time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), now, 6))
}

func TestFutureDatesAreComparedByDay(t *testing.T) {
	now := time.Date(2026, 11, 5, 23, 0, 0, 0, time.UTC)
	assert.False(t, entities.IsFutureDate(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), now))
	assert.True(t, entities.IsFutureDate(time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), now))
}

func TestAgeCountsTheBirthdayOnlyWhenItArrives(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 18, entities.AgeAt(time.Date(2008, 11, 5, 0, 0, 0, 0, time.UTC), now))
	assert.Equal(t, 17, entities.AgeAt(time.Date(2008, 11, 6, 0, 0, 0, 0, time.UTC), now))
	assert.Equal(t, 17, entities.AgeAt(time.Date(2008, 12, 1, 0, 0, 0, 0, time.UTC), now))
}
