package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type NearbyWorkshop struct {
	ID        uuid.UUID
	Name      string
	Point     entities.GeoPoint
	Line      string
	DistanceM float64
}

type IWorkshopDirectory interface {
	Nearest(ctx context.Context, point entities.GeoPoint) (*NearbyWorkshop, error)
}
