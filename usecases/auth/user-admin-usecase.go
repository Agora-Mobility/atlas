package usecases

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrCannotManageSelf = errors.New("users cannot perform this action on their own account")
	ErrReasonRequired   = errors.New("a reason is required")
	ErrUnknownReason    = errors.New("block reason does not exist or is inactive")
)

const (
	auditEntityUser          = "user"
	auditActionBlock         = "user.block"
	auditActionUnblock       = "user.unblock"
	auditActionResetPassword = "user.reset-password"
)

var appAccountRoles = []enums.Role{enums.RoleCustomer, enums.RoleProvider}

type UserAdminUsecase struct {
	userRepo   repository.IUserRepository
	outboxRepo repository.IOutboxRepository
	auditRepo  repository.IAuditLogRepository
	reasonRepo repository.IBlockReasonRepository
	activity   repository.IActivityRepository
	txManager  repository.ITransactionManager
	revoker    *SessionRevoker
	passwords  passwordReplacer
}

func NewUserAdminUsecase(
	userRepo repository.IUserRepository,
	outboxRepo repository.IOutboxRepository,
	auditRepo repository.IAuditLogRepository,
	reasonRepo repository.IBlockReasonRepository,
	activity repository.IActivityRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	revoker *SessionRevoker,
) *UserAdminUsecase {
	return &UserAdminUsecase{
		userRepo:   userRepo,
		outboxRepo: outboxRepo,
		auditRepo:  auditRepo,
		reasonRepo: reasonRepo,
		activity:   activity,
		txManager:  txManager,
		revoker:    revoker,
		passwords:  passwordReplacer{userRepo: userRepo, revoker: revoker, txManager: txManager, hasher: hasher},
	}
}

func (uc *UserAdminUsecase) List(ctx context.Context, query dtos.UserListQuery) (*dtos.UserPage, error) {
	roles := appAccountRoles
	if query.Role != "" {
		roles = []enums.Role{query.Role}
	}
	params := pagination.Params{Page: query.Page, Size: query.Size}
	users, total, err := uc.userRepo.List(ctx, repository.UserFilter{
		Roles:    roles,
		Status:   query.Status,
		Search:   query.Search,
		Category: query.Category,
	}, params)
	if err != nil {
		return nil, err
	}

	views, err := uc.accountViews(ctx, users)
	if err != nil {
		return nil, err
	}
	page := pagination.NewPage(views, params, total)
	return &dtos.UserPage{Items: page.Items, Page: page.Page, Size: page.Size, Total: page.Total, TotalPages: page.TotalPages}, nil
}

func (uc *UserAdminUsecase) Get(ctx context.Context, userID uuid.UUID) (*dtos.AccountView, error) {
	user, err := uc.appAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	views, err := uc.accountViews(ctx, []*entities.User{user})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (uc *UserAdminUsecase) accountViews(ctx context.Context, users []*entities.User) ([]dtos.AccountView, error) {
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	activity, err := uc.activity.Activity(ctx, ids)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.AccountView, 0, len(users))
	for _, u := range users {
		a := activity[u.ID]
		view := dtos.AccountView{
			UserView: dtos.ToUserView(u), CompletedRequests: a.CompletedRequests, CancelledRequests: a.CancelledRequests, LastRequestAt: a.LastRequestAt,
		}
		if u.Role == enums.RoleProvider {
			view.ProviderCategory, view.ProviderCategoryName = a.CategorySlug, a.CategoryName
		}
		views = append(views, view)
	}
	return views, nil
}

func (uc *UserAdminUsecase) ListBlockReasons(ctx context.Context) (*dtos.BlockReasonList, error) {
	reasons, err := uc.reasonRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]dtos.BlockReasonView, 0, len(reasons))
	for _, r := range reasons {
		views = append(views, dtos.ToBlockReasonView(r))
	}
	return &dtos.BlockReasonList{Items: views}, nil
}

