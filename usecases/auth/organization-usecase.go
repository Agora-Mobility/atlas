package usecases

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type OrganizationUsecase struct {
	orgRepo        repository.IOrganizationRepository
	roleRepo       repository.IRoleRepository
	membershipRepo repository.IMembershipRepository
	auditRepo      repository.IAuditLogRepository
	outboxRepo     repository.IOutboxRepository
	txManager      repository.ITransactionManager
}

func NewOrganizationUsecase(
	orgRepo repository.IOrganizationRepository,
	roleRepo repository.IRoleRepository,
	membershipRepo repository.IMembershipRepository,
	auditRepo repository.IAuditLogRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
) *OrganizationUsecase {
	return &OrganizationUsecase{
		orgRepo: orgRepo, roleRepo: roleRepo, membershipRepo: membershipRepo,
		auditRepo: auditRepo, outboxRepo: outboxRepo, txManager: txManager,
	}
}

func (uc *OrganizationUsecase) CreatePartner(ctx context.Context, actor StaffActor, req dtos.CreatePartnerInput) (*dtos.OrganizationView, error) {
	org, err := entities.NewPartnerOrganization(req.Name, req.NIF)
	if err != nil {
		return nil, err
	}
	view := dtos.ToOrganizationView(org)
	entry, err := entities.NewAuditEntry(actor.Audit, auditActionPartnerCreate, auditEntityOrganization, org.ID.String(), nil, view, "", logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}
	created, err := events.NewEnvelope(events.PartnerCreatedV1{
		OrganizationID: org.ID, Name: org.Name, NIF: org.NIF, CreatedBy: actor.Audit.ID, CreatedAt: org.CreatedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.orgRepo.Create(ctx, org); err != nil {
			return err
		}
		if err := uc.outboxRepo.Save(ctx, created); err != nil {
			return err
		}
		return uc.auditRepo.Record(ctx, entry.InOrganization(org.ID))
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (uc *OrganizationUsecase) UpdatePartner(ctx context.Context, actor StaffActor, organizationID uuid.UUID, req dtos.UpdatePartnerInput) (*dtos.OrganizationView, error) {
	org, err := uc.orgRepo.FindByID(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if org == nil || !org.IsPartner() {
		return nil, ErrOrganizationNotFound
	}
	before := dtos.ToOrganizationView(org)
	name, nif := org.Name, org.NIF
	if req.Name != nil {
		name = *req.Name
	}
	if req.NIF != nil {
		nif = *req.NIF
	}
	changed, err := org.UpdateDetails(name, nif)
	if err != nil {
		return nil, err
	}
	after := dtos.ToOrganizationView(org)
	if !changed {
		return &after, nil
	}

	entry, err := entities.NewAuditEntry(actor.Audit, auditActionPartnerUpdate, auditEntityOrganization, org.ID.String(), before, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}
	updated, err := events.NewEnvelope(events.PartnerUpdatedV1{
		OrganizationID: org.ID, Name: org.Name, NIF: org.NIF, UpdatedBy: actor.Audit.ID, UpdatedAt: org.UpdatedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}
	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.orgRepo.Update(ctx, org); err != nil {
			return err
		}
		if err := uc.outboxRepo.Save(ctx, updated); err != nil {
			return err
		}
		return uc.auditRepo.Record(ctx, entry.InOrganization(org.ID))
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrOrganizationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &after, nil
}

// GetPartner returns one partner organization. Any other organization, such as
// the platform one, is not found.
func (uc *OrganizationUsecase) GetPartner(ctx context.Context, organizationID uuid.UUID) (*dtos.OrganizationView, error) {
	org, err := uc.orgRepo.FindByID(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if org == nil || !org.IsPartner() {
		return nil, ErrOrganizationNotFound
	}
	view := dtos.ToOrganizationView(org)
	return &view, nil
}

func (uc *OrganizationUsecase) ListPartners(ctx context.Context, query dtos.PartnerListQuery) (*dtos.OrganizationPage, error) {
	params := pagination.Params{Page: query.Page, Size: query.Size}
	orgs, total, err := uc.orgRepo.ListPartners(ctx, query.Search, params)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.OrganizationView, 0, len(orgs))
	for _, o := range orgs {
		views = append(views, dtos.ToOrganizationView(o))
	}
	page := pagination.NewPage(views, params, total)
	return &dtos.OrganizationPage{Items: page.Items, Page: page.Page, Size: page.Size, Total: page.Total, TotalPages: page.TotalPages}, nil
}

func (uc *OrganizationUsecase) ListRoles(ctx context.Context, actor StaffActor) (*dtos.RoleList, error) {
	org, err := uc.organization(ctx, actor)
	if err != nil {
		return nil, err
	}
	return uc.rolesOf(ctx, org)
}

func (uc *OrganizationUsecase) rolesOf(ctx context.Context, org *entities.Organization) (*dtos.RoleList, error) {
	roles, err := uc.roleRepo.ListForOrganization(ctx, org)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.RoleView, 0, len(roles))
	for _, r := range roles {
		views = append(views, dtos.ToRoleView(r))
	}
	return &dtos.RoleList{Items: views}, nil
}

func (uc *OrganizationUsecase) ListPartnerRoles(ctx context.Context, organizationID uuid.UUID) (*dtos.RoleList, error) {
	org, err := uc.orgRepo.FindByID(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if org == nil || !org.IsPartner() {
		return nil, ErrOrganizationNotFound
	}
	return uc.rolesOf(tenant.WithOrganization(ctx, org.ID), org)
}

func (uc *OrganizationUsecase) ListPermissions(ctx context.Context, actor StaffActor) (*dtos.PermissionList, error) {
	defs, err := uc.roleRepo.ListPermissions(ctx, actor.Access.Scope)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.PermissionView, 0, len(defs))
	for _, d := range defs {
		views = append(views, dtos.PermissionView{Code: d.Code, Description: d.Description, Scopes: d.Scopes})
	}
	return &dtos.PermissionList{Items: views}, nil
}

func (uc *OrganizationUsecase) MyAccess(ctx context.Context, actor StaffActor) (*dtos.MyAccessView, error) {
	org, err := uc.organization(ctx, actor)
	if err != nil {
		return nil, err
	}
	membership, err := uc.membershipRepo.FindInOrganization(ctx, org.ID, actor.Access.MembershipID)
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, ErrMemberNotFound
	}
	roles, err := uc.roleRepo.FindByIDs(ctx, membership.RoleIDs)
	if err != nil {
		return nil, err
	}
	summaries := make([]dtos.RoleSummary, 0, len(roles))
	for _, r := range roles {
		summaries = append(summaries, dtos.RoleSummary{ID: r.ID, Code: r.Code, Name: r.Name})
	}
	return &dtos.MyAccessView{
		Organization: dtos.ToOrganizationView(org),
		MembershipID: actor.Access.MembershipID,
		Roles:        summaries,
		Permissions:  actor.Access.Permissions,
	}, nil
}

func (uc *OrganizationUsecase) organization(ctx context.Context, actor StaffActor) (*entities.Organization, error) {
	if tenant.From(ctx).OrganizationID == nil {
		return nil, ErrOrganizationNotFound
	}
	org, err := uc.orgRepo.FindByID(ctx, actor.Access.OrganizationID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrOrganizationNotFound
	}
	return org, nil
}
