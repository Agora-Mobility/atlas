package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var onboardingNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func validVehicle() entities.ProviderVehicle {
	return entities.ProviderVehicle{Brand: "Toyota", Model: "Corolla", Year: 2020, Color: "Branco", Plate: " ld-45-21-af ", Seats: 5}
}

func TestProviderStatusFollowsTheReviewFlow(t *testing.T) {
	profile := &entities.ProviderProfile{Status: entities.ProviderDraft}
	service := uuid.New()

	assert.ErrorIs(t, profile.Approve(service, onboardingNow), entities.ErrInvalidProviderTransition, "a draft cannot skip the review")
	require.NoError(t, profile.Submit(onboardingNow))
	assert.Equal(t, entities.ProviderPending, profile.Status)
	assert.ErrorIs(t, profile.RequestService(service), entities.ErrProfileLocked, "under review nothing changes")

	assert.ErrorIs(t, profile.Reject(" ", onboardingNow), entities.ErrReviewReasonRequired)
	require.NoError(t, profile.Reject("Fotos ilegíveis", onboardingNow))
	assert.Equal(t, "Fotos ilegíveis", profile.StatusReason)
	require.NoError(t, profile.RequestService(service), "after a rejection the provider corrects")

	require.NoError(t, profile.Submit(onboardingNow))
	assert.Empty(t, profile.StatusReason)
	require.NoError(t, profile.Approve(service, onboardingNow))
	assert.Equal(t, service, *profile.ServiceID)
	assert.ErrorIs(t, profile.SetVehicle(validVehicle(), onboardingNow), entities.ErrProfileLocked)

	assert.ErrorIs(t, profile.Suspend("", onboardingNow), entities.ErrReviewReasonRequired)
	require.NoError(t, profile.Suspend("Queixas", onboardingNow))
	require.NoError(t, profile.Approve(service, onboardingNow), "a suspension can be lifted")
}

func TestPersonalDataAndVehicleAreValidated(t *testing.T) {
	profile := &entities.ProviderProfile{Status: entities.ProviderDraft}

	assert.Error(t, profile.SetPersonalData(onboardingNow.AddDate(-17, 0, 0), "007", "Luanda", onboardingNow), "under 18")
	assert.Error(t, profile.SetPersonalData(onboardingNow.AddDate(-30, 0, 0), " ", "Luanda", onboardingNow))
	require.NoError(t, profile.SetPersonalData(onboardingNow.AddDate(-30, 0, 0), "007", "Luanda", onboardingNow))

	require.NoError(t, profile.SetVehicle(validVehicle(), onboardingNow))
	assert.Equal(t, "LD-45-21-AF", profile.VehiclePlate)
	assert.Equal(t, "Toyota Corolla Branco", profile.VehicleDescription)

	old := validVehicle()
	old.Year = 1900
	assert.Error(t, profile.SetVehicle(old, onboardingNow))
	empty := validVehicle()
	empty.Brand = ""
	assert.Error(t, profile.SetVehicle(empty, onboardingNow))
}

func TestMissingListsEverythingRequiredByTheService(t *testing.T) {
	requirements := &entities.OnboardingRequirements{
		RequiresVehicle: true, MinVehicleSeats: 7,
		Documents: []entities.DocumentType{{Code: "id_card", Name: "BI"}, {Code: "insurance", Name: "Seguro"}, {Code: "inspection", Name: "Inspeção"}},
	}
	profile := &entities.ProviderProfile{Status: entities.ProviderDraft, Vehicle: validVehicle()}
	profile.Vehicle.Plate = "LD"
	documents := []*entities.ProviderDocument{
		{Type: "id_card", Status: entities.DocumentUploaded},
		{Type: "insurance", Status: entities.DocumentRejected, RejectionReason: "Caducado"},
	}

	missing := requirements.MissingFor(profile, "", documents)

	assert.Contains(t, missing, "personal.name")
	assert.Contains(t, missing, "personal.birth_date")
	assert.Contains(t, missing, "vehicle.seats", "the XL rule is data")
	assert.Contains(t, missing, "documents.inspection")
	assert.Contains(t, missing["documents.insurance"], "Caducado")
	assert.NotContains(t, missing, "documents.id_card")
}

func TestApprovalNeedsEveryRequiredDocumentApproved(t *testing.T) {
	requirements := &entities.OnboardingRequirements{Documents: []entities.DocumentType{{Code: "id_card", Name: "BI"}, {Code: "insurance", Name: "Seguro"}}}
	pending := requirements.NotApproved([]*entities.ProviderDocument{{Type: "id_card", Status: entities.DocumentApproved}, {Type: "insurance", Status: entities.DocumentUploaded}})

	assert.Len(t, pending, 1)
	assert.Contains(t, pending, "documents.insurance")
}

func TestDocumentReviewNeedsAReasonToReject(t *testing.T) {
	document := &entities.ProviderDocument{Status: entities.DocumentUploaded}

	assert.ErrorIs(t, document.Review(false, " ", nil, onboardingNow), entities.ErrReviewReasonRequired)
	require.NoError(t, document.Review(false, "Ilegível", nil, onboardingNow))
	assert.Equal(t, entities.DocumentRejected, document.Status)
	require.NoError(t, document.Review(true, "", nil, onboardingNow))
	assert.Equal(t, entities.DocumentApproved, document.Status)
	assert.Empty(t, document.RejectionReason)
}
