package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var providerNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func validVehicle() entities.Vehicle {
	return entities.Vehicle{Brand: "Toyota", Model: "Corolla", Year: 2020, Color: "Branco", Plate: " ld-45-21-af ", Seats: 5}
}

func TestProviderStatusFollowsTheReviewFlow(t *testing.T) {
	profile := &entities.Provider{Status: entities.ProviderDraft}
	service := uuid.New()

	assert.ErrorIs(t, profile.Approve(service, providerNow), entities.ErrInvalidProviderTransition, "a draft cannot skip the review")
	require.NoError(t, profile.Submit(providerNow))
	assert.Equal(t, entities.ProviderPending, profile.Status)
	assert.ErrorIs(t, profile.RequestService(service), entities.ErrProfileLocked, "under review nothing changes")

	assert.ErrorIs(t, profile.Reject(" ", providerNow), entities.ErrReviewReasonRequired)
	require.NoError(t, profile.Reject("Fotos ilegíveis", providerNow))
	assert.Equal(t, "Fotos ilegíveis", profile.StatusReason)
	require.NoError(t, profile.RequestService(service), "after a rejection the provider corrects")

	require.NoError(t, profile.Submit(providerNow))
	assert.Empty(t, profile.StatusReason)
	require.NoError(t, profile.Approve(service, providerNow))
	assert.Equal(t, service, *profile.ServiceID)
	assert.ErrorIs(t, profile.SetVehicle(validVehicle(), providerNow), entities.ErrProfileLocked)

	assert.ErrorIs(t, profile.Suspend("", providerNow), entities.ErrReviewReasonRequired)
	require.NoError(t, profile.Suspend("Queixas", providerNow))
	require.NoError(t, profile.Approve(service, providerNow), "a suspension can be lifted")
}

func TestPersonalDataAndVehicleAreValidated(t *testing.T) {
	profile := &entities.Provider{Status: entities.ProviderDraft}

	home := entities.Address{Point: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, Line: "Rua Rainha Ginga, 29"}
	assert.Error(t, profile.SetPersonalData(providerNow.AddDate(-17, 0, 0), "007", home, providerNow), "under 18")
	assert.Error(t, profile.SetPersonalData(providerNow.AddDate(-30, 0, 0), " ", home, providerNow))
	assert.Error(t, profile.SetPersonalData(providerNow.AddDate(-30, 0, 0), "007", entities.Address{Line: "Sem pin"}, providerNow), "the address needs latitude and longitude")
	assert.Error(t, profile.SetPersonalData(providerNow.AddDate(-30, 0, 0), "007", entities.Address{Point: home.Point}, providerNow), "the address needs a line")
	require.NoError(t, profile.SetPersonalData(providerNow.AddDate(-30, 0, 0), "007", home, providerNow))
	first := profile.HomeAddress.ID
	require.NoError(t, profile.SetPersonalData(providerNow.AddDate(-30, 0, 0), "007", entities.Address{Point: home.Point, Line: "Outra rua"}, providerNow))
	assert.Equal(t, first, profile.HomeAddress.ID, "editing keeps the same address row")

	require.NoError(t, profile.SetVehicle(validVehicle(), providerNow))
	assert.Equal(t, "LD-45-21-AF", profile.Vehicle.Plate)
	assert.Equal(t, "Toyota Corolla Branco", profile.Vehicle.Description())

	old := validVehicle()
	old.Year = 1900
	assert.Error(t, profile.SetVehicle(old, providerNow))
	empty := validVehicle()
	empty.Brand = ""
	assert.Error(t, profile.SetVehicle(empty, providerNow))
}

func TestMissingListsEverythingRequiredByTheService(t *testing.T) {
	requirements := &entities.ProviderRequirements{
		RequiresVehicle: true, MinVehicleSeats: 7,
		Documents: []entities.DocumentType{{Code: "id_card", Name: "BI"}, {Code: "insurance", Name: "Seguro"}, {Code: "inspection", Name: "Inspeção"}},
	}
	profile := &entities.Provider{Status: entities.ProviderDraft, Vehicle: validVehicle()}
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
	requirements := &entities.ProviderRequirements{Documents: []entities.DocumentType{{Code: "id_card", Name: "BI"}, {Code: "insurance", Name: "Seguro"}}}
	pending := requirements.NotApproved([]*entities.ProviderDocument{{Type: "id_card", Status: entities.DocumentApproved}, {Type: "insurance", Status: entities.DocumentUploaded}})

	assert.Len(t, pending, 1)
	assert.Contains(t, pending, "documents.insurance")
}

func TestDocumentReviewNeedsAReasonToReject(t *testing.T) {
	document := &entities.ProviderDocument{Status: entities.DocumentUploaded}

	assert.ErrorIs(t, document.Review(false, " ", nil, providerNow), entities.ErrReviewReasonRequired)
	require.NoError(t, document.Review(false, "Ilegível", nil, providerNow))
	assert.Equal(t, entities.DocumentRejected, document.Status)
	require.NoError(t, document.Review(true, "", nil, providerNow))
	assert.Equal(t, entities.DocumentApproved, document.Status)
	assert.Empty(t, document.RejectionReason)
}
