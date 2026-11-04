package entities

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

type AddressKind string

const (
	AddressHome  AddressKind = "home"
	AddressWork  AddressKind = "work"
	AddressOther AddressKind = "other"

	maxAddressPartLength = 120
)

var ErrAddressKindTaken = errors.New("this user already has an address of this kind")

type Address struct {
	ID           uuid.UUID
	Point        GeoPoint
	Line         string
	Reference    string
	Neighborhood string
	City         string
	Province     string
	CountryCode  string
}

func (a Address) Normalized(field string) (Address, error) {
	if err := a.Point.Validate(field); err != nil {
		return a, err
	}
	a.Line = strings.TrimSpace(a.Line)
	a.Reference = strings.TrimSpace(a.Reference)
	a.Neighborhood = strings.TrimSpace(a.Neighborhood)
	a.City = strings.TrimSpace(a.City)
	a.Province = strings.TrimSpace(a.Province)
	a.CountryCode = strings.ToUpper(strings.TrimSpace(a.CountryCode))
	if len([]rune(a.Line)) > maxAddressLength {
		return a, validationError(field+".line", "A morada tem no máximo 200 caracteres.")
	}
	if len([]rune(a.Reference)) > maxReferenceLength {
		return a, validationError(field+".reference", "O ponto de referência tem no máximo 140 caracteres.")
	}
	for _, part := range []string{a.Neighborhood, a.City, a.Province} {
		if len([]rune(part)) > maxAddressPartLength {
			return a, validationError(field, "O bairro, a cidade e a província têm no máximo 120 caracteres.")
		}
	}
	if a.CountryCode != "" && len(a.CountryCode) != 2 {
		return a, validationError(field+".country_code", "O país tem de ter 2 letras.")
	}
	if a.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return a, err
		}
		a.ID = id
	}
	return a, nil
}

type UserAddress struct {
	Address
	Kind  AddressKind
	Label string
}

func (a UserAddress) Normalized() (UserAddress, error) {
	address, err := a.Address.Normalized("address")
	if err != nil {
		return a, err
	}
	a.Address = address
	a.Label = strings.TrimSpace(a.Label)
	if len([]rune(a.Label)) > maxAddressPartLength {
		return a, validationError("label", "O nome tem no máximo 120 caracteres.")
	}
	if a.Kind != AddressHome && a.Kind != AddressWork && a.Kind != AddressOther {
		return a, validationError("kind", "O tipo tem de ser home, work ou other.")
	}
	return a, nil
}
