package dtos

import (
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type AddressInput struct {
	Lat          float64 `json:"lat" binding:"required" example:"-8.84"`
	Lng          float64 `json:"lng" binding:"required" example:"13.23"`
	Line         string  `json:"line" binding:"max=200" example:"Rua Rainha Ginga, 29"`
	Reference    string  `json:"reference" binding:"max=140" example:"Portão azul, junto à farmácia"`
	Neighborhood string  `json:"neighborhood" binding:"max=120" example:"Ingombota"`
	City         string  `json:"city" binding:"max=120" example:"Luanda"`
	Province     string  `json:"province" binding:"max=120" example:"Luanda"`
	CountryCode  string  `json:"country_code" binding:"max=2" example:"AO"`
}

func (in AddressInput) ToAddress() entities.Address {
	return entities.Address{
		Point: entities.GeoPoint{Lat: in.Lat, Lng: in.Lng}, Line: in.Line, Reference: in.Reference, Neighborhood: in.Neighborhood,
		City: in.City, Province: in.Province, CountryCode: in.CountryCode,
	}
}

type AddressView struct {
	ID           uuid.UUID `json:"id"`
	Lat          float64   `json:"lat"`
	Lng          float64   `json:"lng"`
	Line         string    `json:"line,omitempty"`
	Reference    string    `json:"reference,omitempty"`
	Neighborhood string    `json:"neighborhood,omitempty"`
	City         string    `json:"city,omitempty"`
	Province     string    `json:"province,omitempty"`
	CountryCode  string    `json:"country_code,omitempty"`
}

func ToAddressView(a *entities.Address) AddressView {
	return AddressView{
		ID: a.ID, Lat: a.Point.Lat, Lng: a.Point.Lng, Line: a.Line, Reference: a.Reference, Neighborhood: a.Neighborhood,
		City: a.City, Province: a.Province, CountryCode: a.CountryCode,
	}
}

type UserAddressInput struct {
	AddressInput
	Kind  string `json:"kind" binding:"required" example:"home"`
	Label string `json:"label" binding:"max=120" example:"Casa"`
}

type UserAddressView struct {
	AddressView
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
}

type UserAddressList struct {
	Items []UserAddressView `json:"items"`
}
