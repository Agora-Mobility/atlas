package usecases

import (
	"context"
	"errors"
	"time"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var ErrInvalidInvitation = errors.New("invitation is invalid, expired or already used")

type InvitationUsecase struct {
	userRepo       repository.IUserRepository
	orgRepo        repository.IOrganizationRepository
	membershipRepo repository.IMembershipRepository
	invitationRepo repository.IInvitationRepository
	auditRepo      repository.IAuditLogRepository
	outboxRepo     repository.IOutboxRepository
	txManager      repository.ITransactionManager
	hasher         services.IPasswordHasher
	opaqueTokens   services.IOpaqueTokenGenerator
	otp            services.IOTPService
	codeSender     *VerificationCodeSender
	access         *AccessResolver
}

func NewInvitationUsecase(
	userRepo repository.IUserRepository,
	orgRepo repository.IOrganizationRepository,
	membershipRepo repository.IMembershipRepository,
	invitationRepo repository.IInvitationRepository,
	auditRepo repository.IAuditLogRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	opaqueTokens services.IOpaqueTokenGenerator,
	otp services.IOTPService,
	codeSender *VerificationCodeSender,
	access *AccessResolver,
) *InvitationUsecase {
	return &InvitationUsecase{
		userRepo:       userRepo,
		orgRepo:        orgRepo,
		membershipRepo: membershipRepo,
		invitationRepo: invitationRepo,
		auditRepo:      auditRepo,
		outboxRepo:     outboxRepo,
		txManager:      txManager,
		hasher:         hasher,
		opaqueTokens:   opaqueTokens,
		otp:            otp,
		codeSender:     codeSender,
		access:         access,
	}
}

type pendingInvitation struct {
	invitation *entities.Invitation
	membership *entities.Membership
	org        *entities.Organization
	user       *entities.User
}

func (uc *InvitationUsecase) RequestCode(ctx context.Context, client *entities.Client, req dtos.InvitationTokenInput) error {
	var pending *pendingInvitation
	err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		_, pending, err = uc.pending(ctx, client, req.Token, false)
		return err
	})
	if err != nil {
		return err
	}
	return uc.codeSender.SendTo(ctx, enums.OTPPurposeInvitation, pending.invitation.ID.String(), pending.user.Phone)
}

func (uc *InvitationUsecase) Accept(ctx context.Context, client *entities.Client, req dtos.AcceptInvitationInput, ip, userAgent string) error {
	var activated *entities.Membership
	err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		ctx, pending, err := uc.pending(ctx, client, req.Token, true)
		if err != nil {
			return err
		}
		if err := uc.otp.Verify(ctx, enums.OTPPurposeInvitation, pending.invitation.ID.String(), req.Code); err != nil {
			return err
		}
		if err := uc.setPassword(pending.user, req.Password); err != nil {
			return err
		}
		pending.user.MarkVerified()
		if err := uc.userRepo.Update(ctx, pending.user); err != nil {
			return err
		}
		before := pending.membership.Status
		if err := pending.membership.Activate(); err != nil {
			return ErrInvalidInvitation
		}
		if err := uc.membershipRepo.Update(ctx, pending.membership); err != nil {
			return err
		}
		if err := uc.invitationRepo.Accept(ctx, pending.invitation.ID, time.Now().UTC()); err != nil {
			return err
		}
		activated = pending.membership

		userID, orgID := pending.user.ID, pending.org.ID
		activatedEvent, err := events.NewEnvelope(events.MemberActivatedV1{
			MemberChange: events.NewMemberChange(pending.membership.ID, orgID, userID, &userID, pending.membership.PermVersion),
		}, logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		if err := uc.outboxRepo.Save(ctx, activatedEvent); err != nil {
			return err
		}
		actor := entities.Actor{ID: &userID, Role: enums.RoleStaff, OrganizationID: &orgID, IPAddress: ip, UserAgent: userAgent}
		entry, err := entities.NewAuditEntry(actor, auditActionMemberActivate, auditEntityMember, pending.membership.ID.String(), before, pending.membership.Status, "", logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		return uc.auditRepo.Record(ctx, entry)
	})
	if err != nil {
		return err
	}
	uc.access.Invalidate(ctx, activated.ID)
	return nil
}

func (uc *InvitationUsecase) setPassword(user *entities.User, password string) error {
	if user.HasPassword() {
		matches, _, err := uc.hasher.Verify(password, user.Password)
		if err != nil {
			return err
		}
		if !matches {
			return ErrInvalidCurrentPassword
		}
		return nil
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := uc.hasher.Hash(password)
	if err != nil {
		return err
	}
	return user.ChangePassword(hash)
}

func (uc *InvitationUsecase) pending(ctx context.Context, client *entities.Client, token string, lock bool) (context.Context, *pendingInvitation, error) {
	hash := uc.opaqueTokens.Hash(token)
	scope, err := uc.invitationRepo.Scope(ctx, hash)
	if err != nil {
		return ctx, nil, err
	}
	if scope == nil {
		return ctx, nil, ErrInvalidInvitation
	}
	ctx = tenant.WithOrganization(tenant.WithUser(ctx, scope.UserID), scope.OrganizationID)
	pending, err := uc.load(ctx, client, hash, lock)
	return ctx, pending, err
}

func (uc *InvitationUsecase) load(ctx context.Context, client *entities.Client, hash string, lock bool) (*pendingInvitation, error) {
	find := uc.invitationRepo.FindByTokenHash
	if lock {
		find = uc.invitationRepo.LockByTokenHash
	}
	invitation, err := find(ctx, hash)
	if err != nil {
		return nil, err
	}
	if invitation == nil || !invitation.IsUsable(time.Now().UTC()) {
		return nil, ErrInvalidInvitation
	}
	membership, err := uc.membershipRepo.LockByID(ctx, invitation.MembershipID)
	if err != nil {
		return nil, err
	}
	if membership == nil || membership.Status != enums.MembershipStatusInvited {
		return nil, ErrInvalidInvitation
	}
	org, err := uc.orgRepo.FindByID(ctx, membership.OrganizationID)
	if err != nil {
		return nil, err
	}
	scope, ok := client.StaffScope()
	if org == nil || !ok || scope != org.Type {
		return nil, ErrInvalidInvitation
	}
	user, err := uc.userRepo.FindByID(ctx, membership.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Phone == "" {
		return nil, ErrInvalidInvitation
	}
	return &pendingInvitation{invitation: invitation, membership: membership, org: org, user: user}, nil
}
