package dtos

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type AccessView struct {
	MembershipID   uuid.UUID              `json:"membership_id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	Scope          enums.OrganizationType `json:"scope" example:"platform"`
	Permissions    []entities.Permission  `json:"permissions" swaggertype:"array,string" example:"members:read,members:invite"`
}

func ToAccessView(a *entities.Access) *AccessView {
	if a == nil {
		return nil
	}
	return &AccessView{MembershipID: a.MembershipID, OrganizationID: a.OrganizationID, Scope: a.Scope, Permissions: a.Permissions}
}

type OrganizationView struct {
	ID        uuid.UUID              `json:"id"`
	Type      enums.OrganizationType `json:"type" example:"partner"`
	Name      string                 `json:"name" example:"Transportes Kianda"`
	NIF       string                 `json:"nif,omitempty" example:"5417123456"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func ToOrganizationView(o *entities.Organization) OrganizationView {
	return OrganizationView{ID: o.ID, Type: o.Type, Name: o.Name, NIF: o.NIF, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

type OrganizationPage struct {
	Items      []OrganizationView `json:"items"`
	Page       int                `json:"page"`
	Size       int                `json:"size"`
	Total      int64              `json:"total"`
	TotalPages int64              `json:"total_pages"`
}

type CreatePartnerInput struct {
	Name string `json:"name" binding:"required,max=120" example:"Transportes Kianda"`
	NIF  string `json:"nif" binding:"omitempty,max=20" example:"5417123456"`
}

type UpdatePartnerInput struct {
	Name *string `json:"name" binding:"omitempty,max=120" example:"Transportes Kianda, Lda"`
	NIF  *string `json:"nif" binding:"omitempty,max=20" example:"5417123456"`
}

type PartnerListQuery struct {
	Search string `form:"search" binding:"max=120"`
	Page   int    `form:"page" binding:"omitempty,min=1"`
	Size   int    `form:"size" binding:"omitempty,min=1,max=100"`
}

type PermissionView struct {
	Code        entities.Permission      `json:"code" swaggertype:"string" example:"members:read"`
	Description string                   `json:"description"`
	Scopes      []enums.OrganizationType `json:"scopes"`
}

type PermissionList struct {
	Items []PermissionView `json:"items"`
}

type RoleView struct {
	ID          uuid.UUID              `json:"id"`
	Code        string                 `json:"code" example:"operations"`
	Name        string                 `json:"name" example:"Operações"`
	Scope       enums.OrganizationType `json:"scope" example:"platform"`
	IsSystem    bool                   `json:"is_system"`
	Permissions []entities.Permission  `json:"permissions" swaggertype:"array,string"`
}

func ToRoleView(r *entities.Role) RoleView {
	return RoleView{
		ID: r.ID, Code: r.Code, Name: r.Name, Scope: r.Scope, IsSystem: r.IsSystem,
		Permissions: r.Permissions,
	}
}

type RoleList struct {
	Items []RoleView `json:"items"`
}

type RoleSummary struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code" example:"operations"`
	Name string    `json:"name" example:"Operações"`
}

type MemberView struct {
	ID          uuid.UUID              `json:"id"`
	UserID      uuid.UUID              `json:"user_id"`
	Name        string                 `json:"name"`
	Email       string                 `json:"email,omitempty"`
	Phone       string                 `json:"phone,omitempty"`
	Status      enums.MembershipStatus `json:"status" example:"active"`
	Roles       []RoleSummary          `json:"roles"`
	ActivatedAt *time.Time             `json:"activated_at,omitempty"`
	LastLoginAt *time.Time             `json:"last_login_at,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

type MemberPage struct {
	Items      []MemberView `json:"items"`
	Page       int          `json:"page"`
	Size       int          `json:"size"`
	Total      int64        `json:"total"`
	TotalPages int64        `json:"total_pages"`
}

type MemberListQuery struct {
	Status enums.MembershipStatus `form:"status" binding:"omitempty,oneof=invited active suspended deactivated"`
	RoleID *uuid.UUID             `form:"role_id"`
	Search string                 `form:"search" binding:"max=120"`
	Page   int                    `form:"page" binding:"omitempty,min=1"`
	Size   int                    `form:"size" binding:"omitempty,min=1,max=100"`
}

type InviteMemberInput struct {
	Name    string      `json:"name" binding:"required,max=120" example:"Operador AGORA"`
	Email   string      `json:"email" binding:"required,email,max=254" example:"operador@agora.ao"`
	Phone   string      `json:"phone" binding:"required,max=32" example:"+244923000010"`
	RoleIDs []uuid.UUID `json:"role_ids" binding:"required,min=1,max=20"`
}

type UpdateMemberInput struct {
	Name  *string `json:"name" binding:"omitempty,max=120" example:"Operador AGORA"`
	Phone *string `json:"phone" binding:"omitempty,max=32" example:"+244923000011"`
}

type ReplaceRolesInput struct {
	RoleIDs []uuid.UUID `json:"role_ids" binding:"required,min=1,max=20"`
}

type InvitationTokenInput struct {
	Token string `json:"token" binding:"required,max=128"`
}

type AcceptInvitationInput struct {
	Token    string `json:"token" binding:"required,max=128"`
	Password string `json:"password" binding:"required,max=128" example:"S3gredo!forte"`
	Code     string `json:"code" binding:"required,len=6,numeric" example:"123456"`
}

type MyAccessView struct {
	Organization OrganizationView      `json:"organization"`
	MembershipID uuid.UUID             `json:"membership_id"`
	Roles        []RoleSummary         `json:"roles"`
	Permissions  []entities.Permission `json:"permissions" swaggertype:"array,string"`
}

type CreateRoleInput struct {
	Code        string   `json:"code" binding:"required,max=40" example:"field_support"`
	Name        string   `json:"name" binding:"required,max=80" example:"Suporte de campo"`
	Permissions []string `json:"permissions" binding:"required,min=1,max=60" example:"members:read,providers:read"`
}

type UpdateRoleInput struct {
	Name        *string  `json:"name" binding:"omitempty,max=80" example:"Suporte de campo"`
	Permissions []string `json:"permissions" binding:"omitempty,min=1,max=60"`
}
