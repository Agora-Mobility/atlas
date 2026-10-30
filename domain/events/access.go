package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	MembershipAggregate   = "membership"
	RoleAggregate         = "role"
	OrganizationAggregate = "organization"

	MemberInvited      = "access.member-invited"
	MemberActivated    = "access.member-activated"
	MemberRolesChanged = "access.member-roles-changed"
	MemberSuspended    = "access.member-suspended"
	MemberReactivated  = "access.member-reactivated"
	MemberDeactivated  = "access.member-deactivated"
	MemberMFAReset     = "access.member-mfa-reset"
	RoleCreated        = "access.role-created"
	RoleUpdated        = "access.role-updated"
	RoleDeleted        = "access.role-deleted"
	PartnerCreated     = "access.partner-created"
	PartnerUpdated     = "access.partner-updated"
)

type MemberChange struct {
	MembershipID   uuid.UUID  `json:"membership_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	UserID         uuid.UUID  `json:"user_id"`
	ChangedBy      *uuid.UUID `json:"changed_by,omitempty"`
	PermVersion    int64      `json:"perm_version"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

func (e MemberChange) AggregateID() string { return e.MembershipID.String() }

type MemberInvitedV1 struct {
	MemberChange
	Roles []string `json:"roles"`
}

func (MemberInvitedV1) EventType() string     { return MemberInvited }
func (MemberInvitedV1) EventVersion() int     { return 1 }
func (MemberInvitedV1) AggregateType() string { return MembershipAggregate }

type MemberActivatedV1 struct{ MemberChange }

func (MemberActivatedV1) EventType() string     { return MemberActivated }
func (MemberActivatedV1) EventVersion() int     { return 1 }
func (MemberActivatedV1) AggregateType() string { return MembershipAggregate }

type MemberRolesChangedV1 struct {
	MemberChange
	Before []string `json:"before"`
	After  []string `json:"after"`
}

func (MemberRolesChangedV1) EventType() string     { return MemberRolesChanged }
func (MemberRolesChangedV1) EventVersion() int     { return 1 }
func (MemberRolesChangedV1) AggregateType() string { return MembershipAggregate }

type MemberSuspendedV1 struct{ MemberChange }

func (MemberSuspendedV1) EventType() string     { return MemberSuspended }
func (MemberSuspendedV1) EventVersion() int     { return 1 }
func (MemberSuspendedV1) AggregateType() string { return MembershipAggregate }

type MemberReactivatedV1 struct{ MemberChange }

func (MemberReactivatedV1) EventType() string     { return MemberReactivated }
func (MemberReactivatedV1) EventVersion() int     { return 1 }
func (MemberReactivatedV1) AggregateType() string { return MembershipAggregate }

type MemberDeactivatedV1 struct{ MemberChange }

func (MemberDeactivatedV1) EventType() string     { return MemberDeactivated }
func (MemberDeactivatedV1) EventVersion() int     { return 1 }
func (MemberDeactivatedV1) AggregateType() string { return MembershipAggregate }

type MemberMFAResetV1 struct{ MemberChange }

func (MemberMFAResetV1) EventType() string     { return MemberMFAReset }
func (MemberMFAResetV1) EventVersion() int     { return 1 }
func (MemberMFAResetV1) AggregateType() string { return MembershipAggregate }

type RoleChange struct {
	RoleID         uuid.UUID  `json:"role_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Code           string     `json:"code"`
	Permissions    []string   `json:"permissions,omitempty"`
	ChangedBy      *uuid.UUID `json:"changed_by,omitempty"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

func (e RoleChange) AggregateID() string { return e.RoleID.String() }

type RoleCreatedV1 struct{ RoleChange }

func (RoleCreatedV1) EventType() string     { return RoleCreated }
func (RoleCreatedV1) EventVersion() int     { return 1 }
func (RoleCreatedV1) AggregateType() string { return RoleAggregate }

type RoleUpdatedV1 struct {
	RoleChange
	AffectedMemberships int `json:"affected_memberships"`
}

func (RoleUpdatedV1) EventType() string     { return RoleUpdated }
func (RoleUpdatedV1) EventVersion() int     { return 1 }
func (RoleUpdatedV1) AggregateType() string { return RoleAggregate }

type RoleDeletedV1 struct{ RoleChange }

func (RoleDeletedV1) EventType() string     { return RoleDeleted }
func (RoleDeletedV1) EventVersion() int     { return 1 }
func (RoleDeletedV1) AggregateType() string { return RoleAggregate }

type PartnerCreatedV1 struct {
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	NIF            string     `json:"nif,omitempty"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (PartnerCreatedV1) EventType() string     { return PartnerCreated }
func (PartnerCreatedV1) EventVersion() int     { return 1 }
func (PartnerCreatedV1) AggregateType() string { return OrganizationAggregate }
func (e PartnerCreatedV1) AggregateID() string { return e.OrganizationID.String() }

type PartnerUpdatedV1 struct {
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	NIF            string     `json:"nif,omitempty"`
	UpdatedBy      *uuid.UUID `json:"updated_by,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (PartnerUpdatedV1) EventType() string     { return PartnerUpdated }
func (PartnerUpdatedV1) EventVersion() int     { return 1 }
func (PartnerUpdatedV1) AggregateType() string { return OrganizationAggregate }
func (e PartnerUpdatedV1) AggregateID() string { return e.OrganizationID.String() }

func NewMemberChange(membershipID, organizationID, userID uuid.UUID, changedBy *uuid.UUID, permVersion int64) MemberChange {
	return MemberChange{
		MembershipID: membershipID, OrganizationID: organizationID, UserID: userID,
		ChangedBy: changedBy, PermVersion: permVersion, OccurredAt: time.Now().UTC(),
	}
}
