package enums

type Role string

const (
	RoleCustomer Role = "customer"
	RoleProvider Role = "provider"
	RoleStaff    Role = "staff"
)

func (r Role) IsValid() bool {
	switch r {
	case RoleCustomer, RoleProvider, RoleStaff:
		return true
	}
	return false
}

func (r Role) IsAppAccount() bool {
	return r == RoleCustomer || r == RoleProvider
}

func (r Role) String() string {
	return string(r)
}
