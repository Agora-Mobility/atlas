package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

type OrganizationRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IOrganizationRepository = (*OrganizationRepository)(nil)

func NewOrganizationRepository(pool *pgxpool.Pool) *OrganizationRepository {
	return &OrganizationRepository{pool: pool}
}

func (r *OrganizationRepository) Platform(ctx context.Context) (*entities.Organization, error) {
	row, err := postgres.Queries(ctx, r.pool).GetPlatformOrganization(ctx)
	if err != nil {
		return nil, fmt.Errorf("get platform organization: %w", err)
	}
	return toOrganization(row), nil
}

func (r *OrganizationRepository) FindByID(ctx context.Context, id uuid.UUID) (*entities.Organization, error) {
	row, err := postgres.Queries(ctx, r.pool).GetOrganizationByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return toOrganization(row), nil
}

func (r *OrganizationRepository) Lock(ctx context.Context, id uuid.UUID) error {
	if _, err := postgres.Queries(ctx, r.pool).LockOrganization(ctx, id); err != nil {
		return fmt.Errorf("lock organization: %w", err)
	}
	return nil
}

func (r *OrganizationRepository) Create(ctx context.Context, org *entities.Organization) error {
	err := postgres.Queries(ctx, r.pool).CreateOrganization(ctx, gen.CreateOrganizationParams{
		ID: org.ID, Type: org.Type, Name: org.Name, Nif: nullableString(org.NIF), CreatedAt: org.CreatedAt, UpdatedAt: org.UpdatedAt,
	})
	if err != nil {
		return userWriteError("insert organization", err)
	}
	return nil
}

func (r *OrganizationRepository) Update(ctx context.Context, org *entities.Organization) error {
	rows, err := postgres.Queries(ctx, r.pool).UpdatePartnerOrganization(ctx, gen.UpdatePartnerOrganizationParams{
		ID: org.ID, Name: org.Name, Nif: nullableString(org.NIF), UpdatedAt: org.UpdatedAt,
	})
	if err != nil {
		return userWriteError("update organization", err)
	}
	if rows == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (r *OrganizationRepository) ListPartners(ctx context.Context, search string, page pagination.Params) ([]*entities.Organization, int64, error) {
	q := postgres.Queries(ctx, r.pool)
	pattern := searchPattern(search)
	total, err := q.CountPartnerOrganizations(ctx, pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("count partners: %w", err)
	}
	rows, err := q.ListPartnerOrganizations(ctx, gen.ListPartnerOrganizationsParams{
		Search: pattern, PageLimit: page.Limit(), PageOffset: page.Offset(),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list partners: %w", err)
	}
	orgs := make([]*entities.Organization, 0, len(rows))
	for _, row := range rows {
		orgs = append(orgs, toOrganization(row))
	}
	return orgs, total, nil
}

func toOrganization(row gen.Organization) *entities.Organization {
	return &entities.Organization{
		ID: row.ID, Type: row.Type, Name: row.Name, NIF: valueOrEmpty(row.Nif), CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}

type RoleRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRoleRepository = (*RoleRepository)(nil)

func NewRoleRepository(pool *pgxpool.Pool) *RoleRepository {
	return &RoleRepository{pool: pool}
}

func (r *RoleRepository) ListPermissions(ctx context.Context, scope enums.OrganizationType) ([]*entities.PermissionDefinition, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) ([]*entities.PermissionDefinition, error) {
		rows, err := postgres.Queries(ctx, r.pool).ListPermissionsByScope(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("list permissions: %w", err)
		}
		defs := make([]*entities.PermissionDefinition, 0, len(rows))
		for _, row := range rows {
			scopes := make([]enums.OrganizationType, 0, len(row.Scopes))
			for _, s := range row.Scopes {
				scopes = append(scopes, enums.OrganizationType(s))
			}
			defs = append(defs, &entities.PermissionDefinition{Code: entities.Permission(row.Code), Description: row.Description, Scopes: scopes})
		}
		return defs, nil
	})
}

func (r *RoleRepository) ListForOrganization(ctx context.Context, org *entities.Organization) ([]*entities.Role, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) ([]*entities.Role, error) {
		rows, err := postgres.Queries(ctx, r.pool).ListRolesForOrganization(ctx, gen.ListRolesForOrganizationParams{
			Scope: org.Type, OrganizationID: org.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		return r.withPermissions(ctx, rows)
	})
}

func (r *RoleRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*entities.Role, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) ([]*entities.Role, error) {
		if len(ids) == 0 {
			return []*entities.Role{}, nil
		}
		rows, err := postgres.Queries(ctx, r.pool).GetRolesByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("get roles: %w", err)
		}
		return r.withPermissions(ctx, rows)
	})
}

