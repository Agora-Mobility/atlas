package verification_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/verification"
)

func TestFakeQualityPassesUnlessToldToFail(t *testing.T) {
	checker := verification.NewQualityChecker(verification.ModeFake, verification.FakeSettings{})
	report, err := checker.Check(context.Background(), services.ImageToCheck{Content: []byte("jpeg")})
	require.NoError(t, err)
	assert.True(t, report.Passed())
	assert.True(t, *report.Sharp)

	report, err = checker.Check(context.Background(), services.ImageToCheck{Content: []byte("jpeg FAKE:BLURRY FAKE:GLARE")})
	require.NoError(t, err)
	assert.Equal(t, []string{services.QualityGlare, services.QualityBlurry}, report.Reasons)
	assert.False(t, *report.Sharp)
	assert.True(t, *report.TextLegible)

	forced := verification.NewQualityChecker(verification.ModeFake, verification.FakeSettings{QualityReasons: []string{"shadow_on_data"}})
	report, err = forced.Check(context.Background(), services.ImageToCheck{Content: []byte("jpeg")})
	require.NoError(t, err)
	assert.Equal(t, []string{services.QualityShadowOnData}, report.Reasons)
}

func TestManualModeLeavesEverythingToTheTeam(t *testing.T) {
	report, err := verification.NewQualityChecker(verification.ModeManual, verification.FakeSettings{}).Check(context.Background(), services.ImageToCheck{Content: []byte("FAKE:BLURRY")})
	require.NoError(t, err)
	assert.True(t, report.Passed())

	_, err = verification.NewOCRProvider(verification.ModeManual, verification.FakeSettings{}).ReadIdentityCard(context.Background(), services.IdentityImages{})
	assert.ErrorIs(t, err, services.ErrExtractionUnavailable)

	result, err := verification.NewLivenessProvider(verification.ModeManual, verification.FakeSettings{}).Verify(context.Background(), services.LivenessCheck{})
	require.NoError(t, err)
	assert.Equal(t, services.LivenessUnverified, result.Outcome)
}

func TestFakeOCRAndLivenessCanBeMadeToFail(t *testing.T) {
	ocr := verification.NewOCRProvider(verification.ModeFake, verification.FakeSettings{})
	read, err := ocr.ReadIdentityCard(context.Background(), services.IdentityImages{Front: []byte("front")})
	require.NoError(t, err)
	assert.Equal(t, "005487123LA041", read.Fields["number"])
	_, err = ocr.ReadIdentityCard(context.Background(), services.IdentityImages{Front: []byte("FAKE:OCR_FAIL")})
	assert.ErrorIs(t, err, services.ErrExtractionFailed)

	liveness := verification.NewLivenessProvider(verification.ModeFake, verification.FakeSettings{})
	session, err := liveness.StartSession(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "BLINK", session.Challenge)
	passed, err := liveness.Verify(context.Background(), services.LivenessCheck{SessionID: session.ID, Image: []byte("face")})
	require.NoError(t, err)
	assert.Equal(t, services.LivenessPassed, passed.Outcome)
	failed, err := liveness.Verify(context.Background(), services.LivenessCheck{SessionID: session.ID, Image: []byte("FAKE:MULTIPLE_FACES")})
	require.NoError(t, err)
	assert.Equal(t, services.LivenessFailed, failed.Outcome)
	assert.Equal(t, services.LivenessMultipleFaces, failed.Reason)
}
