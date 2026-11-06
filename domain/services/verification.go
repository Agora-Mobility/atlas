package services

import (
	"context"
	"errors"
	"time"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	QualityTextNotLegible   = "TEXT_NOT_LEGIBLE"
	QualityShadowOnData     = "SHADOW_ON_DATA"
	QualityCornersNotSeen   = "CORNERS_NOT_VISIBLE"
	QualityGlare            = "GLARE"
	QualityBlurry           = "BLURRY"
	QualityTooDark          = "TOO_DARK"
	LivenessNoFace          = "NO_FACE"
	LivenessMultipleFaces   = "MULTIPLE_FACES"
	LivenessChallengeNotMet = "CHALLENGE_NOT_MET"
	LivenessSessionExpired  = "SESSION_EXPIRED"
)

var (
	ErrExtractionUnavailable = errors.New("document reading is not available")
	ErrExtractionFailed      = errors.New("the document could not be read")
)

type ImageToCheck struct {
	DocumentType enums.DocumentType
	Slot         enums.DocumentSlot
	ContentType  string
	Content      []byte
}

type QualityReport struct {
	TextLegible    *bool
	NoShadowOnData *bool
	CornersVisible *bool
	NoGlare        *bool
	Sharp          *bool
	WellLit        *bool
	Reasons        []string
}

func (r *QualityReport) Passed() bool {
	return len(r.Reasons) == 0
}

type IImageQualityChecker interface {
	Check(ctx context.Context, image ImageToCheck) (*QualityReport, error)
}

type IdentityImages struct {
	Front            []byte
	FrontContentType string
	Back             []byte
	BackContentType  string
}

type ExtractedIdentity struct {
	Fields     map[string]string
	Confidence map[string]float64
}

type IDocumentOCRProvider interface {
	ReadIdentityCard(ctx context.Context, images IdentityImages) (*ExtractedIdentity, error)
}

type LivenessSession struct {
	ID        string
	Challenge string
	ExpiresAt time.Time
}

type LivenessOutcome string

const (
	LivenessPassed     LivenessOutcome = "passed"
	LivenessFailed     LivenessOutcome = "failed"
	LivenessUnverified LivenessOutcome = "unverified"
)

type LivenessCheck struct {
	SessionID   string
	ContentType string
	Image       []byte
}

type LivenessResult struct {
	Outcome LivenessOutcome
	Reason  string
}

type ILivenessProvider interface {
	StartSession(ctx context.Context) (*LivenessSession, error)
	Verify(ctx context.Context, check LivenessCheck) (*LivenessResult, error)
}
