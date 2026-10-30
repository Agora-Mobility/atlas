package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

func TestClientAllowsOnlyItsRoles(t *testing.T) {
	tests := []struct {
		clientType enums.ClientType
		allowed    []enums.Role
		denied     []enums.Role
	}{
		{enums.ClientTypeCustomerApp, []enums.Role{enums.RoleCustomer}, []enums.Role{enums.RoleProvider, enums.RoleStaff}},
		{enums.ClientTypeProviderApp, []enums.Role{enums.RoleProvider}, []enums.Role{enums.RoleCustomer, enums.RoleStaff}},
		{enums.ClientTypePartnerPanel, []enums.Role{enums.RoleStaff}, []enums.Role{enums.RoleCustomer, enums.RoleProvider}},
		{enums.ClientTypeBackoffice, []enums.Role{enums.RoleStaff}, []enums.Role{enums.RoleCustomer, enums.RoleProvider}},
	}
	for _, tt := range tests {
		t.Run(tt.clientType.String(), func(t *testing.T) {
			client := &entities.Client{Type: tt.clientType}
			for _, role := range tt.allowed {
				assert.True(t, client.Allows(role), role)
			}
			for _, role := range tt.denied {
				assert.False(t, client.Allows(role), role)
			}
		})
	}
}

func TestClientSignUpRoleComesFromClientType(t *testing.T) {
	role, ok := (&entities.Client{Type: enums.ClientTypeCustomerApp}).SignUpRole()
	assert.True(t, ok)
	assert.Equal(t, enums.RoleCustomer, role)

	role, ok = (&entities.Client{Type: enums.ClientTypeProviderApp}).SignUpRole()
	assert.True(t, ok)
	assert.Equal(t, enums.RoleProvider, role)

	for _, staff := range []enums.ClientType{enums.ClientTypePartnerPanel, enums.ClientTypeBackoffice} {
		_, ok := (&entities.Client{Type: staff}).SignUpRole()
		assert.False(t, ok, staff)
	}
}

func TestClientStaffScope(t *testing.T) {
	scope, ok := (&entities.Client{Type: enums.ClientTypeBackoffice}).StaffScope()
	assert.True(t, ok)
	assert.Equal(t, enums.OrganizationTypePlatform, scope)

	scope, ok = (&entities.Client{Type: enums.ClientTypePartnerPanel}).StaffScope()
	assert.True(t, ok)
	assert.Equal(t, enums.OrganizationTypePartner, scope)

	_, ok = (&entities.Client{Type: enums.ClientTypeCustomerApp}).StaffScope()
	assert.False(t, ok)
}
