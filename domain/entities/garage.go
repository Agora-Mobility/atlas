package entities

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCustomerVehicleNotFound = errors.New("vehicle not found")
	ErrPlateTaken              = errors.New("this plate is already in your vehicles")
	ErrPhotoTypeNotAllowed     = errors.New("this type of file is not allowed")
	ErrPhotoTooLarge           = errors.New("the file is too large")
	ErrPhotoLimit              = errors.New("too many photos")
	ErrPhotoNotFound           = errors.New("photo not found")
	ErrPhotoNotUploaded        = errors.New("the photo was not uploaded or does not match what was announced")
)

var vehicleFuels = []string{"petrol", "diesel", "hybrid", "electric", "gas"}

type CustomerVehicle struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Brand      string
	Model      string
	Plate      string
	Year       int
	Fuel       string
	CreatedAt  time.Time
}

func (v *CustomerVehicle) Normalize(now time.Time) error {
	v.Brand, v.Model = strings.TrimSpace(v.Brand), strings.TrimSpace(v.Model)
	v.Plate = strings.ToUpper(strings.TrimSpace(v.Plate))
	for _, field := range []string{v.Brand, v.Model, v.Plate} {
		if field == "" || len([]rune(field)) > maxProfileTextLen {
			return validationError("vehicle", "Indica a marca, o modelo e a matrícula.")
		}
	}
	if v.Year < minVehicleYear || v.Year > now.Year()+1 {
		return validationError("year", "O ano da viatura não é válido.")
	}
	if !slices.Contains(vehicleFuels, v.Fuel) {
		return validationError("fuel", "O combustível não é válido.")
	}
	return nil
}

type MediaUpload struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	RequestID   *uuid.UUID
	CreatedAt   time.Time
}
