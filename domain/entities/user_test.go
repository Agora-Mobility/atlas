package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const hash = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"

func TestNewUser(t *testing.T) {
	tests := []struct {
		name         string
		userName     string
		email, phone string
		password     string
		role         enums.Role
		wantFields   []string
	}{
		{name: "valid with phone", userName: "Ana", phone: "+244923000000", password: hash, role: enums.RoleCustomer},
		{name: "valid with email", userName: "Rui", email: " Rui@Agora.AO ", password: hash, role: enums.RoleAdmin},
		{name: "blank name", userName: " ", phone: "+244923000000", password: hash, role: enums.RoleCustomer, wantFields: []string{"name"}},
		{name: "invalid email", userName: "Ana", email: "nope", password: hash, role: enums.RoleOperator, wantFields: []string{"email"}},
		{name: "national phone", userName: "Ana", phone: " 923 000 000 ", password: hash, role: enums.RoleProvider},
		{name: "invalid phone", userName: "Ana", phone: "12345", password: hash, role: enums.RoleProvider, wantFields: []string{"phone"}},
		{name: "missing password", userName: "Ana", phone: "+244923000000", role: enums.RolePartner, wantFields: []string{"password"}},
		{name: "unknown role", userName: "Ana", phone: "+244923000000", password: hash, role: "root", wantFields: []string{"role"}},
		{name: "no email nor phone", userName: "Ana", password: hash, role: enums.RoleCustomer, wantFields: []string{"email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := entities.NewUser(tt.userName, tt.email, tt.phone, tt.password, tt.role)

			if len(tt.wantFields) == 0 {
				require.NoError(t, err)
				assert.NotEqual(t, [16]byte{}, [16]byte(u.ID))
				assert.Equal(t, enums.UserStatusActive, u.Status)
				assert.False(t, u.IsVerified)
				assert.True(t, u.CanLogin())
				if tt.email != "" {
					assert.Equal(t, "rui@agora.ao", u.Email)
				}
				if tt.phone != "" {
					assert.Equal(t, "+244923000000", u.Phone)
				}
				return
			}
			validationErr, ok := customerrors.AsValidation(err)
			require.True(t, ok, "err: %v", err)
			for _, f := range tt.wantFields {
				assert.Contains(t, validationErr.Fields, f)
			}
		})
	}
}

func TestUserLifecycle(t *testing.T) {
	u, err := entities.NewUser("Ana", "", "+244923000000", hash, enums.RoleCustomer)
	require.NoError(t, err)

	u.MarkVerified()
	u.RecordLogin()
	assert.True(t, u.IsVerified)
	assert.NotNil(t, u.LastLoginAt)

	u.Block()
	assert.False(t, u.CanLogin())
	u.Activate()
	assert.True(t, u.CanLogin())

	require.Error(t, u.UpdateProfile("", "", ""))
	assert.Equal(t, "Ana", u.Name, "a failed update must not change the user")

	u.SoftDelete()
	assert.True(t, u.IsDeleted())
	assert.False(t, u.CanLogin())
}
