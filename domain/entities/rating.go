package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxRatingComment = 1000

var (
	ErrRatingNotAllowed   = errors.New("only a completed request can be rated")
	ErrAlreadyRated       = errors.New("this request was already rated")
	ErrComplimentNotValid = errors.New("a compliment does not exist or does not apply to this service")
	ErrTipNotAllowed      = errors.New("a tip needs a completed request with a provider")
	ErrTipOutOfRange      = errors.New("the tip is outside the allowed values")
	ErrTipAlreadyGiven    = errors.New("this request already has a tip")
)

type Rating struct {
	ID            uuid.UUID
	RequestID     *uuid.UUID
	BookingID     *uuid.UUID
	CustomerID    uuid.UUID
	ProviderID    *uuid.UUID
	WorkshopID    *uuid.UUID
	Stars         int
	ComplimentIDs []uuid.UUID
	Comment       string
	CreatedAt     time.Time
}

func (r *Rating) Normalize() error {
	if r.Stars < 1 || r.Stars > 5 {
		return validationError("stars", "A avaliação tem de ser de 1 a 5 estrelas.")
	}
	r.Comment = strings.TrimSpace(r.Comment)
	if len([]rune(r.Comment)) > maxRatingComment {
		return validationError("comment", "O comentário tem no máximo 1000 caracteres.")
	}
	return nil
}

func AverageRating(sum int64, count int) float64 {
	if count == 0 {
		return 0
	}
	return float64(sum*10/int64(count)) / 10
}
