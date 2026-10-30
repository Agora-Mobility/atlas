package enums

type MembershipStatus string

const (
	MembershipStatusInvited     MembershipStatus = "invited"
	MembershipStatusActive      MembershipStatus = "active"
	MembershipStatusSuspended   MembershipStatus = "suspended"
	MembershipStatusDeactivated MembershipStatus = "deactivated"
)

func (s MembershipStatus) IsValid() bool {
	switch s {
	case MembershipStatusInvited, MembershipStatusActive, MembershipStatusSuspended, MembershipStatusDeactivated:
		return true
	}
	return false
}

func (s MembershipStatus) String() string {
	return string(s)
}
