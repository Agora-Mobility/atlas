package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	ExampleAggregate = "example"
	ExampleCreated   = "example.created"
)

// ExampleCreatedV1 is published when an example is created.
type ExampleCreatedV1 struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	CreatedAt time.Time `json:"created_at"`
}

func (ExampleCreatedV1) EventType() string     { return ExampleCreated }
func (ExampleCreatedV1) EventVersion() int     { return 1 }
func (ExampleCreatedV1) AggregateType() string { return ExampleAggregate }
func (e ExampleCreatedV1) AggregateID() string { return e.ID.String() }
