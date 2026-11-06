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

func accepted(slots ...enums.DocumentSlot) []entities.DocumentFile {
	files := make([]entities.DocumentFile, 0, len(slots))
	for _, slot := range slots {
		files = append(files, entities.DocumentFile{Slot: slot, Version: 1, Status: entities.FileAccepted})
	}
	return files
}

func document(documentType enums.DocumentType, subject enums.DocumentSubject, files []entities.DocumentFile) *entities.ProviderDocument {
	return &entities.ProviderDocument{Type: documentType, Subject: subject, Status: entities.DocumentUploaded, Files: files}
}

func confirmedIDCard(subject enums.DocumentSubject) *entities.ProviderDocument {
	card := document(enums.DocumentTypeIDCard, subject, accepted(enums.DocumentSlotFront, enums.DocumentSlotBack))
	card.Number, card.Details.Source = "005487123LA041", entities.SourceOCR
	return card
}

func verifiedSelfie(subject enums.DocumentSubject, status string) *entities.ProviderDocument {
	selfie := document(enums.DocumentTypeSelfie, subject, accepted(enums.DocumentSlotMain))
	selfie.Details.Liveness = &entities.Liveness{Status: status, Attempts: 1}
	return selfie
}

func TestADriverWithOnlyTheIDCardIsAtTwelvePercent(t *testing.T) {
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Provider: &entities.Provider{}, Documents: []*entities.ProviderDocument{confirmedIDCard(enums.DocumentSubjectProvider)}}

	progress := snapshot.Progress()

	assert.Equal(t, 1, progress.Completed)
	assert.Equal(t, 8, progress.Total)
	assert.Equal(t, 12, progress.Percent)
	assert.Equal(t, 10, progress.MinutesLeft)
	require.NotNil(t, progress.Next)
	assert.Equal(t, enums.VerificationItemSelfie, *progress.Next)
	require.Len(t, progress.Groups, 3)
	assert.Equal(t, enums.VerificationGroupAboutYou, progress.Groups[0].Key)
	assert.Equal(t, 1, progress.Groups[0].Completed)
	assert.Equal(t, 4, progress.Groups[0].Total)
	assert.Equal(t, 4, progress.Groups[0].Minutes)
	assert.Equal(t, entities.ItemCompleted, progress.Groups[0].Items[0].Status)
	assert.Equal(t, entities.ItemNotStarted, progress.Groups[0].Items[1].Status)
}

func TestAWorkshopHasEightItemsInFourGroups(t *testing.T) {
	progress := (&entities.VerificationSnapshot{Kind: enums.VerificationKindWorkshop}).Progress()

	assert.Equal(t, 8, progress.Total)
	assert.Len(t, progress.Groups, 4)
	assert.Equal(t, 0, progress.Percent)
	assert.Equal(t, enums.VerificationItemBusinessLicense, *progress.Next)
}

func TestFilesWithoutConfirmedDataAreInProgress(t *testing.T) {
	card := document(enums.DocumentTypeIDCard, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotFront))
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{card}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemIdentityCard))

	card.Files = accepted(enums.DocumentSlotFront, enums.DocumentSlotBack)
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemIdentityCard), "both sides but the data is not confirmed")
}

func TestManualDataNeedsReviewButCountsAsDone(t *testing.T) {
	card := confirmedIDCard(enums.DocumentSubjectProvider)
	card.Flag(entities.FlagManualEntry)
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{card}}

	assert.Equal(t, entities.ItemNeedsReview, snapshot.ItemStatus(enums.VerificationItemIdentityCard))
	assert.Equal(t, 1, snapshot.Progress().Completed)
}

func TestARejectedDocumentGoesBackToInProgress(t *testing.T) {
	card := confirmedIDCard(enums.DocumentSubjectProvider)
	card.Status = entities.DocumentRejected
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{card}}

	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemIdentityCard))
}

func TestSelfieThatFailedThreeTimesIsSubmittedForReview(t *testing.T) {
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver}
	failing := verifiedSelfie(enums.DocumentSubjectProvider, entities.LivenessFailed)
	snapshot.Documents = []*entities.ProviderDocument{failing}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemSelfie))

	review := verifiedSelfie(enums.DocumentSubjectProvider, entities.LivenessNeedsReview)
	review.Flag(entities.FlagLivenessNotVerified)
	snapshot.Documents = []*entities.ProviderDocument{review}
	assert.Equal(t, entities.ItemNeedsReview, snapshot.ItemStatus(enums.VerificationItemSelfie))
}

func TestVehicleNeedsTheDataAndTheFivePhotos(t *testing.T) {
	provider := &entities.Provider{Vehicle: entities.Vehicle{Plate: "LD-45-21-AF"}}
	photos := document(enums.DocumentTypeVehiclePhoto, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotFront, enums.DocumentSlotRear, enums.DocumentSlotSide, enums.DocumentSlotInterior))
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Provider: provider, Documents: []*entities.ProviderDocument{photos}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemVehicle))

	photos.Files = append(photos.Files, accepted(enums.DocumentSlotPlate)...)
	assert.Equal(t, entities.ItemCompleted, snapshot.ItemStatus(enums.VerificationItemVehicle))
}

func TestAVehicleOfSomeoneElseNeedsTheDeclarationAndTheOwnerIDCard(t *testing.T) {
	notProvider := false
	registration := document(enums.DocumentTypeVehicleRegistration, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotPage1, enums.DocumentSlotPage2))
	registration.Details.OwnerName, registration.Details.OwnerIsProvider = "Maria Kiala", &notProvider
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{registration}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemVehicleRegistration))

	snapshot.Documents = append(snapshot.Documents,
		document(enums.DocumentTypeVehicleOwnerDeclaration, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotMain)),
		document(enums.DocumentTypeIDCard, enums.DocumentSubjectVehicleOwner, accepted(enums.DocumentSlotFront, enums.DocumentSlotBack)),
	)
	assert.Equal(t, entities.ItemNeedsReview, snapshot.ItemStatus(enums.VerificationItemVehicleRegistration))
}

