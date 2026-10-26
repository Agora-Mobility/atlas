// Package entities holds the domain entities. Entities validate themselves
// and never depend on infrastructure.
package entities

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/validators"
	"github.com/Agora-Mobility/atlas/domain/valueobjects"
)

const (
	exampleNameMaxLength        = 120
	exampleDescriptionMaxLength = 500
)

// Example is the reference entity used to prove the layers end to end.
type Example struct {
	ID          uuid.UUID
	Name        string
	Description string
	Location    valueobjects.GeoPoint
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewExample(name, description string, latitude, longitude float64) (*Example, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &Example{
		ID:          id,
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Location:    valueobjects.GeoPoint{Latitude: latitude, Longitude: longitude},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := e.isValid(); err != nil {
		return nil, err
	}
	return e, nil
}

// Update changes the mutable fields and revalidates the entity.
func (e *Example) Update(name, description string) error {
	updated := *e
	updated.Name = strings.TrimSpace(name)
	updated.Description = strings.TrimSpace(description)
	if err := updated.isValid(); err != nil {
		return err
	}
	updated.UpdatedAt = time.Now().UTC()
	*e = updated
	return nil
}

func (e *Example) isValid() error {
	errs := validators.Errors{}
	errs.Required("name", e.Name)
	errs.MaxLength("name", e.Name, exampleNameMaxLength)
	errs.MaxLength("description", e.Description, exampleDescriptionMaxLength)
	e.Location.Validate(errs)
	return errs.Err()
}
