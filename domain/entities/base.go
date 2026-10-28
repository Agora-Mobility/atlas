package entities

import (
	"time"

	"github.com/google/uuid"
)

type Base struct {
	ID        uuid.UUID  `json:"id" valid:"-"`
	CreatedAt time.Time  `json:"created_at" valid:"-"`
	UpdatedAt time.Time  `json:"updated_at" valid:"-"`
	DeletedAt *time.Time `json:"deleted_at" valid:"-"`
}

func newBase() (Base, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Base{}, err
	}
	now := time.Now().UTC()
	return Base{ID: id, CreatedAt: now, UpdatedAt: now}, nil
}

func (b *Base) Touch() {
	b.UpdatedAt = time.Now().UTC()
}

func (b *Base) SoftDelete() {
	if b.DeletedAt != nil {
		return
	}
	now := time.Now().UTC()
	b.DeletedAt = &now
	b.UpdatedAt = now
}

func (b *Base) IsDeleted() bool {
	return b.DeletedAt != nil
}