func (r *RoleRepository) FindSystemRole(ctx context.Context, scope enums.OrganizationType, code string) (*entities.Role, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Role, error) {
		row, err := postgres.Queries(ctx, r.pool).GetSystemRoleByCode(ctx, gen.GetSystemRoleByCodeParams{Scope: scope, Code: code})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get system role: %w", err)
		}
		roles, err := r.withPermissions(ctx, []gen.Role{row})
		if err != nil {
			return nil, err
		}
		return roles[0], nil
	})
}

func (r *RoleRepository) Create(ctx context.Context, role *entities.Role) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		now := time.Now().UTC()
		err := postgres.Queries(ctx, r.pool).CreateRole(ctx, gen.CreateRoleParams{
			ID: role.ID, OrganizationID: role.OrganizationID, Scope: role.Scope, Code: role.Code, Name: role.Name,
			RequiresMfa: role.RequiresMFA, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return userWriteError("insert role", err)
		}
		return r.addPermissions(ctx, role)
	})
}

func (r *RoleRepository) Update(ctx context.Context, role *entities.Role) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		q := postgres.Queries(ctx, r.pool)
		rows, err := q.UpdateRole(ctx, gen.UpdateRoleParams{ID: role.ID, Name: role.Name, RequiresMfa: role.RequiresMFA, UpdatedAt: time.Now().UTC()})
		if err != nil {
			return fmt.Errorf("update role: %w", err)
		}
		if rows == 0 {
			return repository.ErrNotFound
		}
		if err := q.DeleteRolePermissions(ctx, role.ID); err != nil {
			return fmt.Errorf("delete role permissions: %w", err)
		}
		return r.addPermissions(ctx, role)
	})
}

func (r *RoleRepository) Delete(ctx context.Context, roleID uuid.UUID) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		rows, err := postgres.Queries(ctx, r.pool).DeleteRole(ctx, roleID)
		if err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		if rows == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

func (r *RoleRepository) IsAssigned(ctx context.Context, roleID uuid.UUID) (bool, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (bool, error) {
		assigned, err := postgres.Queries(ctx, r.pool).RoleIsAssigned(ctx, roleID)
		if err != nil {
			return false, fmt.Errorf("check role assignments: %w", err)
		}
		return assigned, nil
	})
}

func (r *RoleRepository) BumpMembershipsWithRole(ctx context.Context, roleID uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) ([]uuid.UUID, error) {
		ids, err := postgres.Queries(ctx, r.pool).BumpMembershipsWithRole(ctx, gen.BumpMembershipsWithRoleParams{RoleID: roleID, UpdatedAt: at})
		if err != nil {
			return nil, fmt.Errorf("bump memberships with role: %w", err)
		}
		return ids, nil
	})
}

func (r *RoleRepository) addPermissions(ctx context.Context, role *entities.Role) error {
	codes := make([]string, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		codes = append(codes, string(p))
	}
	if err := postgres.Queries(ctx, r.pool).AddRolePermissions(ctx, gen.AddRolePermissionsParams{RoleID: role.ID, PermissionCodes: codes}); err != nil {
		return fmt.Errorf("insert role permissions: %w", err)
	}
	return nil
}

