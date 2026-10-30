package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

type MemberFilter struct {
	Status enums.MembershipStatus
	RoleID *uuid.UUID
	Search string
}

type IOrganizationRepository interface {
	Platform(ctx context.Context) (*entities.Organization, error)
	FindByID(ctx context.Context, id uuid.UUID) (*entities.Organization, error)
	Lock(ctx context.Context, id uuid.UUID) error
	Create(ctx context.Context, org *entities.Organization) error
	ListPartners(ctx context.Context, search string, page pagination.Params) ([]*entities.Organization, int64, error)
}

type IRoleRepository interface {
	ListPermissions(ctx context.Context, scope enums.OrganizationType) ([]*entities.PermissionDefinition, error)
	ListForOrganization(ctx context.Context, org *entities.Organization) ([]*entities.Role, error)
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*entities.Role, error)
	FindSystemRole(ctx context.Context, scope enums.OrganizationType, code string) (*entities.Role, error)
	Create(ctx context.Context, role *entities.Role) error
	Update(ctx context.Context, role *entities.Role) error
	Delete(ctx context.Context, roleID uuid.UUID) error
	IsAssigned(ctx context.Context, roleID uuid.UUID) (bool, error)
	BumpMembershipsWithRole(ctx context.Context, roleID uuid.UUID, at time.Time) ([]uuid.UUID, error)
}

type IMembershipRepository interface {
	Create(ctx context.Context, membership *entities.Membership) error
	Update(ctx context.Context, membership *entities.Membership) error
	ReplaceRoles(ctx context.Context, membership *entities.Membership) error
	FindInOrganization(ctx context.Context, organizationID, id uuid.UUID) (*entities.Membership, error)
	LockInOrganization(ctx context.Context, organizationID, id uuid.UUID) (*entities.Membership, error)
	LockByID(ctx context.Context, id uuid.UUID) (*entities.Membership, error)
	FindByUserAndOrganization(ctx context.Context, userID, organizationID uuid.UUID) (*entities.Membership, error)
	ListActiveForSignIn(ctx context.Context, userID uuid.UUID, scope enums.OrganizationType) ([]*entities.Membership, error)
	ListMembers(ctx context.Context, organizationID uuid.UUID, filter MemberFilter, page pagination.Params) ([]*entities.Member, int64, error)
	FindMember(ctx context.Context, organizationID, id uuid.UUID) (*entities.Member, error)
	CountOtherActiveWithRole(ctx context.Context, organizationID, roleID, excludedMembershipID uuid.UUID) (int64, error)
	ExistsActiveWithRole(ctx context.Context, organizationID, roleID uuid.UUID) (bool, error)
	Access(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error)
	HasMembershipOutside(ctx context.Context, userID, organizationID uuid.UUID) (bool, error)
}

type InvitationScope struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
}

type IInvitationRepository interface {
	Scope(ctx context.Context, tokenHash string) (*InvitationScope, error)
	Create(ctx context.Context, invitation *entities.Invitation) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*entities.Invitation, error)
	LockByTokenHash(ctx context.Context, tokenHash string) (*entities.Invitation, error)
	RevokePending(ctx context.Context, membershipID uuid.UUID, at time.Time) error
	Accept(ctx context.Context, id uuid.UUID, at time.Time) error
}

type IUserMFARepository interface {
	Find(ctx context.Context, userID uuid.UUID) (*entities.UserMFA, error)
	SavePending(ctx context.Context, userID uuid.UUID, sealedSecret string, at time.Time) error
	UseStep(ctx context.Context, userID uuid.UUID, step int64, at time.Time) (bool, error)
	Delete(ctx context.Context, userID uuid.UUID) error
	ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, codeHashes []string, at time.Time) error
	UseRecoveryCode(ctx context.Context, userID uuid.UUID, codeHash string, at time.Time) (bool, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int64, error)
}
