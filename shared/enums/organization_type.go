package enums

type OrganizationType string

const (
	OrganizationTypePlatform OrganizationType = "platform"
	OrganizationTypePartner  OrganizationType = "partner"
)

func (t OrganizationType) IsValid() bool {
	switch t {
	case OrganizationTypePlatform, OrganizationTypePartner:
		return true
	}
	return false
}

func (t OrganizationType) String() string {
	return string(t)
}
