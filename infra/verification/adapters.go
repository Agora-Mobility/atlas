package verification

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	ModeFake   = "fake"
	ModeManual = "manual"

	markerPrefix     = "FAKE:"
	markerOCRFail    = "FAKE:OCR_FAIL"
	markerLiveness   = "FAKE:LIVENESS_FAIL"
	livenessTTL      = 10 * time.Minute
	challengeBlink   = "BLINK"
	fakeConfidence   = 0.98
	maxMarkerReasons = 6
)

type FakeSettings struct {
	QualityReasons []string
	FailOCR        bool
	FailLiveness   bool
}

func markers(content []byte) []string {
	found := []string{}
	for _, part := range bytes.Split(content, []byte(markerPrefix))[1:] {
		end := bytes.IndexFunc(part, func(r rune) bool { return (r < 'A' || r > 'Z') && r != '_' })
		if end < 0 {
			end = len(part)
		}
		found = append(found, markerPrefix+string(part[:end]))
		if len(found) == maxMarkerReasons {
			break
		}
	}
	return found
}

type QualityChecker struct {
	settings FakeSettings
}

func NewQualityChecker(mode string, settings FakeSettings) services.IImageQualityChecker {
	if mode == ModeFake {
		return &QualityChecker{settings: settings}
	}
	return ManualQuality{}
}

var qualityReasons = []string{services.QualityTextNotLegible, services.QualityShadowOnData, services.QualityCornersNotSeen, services.QualityGlare, services.QualityBlurry, services.QualityTooDark}

func (c *QualityChecker) Check(_ context.Context, image services.ImageToCheck) (*services.QualityReport, error) {
	failed := map[string]bool{}
	for _, reason := range c.settings.QualityReasons {
		failed[strings.ToUpper(strings.TrimSpace(reason))] = true
	}
	for _, marker := range markers(image.Content) {
		failed[strings.TrimPrefix(marker, markerPrefix)] = true
	}
	verdict := func(reason string) *bool {
		passed := !failed[reason]
		return &passed
	}
	report := &services.QualityReport{
		TextLegible: verdict(services.QualityTextNotLegible), NoShadowOnData: verdict(services.QualityShadowOnData), CornersVisible: verdict(services.QualityCornersNotSeen),
		NoGlare: verdict(services.QualityGlare), Sharp: verdict(services.QualityBlurry), WellLit: verdict(services.QualityTooDark), Reasons: []string{},
	}
	for _, reason := range qualityReasons {
		if failed[reason] {
			report.Reasons = append(report.Reasons, reason)
		}
	}
	return report, nil
}

type ManualQuality struct{}

func (ManualQuality) Check(context.Context, services.ImageToCheck) (*services.QualityReport, error) {
	return &services.QualityReport{Reasons: []string{}}, nil
}

type OCRProvider struct {
	settings FakeSettings
}

func NewOCRProvider(mode string, settings FakeSettings) services.IDocumentOCRProvider {
	if mode == ModeFake {
		return &OCRProvider{settings: settings}
	}
	return ManualOCR{}
}

func (p *OCRProvider) ReadIdentityCard(_ context.Context, images services.IdentityImages) (*services.ExtractedIdentity, error) {
	if p.settings.FailOCR || bytes.Contains(images.Front, []byte(markerOCRFail)) {
		return nil, services.ErrExtractionFailed
	}
	fields := map[string]string{
		"full_name": "Domingos Mateus Kiala", "number": "005487123LA041", "birth_date": "1988-03-14", "issued_at": "2022-01-10", "expires_at": "2032-01-10",
	}
	confidence := make(map[string]float64, len(fields))
	for field := range fields {
		confidence[field] = fakeConfidence
	}
	return &services.ExtractedIdentity{Fields: fields, Confidence: confidence}, nil
}

type ManualOCR struct{}

func (ManualOCR) ReadIdentityCard(context.Context, services.IdentityImages) (*services.ExtractedIdentity, error) {
	return nil, services.ErrExtractionUnavailable
}

type LivenessProvider struct {
	settings FakeSettings
	now      func() time.Time
}

func NewLivenessProvider(mode string, settings FakeSettings) services.ILivenessProvider {
	if mode == ModeFake {
		return &LivenessProvider{settings: settings, now: time.Now}
	}
	return &ManualLiveness{now: time.Now}
}

func newSession(now time.Time) (*services.LivenessSession, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &services.LivenessSession{ID: id.String(), Challenge: challengeBlink, ExpiresAt: now.Add(livenessTTL)}, nil
}

func (p *LivenessProvider) StartSession(context.Context) (*services.LivenessSession, error) {
	return newSession(p.now())
}

func (p *LivenessProvider) Verify(_ context.Context, check services.LivenessCheck) (*services.LivenessResult, error) {
	if p.settings.FailLiveness || bytes.Contains(check.Image, []byte(markerLiveness)) {
		return &services.LivenessResult{Outcome: services.LivenessFailed, Reason: services.LivenessChallengeNotMet}, nil
	}
	if bytes.Contains(check.Image, []byte(markerPrefix+services.LivenessMultipleFaces)) {
		return &services.LivenessResult{Outcome: services.LivenessFailed, Reason: services.LivenessMultipleFaces}, nil
	}
	return &services.LivenessResult{Outcome: services.LivenessPassed}, nil
}

type ManualLiveness struct {
	now func() time.Time
}

func (m *ManualLiveness) StartSession(context.Context) (*services.LivenessSession, error) {
	return newSession(m.now())
}

func (*ManualLiveness) Verify(context.Context, services.LivenessCheck) (*services.LivenessResult, error) {
	return &services.LivenessResult{Outcome: services.LivenessUnverified}, nil
}
