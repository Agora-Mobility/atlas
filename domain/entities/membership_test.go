package entities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

func TestMembershipLifecycle(t *testing.T) {
	m, err := entities.NewInvitedMembership(uuid.New(), uuid.New(), nil, []uuid.UUID{uuid.New()})
	require.NoError(t, err)
	assert.Equal(t, enums.MembershipStatusInvited, m.Status)

	assert.ErrorIs(t, m.Suspend(), entities.ErrInvalidMemberState, "an invited member cannot be suspended")
	assert.ErrorIs(t, m.Reactivate(), entities.ErrInvalidMemberState)

	require.NoError(t, m.Activate())
	assert.NotNil(t, m.ActivatedAt)
	assert.ErrorIs(t, m.Activate(), entities.ErrInvalidMemberState)

	require.NoError(t, m.Suspend())
	require.NoError(t, m.Reactivate())
	assert.ErrorIs(t, m.Reactivate(), entities.ErrInvalidMemberState, "reactivating an active member is invalid")

	require.NoError(t, m.Deactivate())
	for name, action := range map[string]func() error{
		"activate":   m.Activate,
		"suspend":    m.Suspend,
		"reactivate": m.Reactivate,
		"deactivate": m.Deactivate,
		"roles":      func() error { return m.ReplaceRoles([]uuid.UUID{uuid.New()}) },
	} {
		assert.ErrorIs(t, action(), entities.ErrInvalidMemberState, "deactivated is final: %s", name)
	}
}

func TestMembershipBumpsPermVersionOnEveryChange(t *testing.T) {
	m, err := entities.NewActiveMembership(uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), m.PermVersion)

	require.NoError(t, m.ReplaceRoles([]uuid.UUID{uuid.New()}))
	assert.Equal(t, int64(2), m.PermVersion)
	require.NoError(t, m.Suspend())
	assert.Equal(t, int64(3), m.PermVersion)
}

func TestAccessAndRoles(t *testing.T) {
	access := &entities.Access{Permissions: []entities.Permission{entities.PermMembersRead, entities.PermMembersInvite}}
	assert.True(t, access.HasAll([]entities.Permission{entities.PermMembersRead}))
	assert.True(t, access.HasAll(nil))
	assert.False(t, access.HasAll([]entities.Permission{entities.PermMembersRead, entities.PermRolesAssign}))

	a := &entities.Role{Permissions: []entities.Permission{entities.PermRolesRead, entities.PermMembersRead}}
	b := &entities.Role{Permissions: []entities.Permission{entities.PermMembersRead}}
	assert.Equal(t, []entities.Permission{entities.PermMembersRead, entities.PermRolesRead}, entities.PermissionsOf([]*entities.Role{a, b}))

	owner := &entities.Role{IsSystem: true, Scope: enums.OrganizationTypePartner, Code: entities.RoleCodeOwner}
	assert.True(t, owner.IsOwnerRole())
	superAdmin := &entities.Role{IsSystem: true, Scope: enums.OrganizationTypePlatform, Code: entities.RoleCodeSuperAdmin}
	assert.True(t, superAdmin.IsOwnerRole())
	assert.False(t, (&entities.Role{IsSystem: true, Scope: enums.OrganizationTypePlatform, Code: entities.RoleCodeOwner}).IsOwnerRole())
}

func TestNewPartnerOrganization_NIF(t *testing.T) {
	cases := []struct {
		name, nif, want string
		valid           bool
	}{
		{"absent", "", "", true},
		{"company", "5417123456", "5417123456", true},
		{"spaces are ignored", " 541 712 3456 ", "5417123456", true},
		{"sole trader bi number", "004567891la042", "004567891LA042", true},
		{"too short", "541712345", "", false},
		{"letters in a company nif", "54171234AB", "", false},
		{"symbols", "541-712-3456", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			org, err := entities.NewPartnerOrganization("Transportes Kianda", tc.nif)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, org.NIF)
			assert.Equal(t, enums.OrganizationTypePartner, org.Type)
		})
	}
}

func TestOrganization_UpdateDetails(t *testing.T) {
	org, err := entities.NewPartnerOrganization("Transportes Kianda", "5417123456")
	require.NoError(t, err)

	changed, err := org.UpdateDetails(" Transportes Kianda ", "541 712 3456")
	require.NoError(t, err)
	assert.False(t, changed, "the same details after normalisation are no change")

	changed, err = org.UpdateDetails("Kianda, Lda", "")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "Kianda, Lda", org.Name)
	assert.Empty(t, org.NIF, "an empty nif removes it")

	_, err = org.UpdateDetails("", "5417123456")
	require.Error(t, err)
	_, err = org.UpdateDetails("Kianda", "123")
	require.Error(t, err)
	assert.Equal(t, "Kianda, Lda", org.Name, "an invalid update changes nothing")
}