func (r *RoleRepository) withPermissions(ctx context.Context, rows []gen.Role) ([]*entities.Role, error) {
	roles := make([]*entities.Role, 0, len(rows))
	byID := make(map[uuid.UUID]*entities.Role, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		role := &entities.Role{
			ID:             row.ID,
			OrganizationID: row.OrganizationID,
			Scope:          row.Scope,
			Code:           row.Code,
			Name:           row.Name,
			RequiresMFA:    row.RequiresMfa,
			IsSystem:       row.IsSystem,
			Permissions:    []entities.Permission{},
		}
		roles = append(roles, role)
		byID[row.ID] = role
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return roles, nil
	}
	perms, err := postgres.Queries(ctx, r.pool).ListRolePermissions(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	for _, p := range perms {
		byID[p.RoleID].Permissions = append(byID[p.RoleID].Permissions, entities.Permission(p.PermissionCode))
	}
	return roles, nil
}

type MembershipRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IMembershipRepository = (*MembershipRepository)(nil)

func NewMembershipRepository(pool *pgxpool.Pool) *MembershipRepository {
	return &MembershipRepository{pool: pool}
}

func (r *MembershipRepository) Create(ctx context.Context, m *entities.Membership) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		err := postgres.Queries(ctx, r.pool).CreateMembership(ctx, gen.CreateMembershipParams{
			ID:             m.ID,
			UserID:         m.UserID,
			OrganizationID: m.OrganizationID,
			Status:         m.Status,
			PermVersion:    m.PermVersion,
			InvitedBy:      m.InvitedBy,
			ActivatedAt:    m.ActivatedAt,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert membership: %w", err)
		}
		return r.addRoles(ctx, m)
	})
}

func (r *MembershipRepository) Update(ctx context.Context, m *entities.Membership) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		err := postgres.Queries(ctx, r.pool).UpdateMembership(ctx, gen.UpdateMembershipParams{
			ID: m.ID, Status: m.Status, PermVersion: m.PermVersion, ActivatedAt: m.ActivatedAt, UpdatedAt: m.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("update membership: %w", err)
		}
		return nil
	})
}

func (r *MembershipRepository) ReplaceRoles(ctx context.Context, m *entities.Membership) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		if err := postgres.Queries(ctx, r.pool).DeleteMembershipRoles(ctx, m.ID); err != nil {
			return fmt.Errorf("delete membership roles: %w", err)
		}
		if err := r.addRoles(ctx, m); err != nil {
			return err
		}
		return r.Update(ctx, m)
	})
}

func (r *MembershipRepository) addRoles(ctx context.Context, m *entities.Membership) error {
	if len(m.RoleIDs) == 0 {
		return nil
	}
	err := postgres.Queries(ctx, r.pool).AddMembershipRoles(ctx, gen.AddMembershipRolesParams{MembershipID: m.ID, RoleIds: m.RoleIDs})
	if err != nil {
		return fmt.Errorf("insert membership roles: %w", err)
	}
	return nil
}

func (r *MembershipRepository) FindInOrganization(ctx context.Context, organizationID, id uuid.UUID) (*entities.Membership, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Membership, error) {
		row, err := postgres.Queries(ctx, r.pool).GetMembershipInOrganization(ctx, gen.GetMembershipInOrganizationParams{ID: id, OrganizationID: organizationID})
		return r.membershipOrNil(ctx, row, err)
	})
}

func (r *MembershipRepository) LockInOrganization(ctx context.Context, organizationID, id uuid.UUID) (*entities.Membership, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Membership, error) {
		row, err := postgres.Queries(ctx, r.pool).GetMembershipInOrganizationForUpdate(ctx, gen.GetMembershipInOrganizationForUpdateParams{ID: id, OrganizationID: organizationID})
		return r.membershipOrNil(ctx, row, err)
	})
}

func (r *MembershipRepository) LockByID(ctx context.Context, id uuid.UUID) (*entities.Membership, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Membership, error) {
		row, err := postgres.Queries(ctx, r.pool).GetMembershipByIDForUpdate(ctx, id)
		return r.membershipOrNil(ctx, row, err)
	})
}

func (r *MembershipRepository) FindByUserAndOrganization(ctx context.Context, userID, organizationID uuid.UUID) (*entities.Membership, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Membership, error) {
		row, err := postgres.Queries(ctx, r.pool).GetMembershipByUserAndOrganization(ctx, gen.GetMembershipByUserAndOrganizationParams{UserID: userID, OrganizationID: organizationID})
		return r.membershipOrNil(ctx, row, err)
	})
}

func (r *MembershipRepository) ListActiveForSignIn(ctx context.Context, userID uuid.UUID, scope enums.OrganizationType) ([]*entities.Membership, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) ([]*entities.Membership, error) {
		rows, err := postgres.Queries(ctx, r.pool).ListActiveMembershipsForSignIn(ctx, gen.ListActiveMembershipsForSignInParams{UserID: userID, Type: scope})
		if err != nil {
			return nil, fmt.Errorf("list memberships for sign in: %w", err)
		}
		memberships := make([]*entities.Membership, 0, len(rows))
		for i := range rows {
			memberships = append(memberships, toMembership(rows[i]))
		}
		return memberships, r.loadRoles(ctx, memberships)
	})
}

