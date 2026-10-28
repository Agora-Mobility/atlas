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
	enums.ClientTypePartnerPanel: {enums.RolePartner},
	enums.ClientTypeBackoffice:   {enums.RoleOperator, enums.RoleAdmin},
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

func (c *Client) SignUpRole() (enums.Role, bool) {
	role, ok := signUpRoleByClientType[c.Type]
	return role, ok
}
