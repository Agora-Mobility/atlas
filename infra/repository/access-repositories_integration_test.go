//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/tenant"
)

func newStaff(ctx context.Context, t *testing.T, users *repository.UserRepository, email, phone string) *entities.User {
	t.Helper()
	user, err := entities.NewInvitedStaffUser("Staff "+email, email, phone)
	require.NoError(t, err)
	require.NoError(t, users.Create(ctx, user))
	return user
}

func TestMembershipRepository(t *testing.T) {
	pool, ownerPool := setupDBs(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)
	orgs := repository.NewOrganizationRepository(pool)
	roles := repository.NewRoleRepository(pool)
	memberships := repository.NewMembershipRepository(pool)

	platform, err := orgs.Platform(ctx)
	require.NoError(t, err)
	partner, err := entities.NewPartnerOrganization("Parceiro A", "541 712 3456")
	require.NoError(t, err)
	require.NoError(t, orgs.Create(ctx, partner))

	t.Run("a partner is updated, but never the platform", func(t *testing.T) {
		other, err := entities.NewPartnerOrganization("Parceiro Z", "")
		require.NoError(t, err)
		require.NoError(t, orgs.Create(ctx, other))

		_, err = other.UpdateDetails("Parceiro Z, Lda", "5000000001")
		require.NoError(t, err)
		require.NoError(t, orgs.Update(ctx, other))
		stored, err := orgs.FindByID(ctx, other.ID)
		require.NoError(t, err)
		assert.Equal(t, "Parceiro Z, Lda", stored.Name)
		assert.Equal(t, "5000000001", stored.NIF)

		_, err = other.UpdateDetails("Parceiro Z, Lda", "5417123456")
		require.NoError(t, err)
		assert.ErrorIs(t, orgs.Update(ctx, other), domainrepository.ErrNIFAlreadyExists)

		_, err = platform.UpdateDetails("Outro nome", "")
		require.NoError(t, err)
		assert.ErrorIs(t, orgs.Update(ctx, platform), domainrepository.ErrNotFound)
	})

	t.Run("the nif is optional and unique", func(t *testing.T) {
		stored, err := orgs.FindByID(ctx, partner.ID)
		require.NoError(t, err)
		assert.Equal(t, "5417123456", stored.NIF)

		same, err := entities.NewPartnerOrganization("Parceiro B", "5417123456")
		require.NoError(t, err)
		assert.ErrorIs(t, orgs.Create(ctx, same), domainrepository.ErrNIFAlreadyExists)

		for _, name := range []string{"Sem NIF 1", "Sem NIF 2"} {
			withoutNIF, err := entities.NewPartnerOrganization(name, "")
			require.NoError(t, err)
			require.NoError(t, orgs.Create(ctx, withoutNIF), "many organizations can have no nif")
		}

		found, total, err := orgs.ListPartners(ctx, "54171", pagination.Params{})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total, "search by nif")
		assert.Equal(t, partner.ID, found[0].ID)

		platform, err := orgs.Platform(ctx)
		require.NoError(t, err)
		assert.Empty(t, platform.NIF)
	})

	atAgora := tenant.WithOrganization(ctx, platform.ID)
	atPartner := tenant.WithOrganization(ctx, partner.ID)

	superAdmin, err := roles.FindSystemRole(ctx, enums.OrganizationTypePlatform, entities.RoleCodeSuperAdmin)
	require.NoError(t, err)
	assert.True(t, superAdmin.RequiresMFA)
	assert.Contains(t, superAdmin.Permissions, entities.PermRolesManage)
	operations, err := roles.FindSystemRole(ctx, enums.OrganizationTypePlatform, "operations")
	require.NoError(t, err)
	owner, err := roles.FindSystemRole(ctx, enums.OrganizationTypePartner, entities.RoleCodeOwner)
	require.NoError(t, err)

	partnerRoles, err := roles.ListForOrganization(atPartner, partner)
	require.NoError(t, err)
	for _, r := range partnerRoles {
		assert.Equal(t, enums.OrganizationTypePartner, r.Scope, "only roles of the partner scope")
	}

	ana := newStaff(ctx, t, users, "ana@agora.ao", "+244923000001")
	bruno := newStaff(ctx, t, users, "bruno@agora.ao", "+244923000002")

	anaAtAgora, err := entities.NewActiveMembership(ana.ID, platform.ID, []uuid.UUID{superAdmin.ID})
	require.NoError(t, err)
	require.NoError(t, memberships.Create(atAgora, anaAtAgora))
	brunoAtAgora, err := entities.NewInvitedMembership(bruno.ID, platform.ID, &ana.ID, []uuid.UUID{operations.ID})
	require.NoError(t, err)
	require.NoError(t, memberships.Create(atAgora, brunoAtAgora))
	anaAtPartner, err := entities.NewActiveMembership(ana.ID, partner.ID, []uuid.UUID{owner.ID})
	require.NoError(t, err)
	require.NoError(t, memberships.Create(atPartner, anaAtPartner))

	t.Run("RN16 a membership can only be written in its own organization", func(t *testing.T) {
		other, err := entities.NewActiveMembership(bruno.ID, partner.ID, []uuid.UUID{owner.ID})
		require.NoError(t, err)
		assert.ErrorContains(t, memberships.Create(atAgora, other), "row-level security")
		assert.ErrorContains(t, memberships.Create(ctx, other), "row-level security", "no tenant, no write")
	})

	t.Run("RN02 the database refuses a role of another scope", func(t *testing.T) {
		wrong, err := entities.NewActiveMembership(bruno.ID, partner.ID, []uuid.UUID{operations.ID})
		require.NoError(t, err)
		assert.ErrorContains(t, memberships.Create(atPartner, wrong), "cannot be assigned")
	})

	t.Run("access joins the permissions of every role", func(t *testing.T) {
		access, err := memberships.Access(atAgora, anaAtAgora.ID)
		require.NoError(t, err)
		assert.Equal(t, platform.ID, access.OrganizationID)
		assert.Equal(t, enums.OrganizationTypePlatform, access.Scope)
		assert.True(t, access.RequiresMFA)
		assert.ElementsMatch(t, superAdmin.Permissions, access.Permissions)
	})

	t.Run("sign in sees only active memberships of the scope", func(t *testing.T) {
		found, err := memberships.ListActiveForSignIn(tenant.WithUser(ctx, ana.ID), ana.ID, enums.OrganizationTypePartner)
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, anaAtPartner.ID, found[0].ID)
		assert.Equal(t, []uuid.UUID{owner.ID}, found[0].RoleIDs)

		found, err = memberships.ListActiveForSignIn(tenant.WithUser(ctx, bruno.ID), bruno.ID, enums.OrganizationTypePlatform)
		require.NoError(t, err)
		assert.Empty(t, found, "an invited member cannot sign in")
	})

	t.Run("members are always scoped to one organization", func(t *testing.T) {
		list, total, err := memberships.ListMembers(atAgora, platform.ID, domainrepository.MemberFilter{}, pagination.Params{})
		require.NoError(t, err)
		assert.Equal(t, int64(2), total)
		assert.Len(t, list, 2)

		list, total, err = memberships.ListMembers(atAgora, platform.ID, domainrepository.MemberFilter{Status: enums.MembershipStatusInvited, Search: "BRUNO"}, pagination.Params{})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		assert.Equal(t, "bruno@agora.ao", list[0].Email)
		assert.Equal(t, []uuid.UUID{operations.ID}, list[0].RoleIDs)

		found, err := memberships.FindMember(atPartner, partner.ID, brunoAtAgora.ID)
		require.NoError(t, err)
		assert.Nil(t, found, "RN14 another organization does not see the member")
		inOrg, err := memberships.FindInOrganization(atPartner, partner.ID, brunoAtAgora.ID)
		require.NoError(t, err)
		assert.Nil(t, inOrg)
	})

	t.Run("CA16.1 a query without the organization filter only sees the tenant", func(t *testing.T) {
		list, total, err := memberships.ListMembers(atPartner, platform.ID, domainrepository.MemberFilter{}, pagination.Params{})
		require.NoError(t, err)
		assert.Zero(t, total, "the partner asks for AGORA members and RLS answers nothing")
		assert.Empty(t, list)

		var visible []gen.MembershipRole
		require.NoError(t, postgres.Scoped(atPartner, pool, func(ctx context.Context) error {
			var err error
			visible, err = postgres.Queries(ctx, pool).ListMembershipRoles(ctx, []uuid.UUID{anaAtAgora.ID, brunoAtAgora.ID, anaAtPartner.ID})
			return err
		}))
		require.Len(t, visible, 1, "role assignments follow the memberships")
		assert.Equal(t, anaAtPartner.ID, visible[0].MembershipID)
	})

	t.Run("CA16.2 without a tenant the tables answer nothing", func(t *testing.T) {
		found, err := memberships.FindByUserAndOrganization(ctx, ana.ID, platform.ID)
		require.NoError(t, err)
		assert.Nil(t, found)
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM memberships").Scan(&count))
		assert.Zero(t, count)
		require.NoError(t, ownerPool.QueryRow(ctx, "SELECT count(*) FROM memberships").Scan(&count))
		assert.Equal(t, 3, count, "the owner, used only by migrations and the seed, sees every row")
	})

	t.Run("replacing roles and counting owners", func(t *testing.T) {
		others, err := memberships.CountOtherActiveWithRole(atAgora, platform.ID, superAdmin.ID, anaAtAgora.ID)
		require.NoError(t, err)
		assert.Zero(t, others, "ana is the only super admin")

		require.NoError(t, brunoAtAgora.Activate())
		require.NoError(t, memberships.Update(atAgora, brunoAtAgora))
		require.NoError(t, brunoAtAgora.ReplaceRoles([]uuid.UUID{superAdmin.ID, operations.ID}))
		require.NoError(t, memberships.ReplaceRoles(atAgora, brunoAtAgora))

		others, err = memberships.CountOtherActiveWithRole(atAgora, platform.ID, superAdmin.ID, anaAtAgora.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(1), others)
		stored, err := memberships.FindInOrganization(atAgora, platform.ID, brunoAtAgora.ID)
		require.NoError(t, err)
		assert.ElementsMatch(t, []uuid.UUID{superAdmin.ID, operations.ID}, stored.RoleIDs)
		assert.Equal(t, int64(3), stored.PermVersion)
	})
}

