package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
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

	service := uuid.New()
	valid := entities.PersonalData{BirthDate: providerNow.AddDate(-30, 0, 0), IDNumber: " 005487123 la041 ", Province: " Luanda ", Municipality: "Talatona"}
	with := func(change func(*entities.PersonalData)) entities.PersonalData {
		data := valid
		change(&data)
		return data
	}
	assert.Error(t, profile.SetProfile(service, with(func(d *entities.PersonalData) { d.BirthDate = providerNow.AddDate(-17, 0, 0) }), providerNow), "under 18")
	for _, idNumber := range []string{" ", "007", "005487123LA04", "0054871234LA041", "00548712LAA041"} {
		assert.Error(t, profile.SetProfile(service, with(func(d *entities.PersonalData) { d.IDNumber = idNumber }), providerNow), idNumber)
	}
	assert.Error(t, profile.SetProfile(service, with(func(d *entities.PersonalData) { d.Province = " " }), providerNow))
	assert.Error(t, profile.SetProfile(service, with(func(d *entities.PersonalData) { d.Municipality = "" }), providerNow))
	assert.Nil(t, profile.RequestedServiceID, "a refused profile changes nothing")

	require.NoError(t, profile.SetProfile(service, valid, providerNow))
	assert.Equal(t, service, *profile.RequestedServiceID)
	assert.Equal(t, "005487123LA041", profile.IDNumber)
	assert.Equal(t, "Luanda", profile.Province)
	assert.Equal(t, "Talatona", profile.Municipality)

	other := uuid.New()
	require.NoError(t, profile.SetProfile(other, valid, providerNow))
	assert.Equal(t, other, *profile.RequestedServiceID, "a single service: choosing again replaces it")

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

func specs(types ...enums.DocumentType) []entities.DocumentSpec {
	out := make([]entities.DocumentSpec, 0, len(types))
	for _, documentType := range types {
		spec, _ := entities.SpecOf(documentType)
		out = append(out, spec)
	}
	return out
}

func sentDocument(documentType enums.DocumentType, status entities.DocumentStatus) *entities.ProviderDocument {
	return &entities.ProviderDocument{
		Type: documentType, Subject: enums.DocumentSubjectProvider, Status: status,
		Files: []entities.DocumentFile{{Slot: enums.DocumentSlotFront, Status: entities.FileAccepted}},
	}
}

func TestMissingListsEverythingRequiredByTheService(t *testing.T) {
	requirements := &entities.ProviderRequirements{
		RequiresVehicle: true, MinVehicleSeats: 7,
		Documents: specs(enums.DocumentTypeIDCard, enums.DocumentTypeVehicleInsurance, enums.DocumentTypeVehicleInspection),
	}
	profile := &entities.Provider{Status: entities.ProviderDraft, Vehicle: validVehicle()}
	profile.Vehicle.Plate = "LD"
	rejected := sentDocument(enums.DocumentTypeVehicleInsurance, entities.DocumentRejected)
	rejected.RejectionReason = "Caducado"
	documents := []*entities.ProviderDocument{sentDocument(enums.DocumentTypeIDCard, entities.DocumentUploaded), rejected}

	missing := requirements.MissingFor(profile, "", documents)

	assert.Contains(t, missing, "personal.name")
	assert.Contains(t, missing, "personal.birth_date")
	assert.Contains(t, missing, "personal.province")
	assert.Contains(t, missing, "personal.municipality")
	assert.Contains(t, missing, "vehicle.seats", "the XL rule is data")
	assert.Contains(t, missing, "documents.vehicle_inspection")
	assert.Contains(t, missing["documents.vehicle_insurance"], "Caducado")
	assert.NotContains(t, missing, "documents.id_card")
}

func TestADocumentWithoutAnyFileIsStillMissing(t *testing.T) {
	requirements := &entities.ProviderRequirements{Documents: specs(enums.DocumentTypeIDCard)}
	empty := &entities.ProviderDocument{Type: enums.DocumentTypeIDCard, Subject: enums.DocumentSubjectProvider, Status: entities.DocumentUploaded}

	missing := requirements.MissingFor(&entities.Provider{}, "Pedro", []*entities.ProviderDocument{empty})

	assert.Contains(t, missing, "documents.id_card")
}

func TestApprovalNeedsEveryRequiredDocumentApproved(t *testing.T) {
	requirements := &entities.ProviderRequirements{Documents: specs(enums.DocumentTypeIDCard, enums.DocumentTypeVehicleInsurance)}
	pending := requirements.NotApproved([]*entities.ProviderDocument{
		sentDocument(enums.DocumentTypeIDCard, entities.DocumentApproved), sentDocument(enums.DocumentTypeVehicleInsurance, entities.DocumentUploaded),
	})

	assert.Len(t, pending, 1)
	assert.Contains(t, pending, "documents.vehicle_insurance")
}

func TestApprovalLooksOnlyAtTheDocumentsOfTheProvider(t *testing.T) {
	requirements := &entities.ProviderRequirements{Documents: specs(enums.DocumentTypeIDCard)}
	owner := sentDocument(enums.DocumentTypeIDCard, entities.DocumentApproved)
	owner.Subject = enums.DocumentSubjectVehicleOwner

	assert.Contains(t, requirements.NotApproved([]*entities.ProviderDocument{owner}), "documents.id_card")
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
