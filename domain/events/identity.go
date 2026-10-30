package events

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	UserAggregate     = "user"
	UserRegistered    = "identity.user-registered"
	UserPhoneVerified = "identity.phone-verified"
	UserPhoneChanged  = "identity.phone-changed"
	UserBlocked       = "identity.user-blocked"
	UserUnblocked     = "identity.user-unblocked"
	UserDeleted       = "identity.user-deleted"
)

type UserRegisteredV1 struct {
	ID        uuid.UUID  `json:"id"`
	Role      enums.Role `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
}

func (UserRegisteredV1) EventType() string     { return UserRegistered }
func (UserRegisteredV1) EventVersion() int     { return 1 }
func (UserRegisteredV1) AggregateType() string { return UserAggregate }
func (e UserRegisteredV1) AggregateID() string { return e.ID.String() }

type UserPhoneVerifiedV1 struct {
	ID         uuid.UUID `json:"id"`
	VerifiedAt time.Time `json:"verified_at"`
}

func (UserPhoneVerifiedV1) EventType() string     { return UserPhoneVerified }
func (UserPhoneVerifiedV1) EventVersion() int     { return 1 }
func (UserPhoneVerifiedV1) AggregateType() string { return UserAggregate }
func (e UserPhoneVerifiedV1) AggregateID() string { return e.ID.String() }

type UserPhoneChangedV1 struct {
	ID        uuid.UUID `json:"id"`
	ChangedAt time.Time `json:"changed_at"`
}

func (UserPhoneChangedV1) EventType() string     { return UserPhoneChanged }
func (UserPhoneChangedV1) EventVersion() int     { return 1 }
func (UserPhoneChangedV1) AggregateType() string { return UserAggregate }
func (e UserPhoneChangedV1) AggregateID() string { return e.ID.String() }

type UserBlockedV1 struct {
	ID        uuid.UUID  `json:"id"`
	Role      enums.Role `json:"role"`
	BlockedBy *uuid.UUID `json:"blocked_by,omitempty"`
	Reasons   []string   `json:"reasons"`
	Note      string     `json:"note,omitempty"`
	BlockedAt time.Time  `json:"blocked_at"`
}

func (UserBlockedV1) EventType() string     { return UserBlocked }
func (UserBlockedV1) EventVersion() int     { return 1 }
func (UserBlockedV1) AggregateType() string { return UserAggregate }
func (e UserBlockedV1) AggregateID() string { return e.ID.String() }

type UserUnblockedV1 struct {
	ID          uuid.UUID  `json:"id"`
	Role        enums.Role `json:"role"`
	UnblockedBy *uuid.UUID `json:"unblocked_by,omitempty"`
	UnblockedAt time.Time  `json:"unblocked_at"`
}

func (UserUnblockedV1) EventType() string     { return UserUnblocked }
func (UserUnblockedV1) EventVersion() int     { return 1 }
func (UserUnblockedV1) AggregateType() string { return UserAggregate }
func (e UserUnblockedV1) AggregateID() string { return e.ID.String() }

type UserDeletedV1 struct {
	ID        uuid.UUID  `json:"id"`
	Role      enums.Role `json:"role"`
	DeletedAt time.Time  `json:"deleted_at"`
}

func (UserDeletedV1) EventType() string     { return UserDeleted }
func (UserDeletedV1) EventVersion() int     { return 1 }
func (UserDeletedV1) AggregateType() string { return UserAggregate }
func (e UserDeletedV1) AggregateID() string { return e.ID.String() }
