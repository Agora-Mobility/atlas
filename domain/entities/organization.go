package entities

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Organization struct {
	ID        uuid.UUID
	Type      enums.OrganizationType
	Name      string
	NIF       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var nifPattern = regexp.MustCompile(`^(\d{10}|\d{9}[A-Z]{2}\d{3})$`)

func NewPartnerOrganization(name, nif string) (*Organization, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	name, nif, err = partnerDetails(name, nif)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Organization{ID: id, Type: enums.OrganizationTypePartner, Name: name, NIF: nif, CreatedAt: now, UpdatedAt: now}, nil
}

func (o *Organization) UpdateDetails(name, nif string) (bool, error) {
	name, nif, err := partnerDetails(name, nif)
	if err != nil {
		return false, err
	}
	if name == o.Name && nif == o.NIF {
		return false, nil
	}
	o.Name, o.NIF, o.UpdatedAt = name, nif, time.Now().UTC()
	return true, nil
}

func partnerDetails(name, nif string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", validationError("name", "O nome é obrigatório.")
	}
	nif = NormalizeNIF(nif)
	if nif != "" && !nifPattern.MatchString(nif) {
		return "", "", validationError("nif", "O NIF é inválido (10 dígitos, ou o número do BI para empresários em nome individual).")
	}
	return name, nif, nil
}

func NormalizeNIF(nif string) string {
	return strings.ToUpper(strings.Join(strings.Fields(nif), ""))
}

func (o *Organization) IsPartner() bool {
	return o.Type == enums.OrganizationTypePartner
}
