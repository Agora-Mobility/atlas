package usecases

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrUnknownPermission = errors.New("permission does not exist in the scope of the organization")
	ErrSystemRole        = errors.New("system roles cannot be changed")
	ErrRoleInUse         = errors.New("the role is still assigned to members")
	ErrRoleCodeTaken     = repository.ErrRoleCodeTaken
)

const (
	auditEntityRole       = "role"
	auditActionRoleCreate = "role.create"
	auditActionRoleUpdate = "role.update"
	auditActionRoleDelete = "role.delete"
)

type RoleUsecase struct {
	roleRepo   repository.IRoleRepository
	auditRepo  repository.IAuditLogRepository
	outboxRepo repository.IOutboxRepository
	txManager  repository.ITransactionManager
	access     *AccessResolver
}

func NewRoleUsecase(
	roleRepo repository.IRoleRepository,
	auditRepo repository.IAuditLogRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	access *AccessResolver,
) *RoleUsecase {
	return &RoleUsecase{roleRepo: roleRepo, auditRepo: auditRepo, outboxRepo: outboxRepo, txManager: txManager, access: access}
}

func (uc *RoleUsecase) Get(ctx context.Context, actor StaffActor, roleID uuid.UUID) (*dtos.RoleView, error) {
	role, err := uc.visibleRole(uc.scoped(ctx, actor), actor, roleID)
	if err != nil {
		return nil, err
	}
	view := dtos.ToRoleView(role)
	return &view, nil
}

func (uc *RoleUsecase) Create(ctx context.Context, actor StaffActor, req dtos.CreateRoleInput) (*dtos.RoleView, error) {
	ctx = uc.scoped(ctx, actor)
	org := actorOrganization(actor)
	permissions, err := uc.grantable(ctx, actor, req.Permissions)
	if err != nil {
		return nil, err
	}
	role, err := entities.NewCustomRole(org, req.Code, req.Name, permissions)
	if err != nil {
		return nil, err
	}
	system, err := uc.roleRepo.FindSystemRole(ctx, org.Type, role.Code)
	if err != nil {
		return nil, err
	}
	if system != nil {
		return nil, ErrRoleCodeTaken
	}

	view := dtos.ToRoleView(role)
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.roleRepo.Create(ctx, role); err != nil {
			return err
		}
		if err := uc.publish(ctx, events.RoleCreatedV1{RoleChange: roleChange(role, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionRoleCreate, role.ID, nil, view)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (uc *RoleUsecase) Update(ctx context.Context, actor StaffActor, roleID uuid.UUID, req dtos.UpdateRoleInput) (*dtos.RoleView, error) {
	ctx = uc.scoped(ctx, actor)
	role, err := uc.customRole(ctx, actor, roleID)
	if err != nil {
		return nil, err
	}
	before := dtos.ToRoleView(role)
	name, permissions := role.Name, role.Permissions
	if req.Name != nil {
		name = *req.Name
	}
	if req.Permissions != nil {
		if permissions, err = uc.grantable(ctx, actor, req.Permissions); err != nil {
			return nil, err
		}
	}
	if err := role.Change(name, permissions); err != nil {
		return nil, err
	}
	after := dtos.ToRoleView(role)
	accessChanged := !slices.Equal(before.Permissions, after.Permissions)

	var affected []uuid.UUID
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.roleRepo.Update(ctx, role); err != nil {
			return err
		}
		if accessChanged {
			if affected, err = uc.roleRepo.BumpMembershipsWithRole(ctx, role.ID, time.Now().UTC()); err != nil {
				return err
			}
		}
		if err := uc.publish(ctx, events.RoleUpdatedV1{RoleChange: roleChange(role, actor), AffectedMemberships: len(affected)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionRoleUpdate, role.ID, before, after)
	})
	if err != nil {
		return nil, err
	}
	if len(affected) > 0 {
		uc.access.Invalidate(ctx, affected...)
	}
	return &after, nil
}

func (uc *RoleUsecase) Delete(ctx context.Context, actor StaffActor, roleID uuid.UUID) error {
	ctx = uc.scoped(ctx, actor)
	role, err := uc.customRole(ctx, actor, roleID)
	if err != nil {
		return err
	}
	before := dtos.ToRoleView(role)
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		assigned, err := uc.roleRepo.IsAssigned(ctx, role.ID)
		if err != nil {
			return err
		}
		if assigned {
			return ErrRoleInUse
		}
		if err := uc.roleRepo.Delete(ctx, role.ID); err != nil {
			return err
		}
		if err := uc.publish(ctx, events.RoleDeletedV1{RoleChange: roleChange(role, actor)}); err != nil {
			return err
		}
		return uc.audit(ctx, actor, auditActionRoleDelete, role.ID, before, nil)
	})
}

func (uc *RoleUsecase) grantable(ctx context.Context, actor StaffActor, codes []string) ([]entities.Permission, error) {
	catalog, err := uc.roleRepo.ListPermissions(ctx, actor.Access.Scope)
	if err != nil {
		return nil, err
	}
	permissions := make([]entities.Permission, 0, len(codes))
	for _, code := range codes {
		p := entities.Permission(code)
		if !slices.ContainsFunc(catalog, func(d *entities.PermissionDefinition) bool { return d.Code == p }) {
			return nil, ErrUnknownPermission
		}
		permissions = append(permissions, p)
	}
	if !actor.Access.HasAll(permissions) {
		return nil, ErrPrivilegeEscalation
	}
	return permissions, nil
}

func (uc *RoleUsecase) customRole(ctx context.Context, actor StaffActor, roleID uuid.UUID) (*entities.Role, error) {
	role, err := uc.visibleRole(ctx, actor, roleID)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, ErrSystemRole
	}
	if !actor.Access.HasAll(role.Permissions) {
		return nil, ErrPrivilegeEscalation
	}
	return role, nil
}

func (uc *RoleUsecase) visibleRole(ctx context.Context, actor StaffActor, roleID uuid.UUID) (*entities.Role, error) {
	roles, err := uc.roleRepo.FindByIDs(ctx, []uuid.UUID{roleID})
	if err != nil {
		return nil, err
	}
	org := actorOrganization(actor)
	if len(roles) == 0 || !roles[0].VisibleTo(org) || roles[0].Scope != org.Type {
		return nil, ErrRoleNotFound
	}
	return roles[0], nil
}

func (uc *RoleUsecase) scoped(ctx context.Context, actor StaffActor) context.Context {
	return tenant.WithOrganization(ctx, actor.Access.OrganizationID)
}

func (uc *RoleUsecase) publish(ctx context.Context, event events.Event) error {
	envelope, err := events.NewEnvelope(event, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.outboxRepo.Save(ctx, envelope)
}

func (uc *RoleUsecase) audit(ctx context.Context, actor StaffActor, action string, roleID uuid.UUID, before, after any) error {
	entry, err := entities.NewAuditEntry(actor.Audit, action, auditEntityRole, roleID.String(), before, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.auditRepo.Record(ctx, entry.InOrganization(actor.Access.OrganizationID))
}

func actorOrganization(actor StaffActor) *entities.Organization {
	return &entities.Organization{ID: actor.Access.OrganizationID, Type: actor.Access.Scope}
}

func roleChange(role *entities.Role, actor StaffActor) events.RoleChange {
	codes := make([]string, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		codes = append(codes, string(p))
	}
	return events.RoleChange{
		RoleID: role.ID, OrganizationID: actor.Access.OrganizationID, Code: role.Code,
		Permissions: codes, ChangedBy: actor.Audit.ID, OccurredAt: time.Now().UTC(),
	}
}
