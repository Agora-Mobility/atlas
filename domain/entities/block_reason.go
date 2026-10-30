package entities

import (
	"time"

	"github.com/google/uuid"
)

type BlockReason struct {
	ID        uuid.UUID
	Code      string
	Label     string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserBlock struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	BlockedBy *uuid.UUID
	Reasons   []*BlockReason
	Note      string
	BlockedAt time.Time
}

func NewUserBlock(userID uuid.UUID, blockedBy *uuid.UUID, reasons []*BlockReason, note string, blockedAt time.Time) (*UserBlock, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &UserBlock{
		ID:        id,
		UserID:    userID,
		BlockedBy: blockedBy,
		Reasons:   reasons,
		Note:      note,
		BlockedAt: blockedAt,
	}, nil
}

func (b *UserBlock) ReasonIDs() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(b.Reasons))
	for _, r := range b.Reasons {
		ids = append(ids, r.ID)
	}
	return ids
}

func (b *UserBlock) ReasonCodes() []string {
	codes := make([]string, 0, len(b.Reasons))
	for _, r := range b.Reasons {
		codes = append(codes, r.Code)
	}
	return codes
}