func (r *MembershipRepository) ListMembers(ctx context.Context, organizationID uuid.UUID, filter repository.MemberFilter, page pagination.Params) ([]*entities.Member, int64, error) {
	var members []*entities.Member
	var total int64
	err := postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		q := postgres.Queries(ctx, r.pool)
		var status *enums.MembershipStatus
		if filter.Status != "" {
			status = &filter.Status
		}
		search := searchPattern(filter.Search)
		var err error
		total, err = q.CountMembers(ctx, gen.CountMembersParams{OrganizationID: organizationID, Status: status, RoleID: filter.RoleID, Search: search})
		if err != nil {
			return fmt.Errorf("count members: %w", err)
		}
		rows, err := q.ListMembers(ctx, gen.ListMembersParams{
			OrganizationID: organizationID, Status: status, RoleID: filter.RoleID, Search: search,
			PageLimit: page.Limit(), PageOffset: page.Offset(),
		})
		if err != nil {
			return fmt.Errorf("list members: %w", err)
		}
		members = make([]*entities.Member, 0, len(rows))
		for i := range rows {
			members = append(members, toMember(&rows[i]))
		}
		return r.loadMemberRoles(ctx, members)
	})
	return members, total, err
}

func (r *MembershipRepository) FindMember(ctx context.Context, organizationID, id uuid.UUID) (*entities.Member, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Member, error) {
		rows, err := postgres.Queries(ctx, r.pool).ListMembers(ctx, gen.ListMembersParams{
			OrganizationID: organizationID, PageLimit: 1, PageOffset: 0, MembershipID: &id,
		})
		if err != nil {
			return nil, fmt.Errorf("get member: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		members := []*entities.Member{toMember(&rows[0])}
		return members[0], r.loadMemberRoles(ctx, members)
	})
}

func (r *MembershipRepository) CountOtherActiveWithRole(ctx context.Context, organizationID, roleID, excludedMembershipID uuid.UUID) (int64, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (int64, error) {
		count, err := postgres.Queries(ctx, r.pool).CountOtherActiveMembersWithRole(ctx, gen.CountOtherActiveMembersWithRoleParams{
			OrganizationID: organizationID, RoleID: roleID, ExcludedMembershipID: excludedMembershipID,
		})
		if err != nil {
			return 0, fmt.Errorf("count members with role: %w", err)
		}
		return count, nil
	})
}

func (r *MembershipRepository) ExistsActiveWithRole(ctx context.Context, organizationID, roleID uuid.UUID) (bool, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (bool, error) {
		exists, err := postgres.Queries(ctx, r.pool).ExistsActiveMemberWithRole(ctx, gen.ExistsActiveMemberWithRoleParams{OrganizationID: organizationID, RoleID: roleID})
		if err != nil {
			return false, fmt.Errorf("check members with role: %w", err)
		}
		return exists, nil
	})
}

func (r *MembershipRepository) Access(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Access, error) {
		q := postgres.Queries(ctx, r.pool)
		row, err := q.GetMembershipAccess(ctx, membershipID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get membership access: %w", err)
		}
		codes, err := q.ListMembershipPermissions(ctx, membershipID)
		if err != nil {
			return nil, fmt.Errorf("list membership permissions: %w", err)
		}
		perms := make([]entities.Permission, 0, len(codes))
		for _, c := range codes {
			perms = append(perms, entities.Permission(c))
		}
		return &entities.Access{
			MembershipID:   row.ID,
			UserID:         row.UserID,
			OrganizationID: row.OrganizationID,
			Scope:          row.OrganizationType,
			Status:         row.Status,
			PermVersion:    row.PermVersion,
			RequiresMFA:    row.RequiresMfa,
			Permissions:    perms,
		}, nil
	})
}

func (r *MembershipRepository) HasMembershipOutside(ctx context.Context, userID, organizationID uuid.UUID) (bool, error) {
	outside, err := postgres.Queries(ctx, r.pool).UserHasMembershipOutside(ctx, gen.UserHasMembershipOutsideParams{UserID: userID, OrganizationID: organizationID})
	if err != nil {
		return false, fmt.Errorf("check memberships outside the organization: %w", err)
	}
	return outside, nil
}

