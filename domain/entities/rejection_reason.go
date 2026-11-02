package entities

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type RejectionScope string

const (
	RejectionScopeDocument   RejectionScope = "document"
	RejectionScopeProvider   RejectionScope = "provider"
	RejectionScopeSuspension RejectionScope = "suspension"

	maxReasonLabelLength = 120
	maxReasonNoteLength  = 400
)

var (
	ErrUnknownRejectionReason = errors.New("a reason does not exist or does not apply here")
	reasonCodePattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

type RejectionReason struct {
	ID           uuid.UUID
	Scope        RejectionScope
	Code         string
	Label        string
	RequiresNote bool
	IsActive     bool
	DisplayOrder int
}

func (r RejectionReason) Validated() (RejectionReason, error) {
	if r.Scope != RejectionScopeDocument && r.Scope != RejectionScopeProvider && r.Scope != RejectionScopeSuspension {
		return r, validationError("scope", "O âmbito tem de ser document, provider ou suspension.")
	}
	if !reasonCodePattern.MatchString(r.Code) {
		return r, validationError("code", "O código usa letras minúsculas, números e _.")
	}
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" || len([]rune(r.Label)) > maxReasonLabelLength {
		return r, validationError("label", "O texto é obrigatório e tem no máximo 120 caracteres.")
	}
	return r, nil
}

func ComposeRejection(reasons []RejectionReason, note string) (string, error) {
	if len(reasons) == 0 {
		return "", ErrReviewReasonRequired
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > maxReasonNoteLength {
		return "", validationError("note", "A nota tem no máximo 400 caracteres.")
	}
	labels := make([]string, 0, len(reasons)+1)
	for i := range reasons {
		if reasons[i].RequiresNote && note == "" {
			return "", validationError("note", "Este motivo precisa de uma nota: "+reasons[i].Label+".")
		}
		labels = append(labels, reasons[i].Label)
	}
	if note != "" {
		labels = append(labels, note)
	}
	return strings.Join(labels, "; "), nil
}
