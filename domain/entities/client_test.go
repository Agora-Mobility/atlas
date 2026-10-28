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
		{enums.ClientTypeCustomerApp, []enums.Role{enums.RoleCustomer}, []enums.Role{enums.RoleProvider, enums.RoleAdmin}},
		{enums.ClientTypeProviderApp, []enums.Role{enums.RoleProvider}, []enums.Role{enums.RoleCustomer, enums.RoleOperator}},
		{enums.ClientTypePartnerPanel, []enums.Role{enums.RolePartner}, []enums.Role{enums.RoleAdmin}},
		{enums.ClientTypeBackoffice, []enums.Role{enums.RoleOperator, enums.RoleAdmin}, []enums.Role{enums.RoleCustomer, enums.RolePartner}},
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
