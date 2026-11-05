package entities_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

func TestEveryDocumentTypeHasASpec(t *testing.T) {
	for _, documentType := range enums.DocumentTypes() {
		spec, known := entities.SpecOf(documentType)
		assert.True(t, known, documentType)
		assert.NotEmpty(t, spec.Name, documentType)
		assert.NotEmpty(t, spec.Subjects, documentType)
	}
}

func TestPDFIsAcceptedOnlyWhereTheScreenTakesIt(t *testing.T) {
	idCard, _ := entities.SpecOf(enums.DocumentTypeIDCard)
	assert.False(t, idCard.Allows(enums.DocumentSlotFront, entities.ContentTypePDF))
	assert.True(t, idCard.Allows(enums.DocumentSlotRenewalReceipt, entities.ContentTypePDF))
	assert.True(t, idCard.Allows(enums.DocumentSlotFront, entities.ContentTypeHEIC))
	assert.False(t, idCard.AcceptsSlot(enums.DocumentSlotMain))

	record, _ := entities.SpecOf(enums.DocumentTypeCriminalRecord)
	assert.True(t, record.Allows(enums.DocumentSlotMain, entities.ContentTypePDF))
	assert.False(t, record.Allows(enums.DocumentSlotMain, "image/gif"))
}

func TestOnlyTheIdentityCardTakesTheVehicleOwner(t *testing.T) {
	idCard, _ := entities.SpecOf(enums.DocumentTypeIDCard)
	license, _ := entities.SpecOf(enums.DocumentTypeDrivingLicense)
	assert.True(t, idCard.AcceptsSubject(enums.DocumentSubjectVehicleOwner))
	assert.False(t, license.AcceptsSubject(enums.DocumentSubjectVehicleOwner))
}

func TestFilesAreCompleteWhenEveryRequiredSlotWasAccepted(t *testing.T) {
	document, err := entities.NewProviderDocument([16]byte{1}, enums.DocumentTypeIDCard, enums.DocumentSubjectProvider, time.Now())
	require.NoError(t, err)
	document.Files = []entities.DocumentFile{
		{Slot: enums.DocumentSlotFront, Version: 1, Status: entities.FileSuperseded},
		{Slot: enums.DocumentSlotFront, Version: 2, Status: entities.FileAccepted},
		{Slot: enums.DocumentSlotBack, Version: 1, Status: entities.FileQualityFailed},
	}
	assert.False(t, document.FilesComplete())
	assert.Equal(t, 2, document.Current(enums.DocumentSlotFront).Version)
	assert.Equal(t, entities.FileQualityFailed, document.Current(enums.DocumentSlotBack).Status)

	document.Files = append(document.Files, entities.DocumentFile{Slot: enums.DocumentSlotBack, Version: 2, Status: entities.FileAccepted})
	assert.True(t, document.FilesComplete())
}

func TestFlagsMarkADocumentForReview(t *testing.T) {
	document := &entities.ProviderDocument{}
	document.Flag("MANUAL_ENTRY")
	document.Flag("MANUAL_ENTRY")
	assert.Equal(t, []string{"MANUAL_ENTRY"}, document.ReviewFlags)
	assert.True(t, document.NeedsReview())
	document.Unflag("MANUAL_ENTRY")
	assert.False(t, document.NeedsReview())
}
