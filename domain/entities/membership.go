package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

var ErrInvalidMemberState = errors.New("the member is not in a state that allows this action")

type Membership struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	Status         enums.MembershipStatus
	PermVersion    int64
	InvitedBy      *uuid.UUID
	ActivatedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RoleIDs        []uuid.UUID
}

type Member struct {
	Membership
	Name        string
	Email       string
	Phone       string
	LastLoginAt *time.Time
}

func NewInvitedMembership(userID, organizationID uuid.UUID, invitedBy *uuid.UUID, roleIDs []uuid.UUID) (*Membership, error) {
	return newMembership(userID, organizationID, enums.MembershipStatusInvited, invitedBy, roleIDs)
}

func NewActiveMembership(userID, organizationID uuid.UUID, roleIDs []uuid.UUID) (*Membership, error) {
	m, err := newMembership(userID, organizationID, enums.MembershipStatusActive, nil, roleIDs)
	if err != nil {
		return nil, err
	}
	m.ActivatedAt = &m.CreatedAt
	return m, nil
}

func newMembership(userID, organizationID uuid.UUID, status enums.MembershipStatus, invitedBy *uuid.UUID, roleIDs []uuid.UUID) (*Membership, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Membership{
		ID:             id,
		UserID:         userID,
		OrganizationID: organizationID,
		Status:         status,
		PermVersion:    1,
		InvitedBy:      invitedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		RoleIDs:        roleIDs,
	}, nil
}

func (m *Membership) IsDeactivated() bool {
	return m.Status == enums.MembershipStatusDeactivated
}

func (m *Membership) IsActive() bool {
	return m.Status == enums.MembershipStatusActive
}

func (m *Membership) Activate() error {
	if m.Status != enums.MembershipStatusInvited {
		return ErrInvalidMemberState
	}
	now := time.Now().UTC()
	m.ActivatedAt = &now
	m.change(enums.MembershipStatusActive)
	return nil
}

func (m *Membership) Suspend() error {
	if m.Status != enums.MembershipStatusActive {
		return ErrInvalidMemberState
	}
	m.change(enums.MembershipStatusSuspended)
	return nil
}

func (m *Membership) Reactivate() error {
	if m.Status != enums.MembershipStatusSuspended {
		return ErrInvalidMemberState
	}
	m.change(enums.MembershipStatusActive)
	return nil
}

func (m *Membership) Deactivate() error {
	if m.IsDeactivated() {
		return ErrInvalidMemberState
	}
	m.change(enums.MembershipStatusDeactivated)
	return nil
}

func (m *Membership) ReplaceRoles(roleIDs []uuid.UUID) error {
	if m.IsDeactivated() {
		return ErrInvalidMemberState
	}
	m.RoleIDs = roleIDs
	m.bump()
	return nil
}

func (m *Membership) change(status enums.MembershipStatus) {
	m.Status = status
	m.bump()
}

func (m *Membership) bump() {
	m.PermVersion++
	m.UpdatedAt = time.Now().UTC()
}
