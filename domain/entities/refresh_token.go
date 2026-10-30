package entities

import (
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ClientID     uuid.UUID
	MembershipID *uuid.UUID
	FamilyID     uuid.UUID
	TokenHash    string
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	ReplacedByID *uuid.UUID
	UserAgent    string
	IPAddress    string
	CreatedAt    time.Time
}

type SessionMetadata struct {
	UserAgent string
	IPAddress string
}

func StartRefreshTokenFamily(userID, clientID uuid.UUID, membershipID *uuid.UUID, tokenHash string, ttl time.Duration, meta SessionMetadata) (*RefreshToken, error) {
	familyID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return newRefreshToken(userID, clientID, membershipID, familyID, tokenHash, ttl, meta)
}

func (t *RefreshToken) Rotate(tokenHash string, ttl time.Duration, meta SessionMetadata) (*RefreshToken, error) {
	next, err := newRefreshToken(t.UserID, t.ClientID, t.MembershipID, t.FamilyID, tokenHash, ttl, meta)
	if err != nil {
		return nil, err
	}
	t.RevokedAt = &next.CreatedAt
	t.ReplacedByID = &next.ID
	return next, nil
}

func (t *RefreshToken) BelongsTo(client *Client) bool {
	return t.ClientID == client.ID
}

func (t *RefreshToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

func (t *RefreshToken) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func newRefreshToken(userID, clientID uuid.UUID, membershipID *uuid.UUID, familyID uuid.UUID, tokenHash string, ttl time.Duration, meta SessionMetadata) (*RefreshToken, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &RefreshToken{
		ID:           id,
		UserID:       userID,
		ClientID:     clientID,
		MembershipID: membershipID,
		FamilyID:     familyID,
		TokenHash:    tokenHash,
		ExpiresAt:    now.Add(ttl),
		UserAgent:    meta.UserAgent,
		IPAddress:    meta.IPAddress,
		CreatedAt:    now,
	}, nil
}
