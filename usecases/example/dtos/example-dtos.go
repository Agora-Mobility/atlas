// Package dtos holds the input and output types of the example usecase.
package dtos

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

type CreateExampleInput struct {
	Name        string  `json:"name" binding:"required,max=120" example:"Marginal de Luanda"`
	Description string  `json:"description" binding:"max=500" example:"Ponto de referência"`
	Latitude    float64 `json:"latitude" binding:"gte=-90,lte=90" example:"-8.8130"`
	Longitude   float64 `json:"longitude" binding:"gte=-180,lte=180" example:"13.2343"`
}

type GetExampleInput struct {
	ID uuid.UUID
}

type ListExamplesInput struct {
	pagination.Params
}

type ExampleOutput struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ExamplePageOutput exists so swag can document the generic page.
type ExamplePageOutput = pagination.Page[ExampleOutput]

func ToExampleOutput(e *entities.Example) ExampleOutput {
	return ExampleOutput{
		ID:          e.ID,
		Name:        e.Name,
		Description: e.Description,
		Latitude:    e.Location.Latitude,
		Longitude:   e.Location.Longitude,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}
