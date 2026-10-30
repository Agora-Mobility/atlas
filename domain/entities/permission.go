package entities

import (
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Permission string

const (
	PermMembersRead            Permission = "members:read"
	PermMembersInvite          Permission = "members:invite"
	PermMembersUpdate          Permission = "members:update"
	PermMembersSuspend         Permission = "members:suspend"
	PermMembersDeactivate      Permission = "members:deactivate"
	PermMembersMFAReset        Permission = "members:mfa-reset"
	PermRolesRead              Permission = "roles:read"
	PermRolesAssign            Permission = "roles:assign"
	PermRolesManage            Permission = "roles:manage"
	PermPartnersRead           Permission = "partners:read"
	PermPartnersManage         Permission = "partners:manage"
	PermUsersRead              Permission = "users:read"
	PermUsersBlock             Permission = "users:block"
	PermUsersResetPassword     Permission = "users:reset-password"
	PermProvidersRead          Permission = "providers:read"
	PermProvidersApprove       Permission = "providers:approve"
	PermProvidersSuspend       Permission = "providers:suspend"
	PermCategoriesManage       Permission = "categories:manage"
	PermServicesManage         Permission = "services:manage"
	PermRequestsRead           Permission = "requests:read"
	PermRequestsManage         Permission = "requests:manage"
	PermRequestsCancel         Permission = "requests:cancel"
	PermFinanceRead            Permission = "finance:read"
	PermRefundsCreate          Permission = "refunds:create"
	PermRefundsApprove         Permission = "refunds:approve"
	PermReconciliationRun      Permission = "reconciliation:run"
	PermAuditRead              Permission = "audit:read"
	PermPartnerProvidersRead   Permission = "partner.providers:read"
	PermPartnerProvidersManage Permission = "partner.providers:manage"
	PermPartnerRequestsRead    Permission = "partner.requests:read"
	PermPartnerFinanceRead     Permission = "partner.finance:read"
)

const (
	RoleCodeSuperAdmin = "super_admin"
	RoleCodeOwner      = "owner"
)

func OwnerRoleCode(scope enums.OrganizationType) string {
	if scope == enums.OrganizationTypePartner {
		return RoleCodeOwner
	}
	return RoleCodeSuperAdmin
}

type PermissionDefinition struct {
	Code        Permission
	Description string
	Scopes      []enums.OrganizationType
}

type Role struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	Scope          enums.OrganizationType
	Code           string
	Name           string
	RequiresMFA    bool
	IsSystem       bool
	Permissions    []Permission
}

func (r *Role) IsOwnerRole() bool {
	return r.IsSystem && r.Code == OwnerRoleCode(r.Scope)
}

func (r *Role) VisibleTo(org *Organization) bool {
	return r.OrganizationID == nil || *r.OrganizationID == org.ID
}

type Access struct {
	MembershipID   uuid.UUID
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	Scope          enums.OrganizationType
	Status         enums.MembershipStatus
	PermVersion    int64
	RequiresMFA    bool
	Permissions    []Permission
}

func (a *Access) IsActive() bool {
	return a.Status == enums.MembershipStatusActive
}

func (a *Access) Has(permission Permission) bool {
	return slices.Contains(a.Permissions, permission)
}

func (a *Access) HasAll(permissions []Permission) bool {
	for _, p := range permissions {
		if !a.Has(p) {
			return false
		}
	}
	return true
}

func (a *Access) Actor(role enums.Role, ip, userAgent string) Actor {
	userID := a.UserID
	organizationID := a.OrganizationID
	return Actor{ID: &userID, Role: role, OrganizationID: &organizationID, IPAddress: ip, UserAgent: userAgent}
}

func PermissionsOf(roles []*Role) []Permission {
	var all []Permission
	for _, r := range roles {
		for _, p := range r.Permissions {
			if !slices.Contains(all, p) {
				all = append(all, p)
			}
		}
	}
	slices.Sort(all)
	return all
}

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func NewCustomRole(org *Organization, code, name string, requiresMFA bool, permissions []Permission) (*Role, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	role := &Role{ID: id, OrganizationID: &org.ID, Scope: org.Type, Code: strings.TrimSpace(code)}
	if !roleCodePattern.MatchString(role.Code) {
		return nil, validationError("code", "O código deve ter 2 a 40 letras minúsculas, números ou _, e começar por uma letra.")
	}
	if err := role.Change(name, requiresMFA, permissions); err != nil {
		return nil, err
	}
	return role, nil
}

func (r *Role) Change(name string, requiresMFA bool, permissions []Permission) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return validationError("name", "O nome é obrigatório e tem no máximo 80 caracteres.")
	}
	distinct := make([]Permission, 0, len(permissions))
	for _, p := range permissions {
		if !slices.Contains(distinct, p) {
			distinct = append(distinct, p)
		}
	}
	if len(distinct) == 0 {
		return validationError("permissions", "Indique pelo menos uma permissão.")
	}
	slices.Sort(distinct)
	r.Name = name
	r.RequiresMFA = requiresMFA
	r.Permissions = distinct
	return nil
}