func TestInvitationAndMFARepositories(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(pool)
	orgs := repository.NewOrganizationRepository(pool)
	invitations := repository.NewInvitationRepository(pool)
	mfa := repository.NewUserMFARepository(pool)
	platform, err := orgs.Platform(ctx)
	require.NoError(t, err)
	ana := newStaff(ctx, t, users, "ana@agora.ao", "+244923000001")
	membership, err := entities.NewInvitedMembership(ana.ID, platform.ID, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repository.NewMembershipRepository(pool).Create(tenant.WithOrganization(ctx, platform.ID), membership))

	t.Run("a resent invitation replaces the previous one", func(t *testing.T) {
		scope, err := invitations.Scope(ctx, "hash-1")
		require.NoError(t, err)
		assert.Nil(t, scope, "unknown token")
		scoped := tenant.WithOrganization(ctx, platform.ID)
		first, err := entities.NewInvitation(membership.ID, "hash-1", nil)
		require.NoError(t, err)
		require.NoError(t, invitations.Create(scoped, first))
		require.NoError(t, invitations.RevokePending(scoped, membership.ID, time.Now().UTC()))
		second, err := entities.NewInvitation(membership.ID, "hash-2", nil)
		require.NoError(t, err)
		require.NoError(t, invitations.Create(scoped, second))

		scope, err = invitations.Scope(ctx, "hash-2")
		require.NoError(t, err)
		assert.Equal(t, platform.ID, scope.OrganizationID, "the token alone tells the tenant")
		assert.Equal(t, ana.ID, scope.UserID)
		hidden, err := invitations.FindByTokenHash(ctx, "hash-2")
		require.NoError(t, err)
		assert.Nil(t, hidden, "but the invitation itself needs the tenant")

		old, err := invitations.FindByTokenHash(scoped, "hash-1")
		require.NoError(t, err)
		assert.False(t, old.IsUsable(time.Now()))
		current, err := invitations.FindByTokenHash(scoped, "hash-2")
		require.NoError(t, err)
		assert.True(t, current.IsUsable(time.Now()))

		require.NoError(t, invitations.Accept(scoped, current.ID, time.Now().UTC()))
		used, err := invitations.FindByTokenHash(scoped, "hash-2")
		require.NoError(t, err)
		assert.False(t, used.IsUsable(time.Now()))
	})

	t.Run("an authenticator step is used once", func(t *testing.T) {
		require.NoError(t, mfa.SavePending(ctx, ana.ID, "sealed-1", time.Now().UTC()))
		pending, err := mfa.Find(ctx, ana.ID)
		require.NoError(t, err)
		assert.False(t, pending.IsConfirmed())

		used, err := mfa.UseStep(ctx, ana.ID, 100, time.Now().UTC())
		require.NoError(t, err)
		assert.True(t, used)
		used, err = mfa.UseStep(ctx, ana.ID, 100, time.Now().UTC())
		require.NoError(t, err)
		assert.False(t, used, "replay")
		used, err = mfa.UseStep(ctx, ana.ID, 99, time.Now().UTC())
		require.NoError(t, err)
		assert.False(t, used, "older step")

		require.NoError(t, mfa.SavePending(ctx, ana.ID, "sealed-2", time.Now().UTC()))
		confirmed, err := mfa.Find(ctx, ana.ID)
		require.NoError(t, err)
		assert.True(t, confirmed.IsConfirmed())
		assert.Equal(t, "sealed-1", confirmed.Secret, "a confirmed secret is never replaced by a new setup")
	})
}