func TestInsuranceItemNeedsBothCertificates(t *testing.T) {
	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	insurer := uuid.New()
	insurance := document(enums.DocumentTypeVehicleInsurance, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotMain))
	insurance.ExpiresAt, insurance.Details.InsurerID = &expires, &insurer
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{insurance}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemVehicleInsurance))

	inspection := document(enums.DocumentTypeVehicleInspection, enums.DocumentSubjectProvider, accepted(enums.DocumentSlotMain))
	inspection.ExpiresAt = &expires
	snapshot.Documents = append(snapshot.Documents, inspection)
	assert.Equal(t, entities.ItemCompleted, snapshot.ItemStatus(enums.VerificationItemVehicleInsurance))
}

func TestRepresentativeNeedsTheIDCardAndTheSelfie(t *testing.T) {
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindWorkshop, Documents: []*entities.ProviderDocument{confirmedIDCard(enums.DocumentSubjectLegalRepresentative)}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemLegalRepresentative))

	snapshot.Documents = append(snapshot.Documents, verifiedSelfie(enums.DocumentSubjectLegalRepresentative, entities.LivenessAccepted))
	assert.Equal(t, entities.ItemCompleted, snapshot.ItemStatus(enums.VerificationItemLegalRepresentative))
	assert.Equal(t, entities.ItemNotStarted, snapshot.ItemStatus(enums.VerificationItemIdentityCard), "the provider's own card is another item")
}

func TestWorkshopLocationNeedsAnOpenDay(t *testing.T) {
	lat, lng := -8.83, 13.23
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindWorkshop, Workshop: entities.WorkshopDraft{Address: "Rua 1", Lat: &lat, Lng: &lng, Days: []entities.WorkshopDay{{Weekday: 0, Closed: true}}}}
	assert.Equal(t, entities.ItemInProgress, snapshot.ItemStatus(enums.VerificationItemWorkshopLocation))

	snapshot.Workshop.Days = append(snapshot.Workshop.Days, entities.WorkshopDay{Weekday: 1, Open: "08:00", Close: "18:00"})
	assert.Equal(t, entities.ItemCompleted, snapshot.ItemStatus(enums.VerificationItemWorkshopLocation))
}

func TestOpeningHoursMustOpenBeforeTheyClose(t *testing.T) {
	_, _, err := entities.WorkshopDay{Weekday: 1, Open: "18:00", Close: "08:00"}.Hours()
	assert.Error(t, err)
	_, _, err = entities.WorkshopDay{Weekday: 7, Closed: true}.Hours()
	assert.Error(t, err)
	_, _, err = entities.WorkshopDay{Weekday: 1, Open: "8h", Close: "18:00"}.Hours()
	assert.Error(t, err)
	hours, open, err := entities.WorkshopDay{Weekday: 6, Open: "08:30", Close: "13:00"}.Hours()
	require.NoError(t, err)
	assert.True(t, open)
	assert.Equal(t, 510, hours.OpenMinute)
	assert.Equal(t, 780, hours.CloseMinute)
}

func TestMissingListsTheItemsInPlanOrder(t *testing.T) {
	snapshot := &entities.VerificationSnapshot{Kind: enums.VerificationKindDriver, Documents: []*entities.ProviderDocument{confirmedIDCard(enums.DocumentSubjectProvider)}}
	missing := snapshot.Missing()
	assert.Len(t, missing, 7)
	assert.Equal(t, enums.VerificationItemSelfie, missing[0])
	assert.Equal(t, enums.VerificationItemBankAccount, missing[6])
}

func TestVerificationStatusFollowsTheProvider(t *testing.T) {
	submitted := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	pending := &entities.Provider{Status: entities.ProviderPending, SubmittedAt: &submitted}
	assert.Equal(t, entities.VerificationSubmitted, entities.VerificationStatusOf(pending, nil))

	reviewed := submitted.Add(time.Hour)
	assert.Equal(t, entities.VerificationInReview, entities.VerificationStatusOf(pending, []*entities.ProviderDocument{{ReviewedAt: &reviewed}}))
	assert.Equal(t, entities.VerificationInProgress, entities.VerificationStatusOf(&entities.Provider{Status: entities.ProviderDraft}, nil))
	assert.Equal(t, entities.VerificationNeedsChanges, entities.VerificationStatusOf(&entities.Provider{Status: entities.ProviderRejected}, nil))
	assert.Equal(t, entities.VerificationApproved, entities.VerificationStatusOf(&entities.Provider{Status: entities.ProviderApproved}, nil))
}

func TestEachKindSendsOnlyItsOwnDocuments(t *testing.T) {
	assert.True(t, entities.AcceptsDocument(enums.VerificationKindDriver, enums.DocumentTypeIDCard, enums.DocumentSubjectVehicleOwner))
	assert.False(t, entities.AcceptsDocument(enums.VerificationKindDriver, enums.DocumentTypeWorkshopPhoto, enums.DocumentSubjectProvider))
	assert.False(t, entities.AcceptsDocument(enums.VerificationKindWorkshop, enums.DocumentTypeIDCard, enums.DocumentSubjectProvider), "a workshop sends the card of its representative")
	assert.True(t, entities.AcceptsDocument(enums.VerificationKindWorkshop, enums.DocumentTypeSelfie, enums.DocumentSubjectLegalRepresentative))
	assert.Equal(t, enums.DocumentSubjectLegalRepresentative, entities.PersonSubject(enums.VerificationKindWorkshop))
}
