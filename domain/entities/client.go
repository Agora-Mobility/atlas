package entities

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

var rolesByClientType = map[enums.ClientType][]enums.Role{
	enums.ClientTypeCustomerApp:  {enums.RoleCustomer},
	enums.ClientTypeProviderApp:  {enums.RoleProvider},
	enums.ClientTypePartnerPanel: {enums.RoleStaff},
	enums.ClientTypeBackoffice:   {enums.RoleStaff},
}

var staffScopeByClientType = map[enums.ClientType]enums.OrganizationType{
	enums.ClientTypeBackoffice:   enums.OrganizationTypePlatform,
	enums.ClientTypePartnerPanel: enums.OrganizationTypePartner,
}

var signUpRoleByClientType = map[enums.ClientType]enums.Role{
	enums.ClientTypeCustomerApp: enums.RoleCustomer,
	enums.ClientTypeProviderApp: enums.RoleProvider,
}

type Client struct {
	ID        uuid.UUID
	ClientID  string
	Name      string
	Type      enums.ClientType
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Client) Allows(role enums.Role) bool {
	return slices.Contains(rolesByClientType[c.Type], role)
}

func (c *Client) StaffScope() (enums.OrganizationType, bool) {
	scope, ok := staffScopeByClientType[c.Type]
	return scope, ok
}

func (c *Client) SignUpRole() (enums.Role, bool) {
	role, ok := signUpRoleByClientType[c.Type]
	return role, ok
}
