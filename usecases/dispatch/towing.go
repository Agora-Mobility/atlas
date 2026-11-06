package dispatch

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func (u *Usecase) settingNumber(ctx context.Context, key string) (int, error) {
	raw, err := u.Reference.Setting(ctx, key)
	if err != nil || raw == "" {
		return 0, err
	}
	return strconv.Atoi(raw)
}

func (u *Usecase) prepareTowing(ctx context.Context, request *entities.ServiceRequest, policy *entities.DispatchPolicy, in dtos.CreateRequestInput) error {
	request.VehicleTypeID = request.Fare.VehicleTypeID
	if policy.RequiresCode {
		digits, err := u.settingNumber(ctx, entities.SettingTowingCompletionCode)
		if err != nil {
			return err
		}
		if request.CompletionCode, err = entities.NewCompletionCode(digits); err != nil {
			return err
		}
	}
	if request.VehicleTypeID == nil || isRemovalEstimate(request.Fare) {
		return nil
	}
	if in.Towing == nil || in.Towing.Drivable == nil {
		return entities.ErrTowingDetailsMissing
	}
	incident, err := u.Reference.Item(ctx, in.Towing.IncidentID)
	if err != nil {
		return err
	}
	if incident == nil || incident.Kind != entities.CatalogTowingIncident || !incident.IsActive {
		return entities.ErrUnknownIncident
	}
	vehicleType, err := u.Reference.Item(ctx, *request.VehicleTypeID)
	if err != nil {
		return err
	}
	if vehicleType == nil {
		return entities.ErrUnknownVehicleType
	}
	var fields map[string]any
	if err := json.Unmarshal(request.Details, &fields); err != nil || fields == nil {
		fields = map[string]any{}
	}
	fields["towing"] = map[string]any{"vehicle_type": vehicleType.Label, "vehicle_type_code": vehicleType.Code, "incident": incident.Label, "incident_code": incident.Code, "drivable": *in.Towing.Drivable}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	request.Details = encoded
	return nil
}

func (u *Usecase) filterByVehicleType(ctx context.Context, request *entities.ServiceRequest, providers []entities.EligibleProvider) ([]entities.EligibleProvider, error) {
	if request.VehicleTypeID == nil {
		return providers, nil
	}
	vehicleType, err := u.Reference.Item(ctx, *request.VehicleTypeID)
	if err != nil || vehicleType == nil {
		return providers, err
	}
	minimum := vehicleType.AttrInt(entities.AttrMinCapacityKg)
	out := make([]entities.EligibleProvider, 0, len(providers))
	for i := range providers {
		if providers[i].CanCarry(minimum) {
			out = append(out, providers[i])
		}
	}
	return out, nil
}

func (u *Usecase) checkCompletionCode(ctx context.Context, providerID, requestID uuid.UUID, code string) error {
	wrong := false
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		request, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if request == nil || !request.IsProvider(providerID) {
			return ErrRequestNotFound
		}
		if request.CompletionCode == "" || request.Status != entities.RequestInProgress {
			return nil
		}
		attempts, err := u.settingNumber(ctx, entities.SettingCompletionAttempts)
		if err != nil {
			return err
		}
		if request.CompletionTries >= attempts {
			return entities.ErrCompletionCodeLocked
		}
		if code == request.CompletionCode {
			return nil
		}
		wrong = true
		request.CompletionTries++
		return u.Requests.Update(ctx, request, request.Touch(u.now()))
	})
	if err != nil {
		return err
	}
	if wrong {
		return entities.ErrCompletionCodeWrong
	}
	return nil
}
