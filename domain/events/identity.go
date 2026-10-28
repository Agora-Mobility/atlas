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
