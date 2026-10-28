package enums

// Role is the access role carried in the JWT.
type Role string

const (
	RoleCustomer Role = "customer"
	RoleProvider Role = "provider"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
	RolePartner  Role = "partner"
)

func (r Role) IsValid() bool {
	switch r {
	case RoleCustomer, RoleProvider, RoleOperator, RoleAdmin, RolePartner:
		return true
	}
	return false
}

func (r Role) String() string {
	return string(r)
}
