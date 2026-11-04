package experience

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func (u *Usecase) TowingOptions(ctx context.Context, point *entities.GeoPoint) (*dtos.TowingOptions, error) {
	types, err := u.Reference.Items(ctx, entities.CatalogTowingVehicle, "towing", false)
	if err != nil {
		return nil, err
	}
	incidents, err := u.Reference.Items(ctx, entities.CatalogTowingIncident, "towing", false)
	if err != nil {
		return nil, err
	}
	view := &dtos.TowingOptions{VehicleTypes: make([]dtos.CatalogItemView, 0, len(types)), Incidents: make([]dtos.CatalogItemView, 0, len(incidents))}
	for i := range types {
		view.VehicleTypes = append(view.VehicleTypes, toItemView(&types[i]))
	}
	for i := range incidents {
		view.Incidents = append(view.Incidents, toItemView(&incidents[i]))
	}
	if point == nil {
		return view, nil
	}
	if err := point.Validate("position"); err != nil {
		return nil, err
	}
	nearest, err := u.Workshops.Nearest(ctx, *point)
	if err != nil {
		return nil, err
	}
	if nearest != nil {
		view.RecommendedWorkshop = &dtos.RecommendedWorkshop{ID: nearest.ID, Name: nearest.Name, Lat: nearest.Point.Lat, Lng: nearest.Point.Lng, Address: nearest.Line, DistanceM: int(nearest.DistanceM)}
	}
	return view, nil
}