func (r *MembershipRepository) membershipOrNil(ctx context.Context, row gen.Membership, err error) (*entities.Membership, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get membership: %w", err)
	}
	m := toMembership(row)
	return m, r.loadRoles(ctx, []*entities.Membership{m})
}

func (r *MembershipRepository) loadRoles(ctx context.Context, memberships []*entities.Membership) error {
	if len(memberships) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]*entities.Membership, len(memberships))
	ids := make([]uuid.UUID, 0, len(memberships))
	for _, m := range memberships {
		m.RoleIDs = []uuid.UUID{}
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	rows, err := postgres.Queries(ctx, r.pool).ListMembershipRoles(ctx, ids)
	if err != nil {
		return fmt.Errorf("list membership roles: %w", err)
	}
	for _, row := range rows {
		byID[row.MembershipID].RoleIDs = append(byID[row.MembershipID].RoleIDs, row.RoleID)
	}
	return nil
}

func (r *MembershipRepository) loadMemberRoles(ctx context.Context, members []*entities.Member) error {
	memberships := make([]*entities.Membership, 0, len(members))
	for _, m := range members {
		memberships = append(memberships, &m.Membership)
	}
	return r.loadRoles(ctx, memberships)
}

func toMembership(row gen.Membership) *entities.Membership {
	return &entities.Membership{
		ID:             row.ID,
		UserID:         row.UserID,
		OrganizationID: row.OrganizationID,
		Status:         row.Status,
		PermVersion:    row.PermVersion,
		InvitedBy:      row.InvitedBy,
		ActivatedAt:    utcOrNil(row.ActivatedAt),
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
	}
}

func toMember(row *gen.ListMembersRow) *entities.Member {
	return &entities.Member{
		Membership: *toMembership(gen.Membership{
			ID: row.ID, UserID: row.UserID, OrganizationID: row.OrganizationID, Status: row.Status,
			PermVersion: row.PermVersion, InvitedBy: row.InvitedBy, ActivatedAt: row.ActivatedAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}),
		Name:        row.UserName,
		Email:       valueOrEmpty(row.UserEmail),
		Phone:       valueOrEmpty(row.UserPhone),
		LastLoginAt: utcOrNil(row.UserLastLoginAt),
	}
}

type InvitationRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IInvitationRepository = (*InvitationRepository)(nil)

func NewInvitationRepository(pool *pgxpool.Pool) *InvitationRepository {
	return &InvitationRepository{pool: pool}
}

func (r *InvitationRepository) Scope(ctx context.Context, tokenHash string) (*repository.InvitationScope, error) {
	row, err := postgres.Queries(ctx, r.pool).GetInvitationScope(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invitation scope: %w", err)
	}
	return &repository.InvitationScope{OrganizationID: row.OrganizationID, UserID: row.UserID}, nil
}

func (r *InvitationRepository) Create(ctx context.Context, i *entities.Invitation) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		err := postgres.Queries(ctx, r.pool).CreateInvitation(ctx, gen.CreateInvitationParams{
			ID: i.ID, MembershipID: i.MembershipID, TokenHash: i.TokenHash, CreatedBy: i.CreatedBy,
			ExpiresAt: i.ExpiresAt, AcceptedAt: i.AcceptedAt, RevokedAt: i.RevokedAt, CreatedAt: i.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert invitation: %w", err)
		}
		return nil
	})
}

func (r *InvitationRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*entities.Invitation, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Invitation, error) {
		return invitationOrNil(postgres.Queries(ctx, r.pool).GetInvitationByHash(ctx, tokenHash))
	})
}

func (r *InvitationRepository) LockByTokenHash(ctx context.Context, tokenHash string) (*entities.Invitation, error) {
	return inTenant(ctx, r.pool, func(ctx context.Context) (*entities.Invitation, error) {
		return invitationOrNil(postgres.Queries(ctx, r.pool).GetInvitationByHashForUpdate(ctx, tokenHash))
	})
}