func (uc *UserAdminUsecase) Block(ctx context.Context, actor entities.Actor, userID uuid.UUID, req dtos.BlockUserInput) (*dtos.UserView, error) {
	reasonIDs := distinctIDs(req.ReasonIDs)
	if len(reasonIDs) == 0 {
		return nil, ErrReasonRequired
	}
	user, err := uc.managedUser(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	if user.IsBlocked() {
		view := dtos.ToUserView(user)
		return &view, nil
	}
	reasons, err := uc.reasonRepo.ListActiveByIDs(ctx, reasonIDs)
	if err != nil {
		return nil, err
	}
	if len(reasons) != len(reasonIDs) {
		return nil, ErrUnknownReason
	}

	before := dtos.ToUserView(user)
	user.Block()
	block, err := entities.NewUserBlock(user.ID, actor.ID, reasons, strings.TrimSpace(req.Note), user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	blocked, err := events.NewEnvelope(events.UserBlockedV1{
		ID: user.ID, Role: user.Role, BlockedBy: actor.ID, Reasons: block.ReasonCodes(), Note: block.Note, BlockedAt: block.BlockedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	return uc.applyStatusChange(ctx, actor, user, before, auditActionBlock, blockAuditReason(block), blocked, func(ctx context.Context) error {
		if err := uc.reasonRepo.RecordBlock(ctx, block); err != nil {
			return err
		}
		return uc.revoker.RevokeUser(ctx, user.ID)
	})
}

func (uc *UserAdminUsecase) Unblock(ctx context.Context, actor entities.Actor, userID uuid.UUID, req dtos.UnblockUserInput) (*dtos.UserView, error) {
	user, err := uc.managedUser(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsBlocked() {
		view := dtos.ToUserView(user)
		return &view, nil
	}

	before := dtos.ToUserView(user)
	user.Activate()
	unblocked, err := events.NewEnvelope(events.UserUnblockedV1{
		ID: user.ID, Role: user.Role, UnblockedBy: actor.ID, UnblockedAt: user.UpdatedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	return uc.applyStatusChange(ctx, actor, user, before, auditActionUnblock, strings.TrimSpace(req.Reason), unblocked, nil)
}

func (uc *UserAdminUsecase) ResetPassword(ctx context.Context, actor entities.Actor, userID uuid.UUID, req dtos.ResetPasswordInput) error {
	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}
	user, err := uc.managedUser(ctx, actor, userID)
	if err != nil {
		return err
	}

	view := dtos.ToUserView(user)
	entry, err := entities.NewAuditEntry(actor, auditActionResetPassword, auditEntityUser, user.ID.String(), nil, view, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return uc.passwords.replace(ctx, user, req.NewPassword, func(ctx context.Context) error {
		return uc.auditRepo.Record(ctx, entry)
	})
}

func (uc *UserAdminUsecase) applyStatusChange(
	ctx context.Context,
	actor entities.Actor,
	user *entities.User,
	before dtos.UserView,
	action, reason string,
	event events.Envelope,
	alsoInTransaction func(context.Context) error,
) (*dtos.UserView, error) {
	after := dtos.ToUserView(user)
	entry, err := entities.NewAuditEntry(actor, action, auditEntityUser, user.ID.String(), before, after, reason, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.userRepo.Update(ctx, user); err != nil {
			return err
		}
		if alsoInTransaction != nil {
			if err := alsoInTransaction(ctx); err != nil {
				return err
			}
		}
		if err := uc.outboxRepo.Save(ctx, event); err != nil {
			return err
		}
		return uc.auditRepo.Record(ctx, entry)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return &after, nil
}

func (uc *UserAdminUsecase) managedUser(ctx context.Context, actor entities.Actor, userID uuid.UUID) (*entities.User, error) {
	if actor.ID != nil && *actor.ID == userID {
		return nil, ErrCannotManageSelf
	}
	return uc.appAccount(ctx, userID)
}

func (uc *UserAdminUsecase) appAccount(ctx context.Context, userID uuid.UUID) (*entities.User, error) {
	user, err := uc.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.Role.IsAppAccount() {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func distinctIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id == uuid.Nil {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func blockAuditReason(block *entities.UserBlock) string {
	reason := strings.Join(block.ReasonCodes(), ", ")
	if block.Note == "" {
		return reason
	}
	return reason + ": " + block.Note
}
