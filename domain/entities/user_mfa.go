package entities

import (
	"time"

	"github.com/google/uuid"
)

type UserMFA struct {
	UserID       uuid.UUID
	Secret       string
	ConfirmedAt  *time.Time
	LastUsedStep int64
}

func (m *UserMFA) IsConfirmed() bool {
	return m != nil && m.ConfirmedAt != nil
}