func (r *InvitationRepository) RevokePending(ctx context.Context, membershipID uuid.UUID, at time.Time) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		if err := postgres.Queries(ctx, r.pool).RevokePendingInvitations(ctx, gen.RevokePendingInvitationsParams{MembershipID: membershipID, RevokedAt: &at}); err != nil {
			return fmt.Errorf("revoke invitations: %w", err)
		}
		return nil
	})
}

func (r *InvitationRepository) Accept(ctx context.Context, id uuid.UUID, at time.Time) error {
	return postgres.Scoped(ctx, r.pool, func(ctx context.Context) error {
		if err := postgres.Queries(ctx, r.pool).AcceptInvitation(ctx, gen.AcceptInvitationParams{ID: id, AcceptedAt: &at}); err != nil {
			return fmt.Errorf("accept invitation: %w", err)
		}
		return nil
	})
}

func invitationOrNil(row gen.Invitation, err error) (*entities.Invitation, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invitation: %w", err)
	}
	return &entities.Invitation{
		ID: row.ID, MembershipID: row.MembershipID, TokenHash: row.TokenHash, CreatedBy: row.CreatedBy,
		ExpiresAt: row.ExpiresAt.UTC(), AcceptedAt: utcOrNil(row.AcceptedAt), RevokedAt: utcOrNil(row.RevokedAt),
		CreatedAt: row.CreatedAt.UTC(),
	}, nil
}

type UserMFARepository struct {
	pool *pgxpool.Pool
}

var _ repository.IUserMFARepository = (*UserMFARepository)(nil)

func NewUserMFARepository(pool *pgxpool.Pool) *UserMFARepository {
	return &UserMFARepository{pool: pool}
}

func (r *UserMFARepository) Find(ctx context.Context, userID uuid.UUID) (*entities.UserMFA, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserMFA(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user mfa: %w", err)
	}
	return &entities.UserMFA{UserID: row.UserID, Secret: row.Secret, ConfirmedAt: utcOrNil(row.ConfirmedAt), LastUsedStep: row.LastUsedStep}, nil
}

func (r *UserMFARepository) SavePending(ctx context.Context, userID uuid.UUID, sealedSecret string, at time.Time) error {
	if err := postgres.Queries(ctx, r.pool).SavePendingUserMFA(ctx, gen.SavePendingUserMFAParams{UserID: userID, Secret: sealedSecret, UpdatedAt: at}); err != nil {
		return fmt.Errorf("save user mfa: %w", err)
	}
	return nil
}

func (r *UserMFARepository) Delete(ctx context.Context, userID uuid.UUID) error {
	q := postgres.Queries(ctx, r.pool)
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	if err := q.DeleteUserMFA(ctx, userID); err != nil {
		return fmt.Errorf("delete user mfa: %w", err)
	}
	return nil
}

func (r *UserMFARepository) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, codeHashes []string, at time.Time) error {
	return postgres.NewTxManager(r.pool).WithinTransaction(ctx, func(ctx context.Context) error {
		q := postgres.Queries(ctx, r.pool)
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}
		if err := q.CreateRecoveryCodes(ctx, gen.CreateRecoveryCodesParams{UserID: userID, CodeHashes: codeHashes, CreatedAt: at}); err != nil {
			return fmt.Errorf("insert recovery codes: %w", err)
		}
		return nil
	})
}

func (r *UserMFARepository) UseRecoveryCode(ctx context.Context, userID uuid.UUID, codeHash string, at time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).UseRecoveryCode(ctx, gen.UseRecoveryCodeParams{UserID: userID, CodeHash: codeHash, UsedAt: &at})
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	return rows == 1, nil
}

func (r *UserMFARepository) CountUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int64, error) {
	count, err := postgres.Queries(ctx, r.pool).CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return count, nil
}

func (r *UserMFARepository) UseStep(ctx context.Context, userID uuid.UUID, step int64, at time.Time) (bool, error) {
	rows, err := postgres.Queries(ctx, r.pool).UseUserMFAStep(ctx, gen.UseUserMFAStepParams{UserID: userID, Step: step, UsedAt: at})
	if err != nil {
		return false, fmt.Errorf("use mfa step: %w", err)
	}
	return rows == 1, nil
}

func inTenant[T any](ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context) (T, error)) (T, error) {
	var out T
	err := postgres.Scoped(ctx, pool, func(ctx context.Context) error {
		var err error
		out, err = fn(ctx)
		return err
	})
	return out, err
}
