package entities

import (
	"time"

	"github.com/google/uuid"
)

const InvitationTTL = 48 * time.Hour

type Invitation struct {
	ID           uuid.UUID
	MembershipID uuid.UUID
	TokenHash    string
	CreatedBy    *uuid.UUID
	ExpiresAt    time.Time
	AcceptedAt   *time.Time
	RevokedAt    *time.Time
	CreatedAt    time.Time
}

func NewInvitation(membershipID uuid.UUID, tokenHash string, createdBy *uuid.UUID) (*Invitation, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Invitation{
		ID:           id,
		MembershipID: membershipID,
		TokenHash:    tokenHash,
		CreatedBy:    createdBy,
		ExpiresAt:    now.Add(InvitationTTL),
		CreatedAt:    now,
	}, nil
}

func (i *Invitation) IsUsable(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}
