package usecases

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var ErrSystemRoleMissing = errors.New("the super admin role is missing: run the migrations")

type AdminBootstrap struct {
	userRepo       repository.IUserRepository
	orgRepo        repository.IOrganizationRepository
	roleRepo       repository.IRoleRepository
	membershipRepo repository.IMembershipRepository
	outboxRepo     repository.IOutboxRepository
	auditRepo      repository.IAuditLogRepository
	txManager      repository.ITransactionManager
	hasher         services.IPasswordHasher
}

func NewAdminBootstrap(
	userRepo repository.IUserRepository,
	orgRepo repository.IOrganizationRepository,
	roleRepo repository.IRoleRepository,
	membershipRepo repository.IMembershipRepository,
	outboxRepo repository.IOutboxRepository,
	auditRepo repository.IAuditLogRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
) *AdminBootstrap {
	return &AdminBootstrap{
		userRepo:       userRepo,
		orgRepo:        orgRepo,
		roleRepo:       roleRepo,
		membershipRepo: membershipRepo,
		outboxRepo:     outboxRepo,
		auditRepo:      auditRepo,
		txManager:      txManager,
		hasher:         hasher,
	}
}

func (b *AdminBootstrap) Run(ctx context.Context, name, email, password string) (bool, error) {
	org, err := b.orgRepo.Platform(ctx)
	if err != nil {
		return false, err
	}
	ctx = tenant.WithOrganization(ctx, org.ID)
	role, err := b.roleRepo.FindSystemRole(ctx, enums.OrganizationTypePlatform, entities.RoleCodeSuperAdmin)
	if err != nil {
		return false, err
	}
	if role == nil {
		return false, ErrSystemRoleMissing
	}
	exists, err := b.membershipRepo.ExistsActiveWithRole(ctx, org.ID, role.ID)
	if err != nil || exists {
		return false, err
	}
	if err := validatePassword(password); err != nil {
		return false, err
	}

	email = normalizeEmail(email)
	if err := ensureIdentityIsFree(ctx, b.userRepo, email, ""); err != nil {
		return false, err
	}
	hash, err := b.hasher.Hash(password)
	if err != nil {
		return false, err
	}
	user, err := entities.NewUser(name, email, "", hash, enums.RoleStaff)
	if err != nil {
		return false, err
	}
	user.MarkVerified()
	membership, err := entities.NewActiveMembership(user.ID, org.ID, []uuid.UUID{role.ID})
	if err != nil {
		return false, err
	}

	correlationID := logger.CorrelationID(ctx)
	created, err := events.NewEnvelope(events.UserRegisteredV1{ID: user.ID, Role: user.Role, CreatedAt: user.CreatedAt}, correlationID)
	if err != nil {
		return false, err
	}
	after := map[string]any{"user": dtos.ToUserView(user), "roles": []string{role.Code}, "status": membership.Status}
	entry, err := entities.NewAuditEntry(entities.SystemActor(), auditActionAdminBootstrap, auditEntityMember, membership.ID.String(), nil, after, "", correlationID)
	if err != nil {
		return false, err
	}

	err = b.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := b.userRepo.Create(ctx, user); err != nil {
			return err
		}
		if err := b.membershipRepo.Create(ctx, membership); err != nil {
			return err
		}
		if err := b.outboxRepo.Save(ctx, created); err != nil {
			return err
		}
		return b.auditRepo.Record(ctx, entry.InOrganization(org.ID))
	})
	if err != nil {
		return false, fmt.Errorf("bootstrap super admin: %w", err)
	}
	return true, nil
}
