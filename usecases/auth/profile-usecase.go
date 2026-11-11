package usecases

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

// ProfileUsecase lets signed-in users manage their own account (RF03).
type ProfileUsecase struct {
	userRepo   repository.IUserRepository
	outboxRepo repository.IOutboxRepository
	txManager  repository.ITransactionManager
	hasher     services.IPasswordHasher
	otp        services.IOTPService
	codeSender *VerificationCodeSender
	revoker    *SessionRevoker
}

func NewProfileUsecase(
	userRepo repository.IUserRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	otp services.IOTPService,
	codeSender *VerificationCodeSender,
	revoker *SessionRevoker,
) *ProfileUsecase {
	return &ProfileUsecase{
		userRepo:   userRepo,
		outboxRepo: outboxRepo,
		txManager:  txManager,
		hasher:     hasher,
		otp:        otp,
		codeSender: codeSender,
		revoker:    revoker,
	}
}

func (uc *ProfileUsecase) UpdateProfile(ctx context.Context, userID uuid.UUID, req dtos.UpdateProfileInput) (*dtos.UserView, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	name := user.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, customerrors.FieldError("name", "O nome é obrigatório.")
		}
	}
	email := user.Email
	if req.Email != nil {
		email = normalizeEmail(*req.Email)
		if email == "" && user.IsStaff() {
			return nil, customerrors.FieldError("email", "O e-mail é obrigatório para iniciar sessão.")
		}
	}

	if email != user.Email {
		if err := uc.confirmIdentity(ctx, user, req.CurrentPassword, req.Code); err != nil {
			return nil, err
		}
		if err := uc.ensureEmailIsFree(ctx, email, user.ID); err != nil {
			return nil, err
		}
	}

	if err := user.UpdateProfile(name, email, user.Phone); err != nil {
		return nil, err
	}
	if err := uc.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}

	view := dtos.ToUserView(user)
	return &view, nil
}

func (uc *ProfileUsecase) RequestIdentityCode(ctx context.Context, userID uuid.UUID) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.IsStaff() {
		return ErrClientNotAllowed
	}
	return uc.codeSender.SendTo(ctx, enums.OTPPurposeConfirmIdentity, user.ID.String(), user.Phone)
}

// RequestPhoneChange sends a code to the new number. The code is bound to the
// user, so only the signed-in user who asked for it can use it.
func (uc *ProfileUsecase) RequestPhoneChange(ctx context.Context, userID uuid.UUID, req dtos.PhoneChangeRequestInput) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}
	newPhone, err := uc.checkNewPhone(ctx, user, req.Phone)
	if err != nil {
		return err
	}
	return uc.codeSender.SendTo(ctx, enums.OTPPurposeChangePhone, phoneChangeSubject(user.ID, newPhone), newPhone)
}

func (uc *ProfileUsecase) ConfirmPhoneChange(ctx context.Context, userID uuid.UUID, req dtos.PhoneChangeConfirmInput) (*dtos.UserView, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	newPhone, err := uc.checkNewPhone(ctx, user, req.Phone)
	if err != nil {
		return nil, err
	}
	if err := uc.otp.Verify(ctx, enums.OTPPurposeChangePhone, phoneChangeSubject(user.ID, newPhone), req.Code); err != nil {
		return nil, err
	}

	if err := user.UpdateProfile(user.Name, user.Email, newPhone); err != nil {
		return nil, err
	}
	user.MarkVerified()
	changed, err := events.NewEnvelope(events.UserPhoneChangedV1{ID: user.ID, ChangedAt: user.UpdatedAt}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.userRepo.Update(ctx, user); err != nil {
			return err
		}
		return uc.outboxRepo.Save(ctx, changed)
	})
	if err != nil {
		return nil, fmt.Errorf("change phone: %w", err)
	}

	view := dtos.ToUserView(user)
	return &view, nil
}

// DeleteAccount closes a customer or provider account: the user is soft
// deleted, every session ends and the phone and e-mail can be used again.
func (uc *ProfileUsecase) DeleteAccount(ctx context.Context, userID uuid.UUID, req dtos.DeleteAccountInput) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.Role != enums.RoleCustomer && user.Role != enums.RoleProvider {
		return ErrAccountDeletionNotAllowed
	}
	if err := uc.confirmIdentity(ctx, user, req.Password, req.Code); err != nil {
		return err
	}

	deletedAt := time.Now().UTC()
	deleted, err := events.NewEnvelope(events.UserDeletedV1{ID: user.ID, Role: user.Role, DeletedAt: deletedAt}, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.userRepo.SoftDelete(ctx, user.ID, deletedAt); err != nil {
			return err
		}
		if err := uc.revoker.RevokeUser(ctx, user.ID); err != nil {
			return err
		}
		return uc.outboxRepo.Save(ctx, deleted)
	})
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

func (uc *ProfileUsecase) findUser(ctx context.Context, userID uuid.UUID) (*entities.User, error) {
	user, err := uc.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (uc *ProfileUsecase) confirmIdentity(ctx context.Context, user *entities.User, password, code string) error {
	if code != "" && !user.IsStaff() {
		return uc.otp.Verify(ctx, enums.OTPPurposeConfirmIdentity, user.ID.String(), code)
	}
	if password == "" && !user.HasPassword() && !user.IsStaff() {
		return ErrConfirmationRequired
	}
	return uc.confirmPassword(user, password)
}

func (uc *ProfileUsecase) confirmPassword(user *entities.User, password string) error {
	if !user.HasPassword() {
		return ErrPasswordNotSet
	}
	if password == "" {
		return ErrCurrentPasswordRequired
	}
	matches, _, err := uc.hasher.Verify(password, user.Password)
	if err != nil {
		return err
	}
	if !matches {
		return ErrInvalidCurrentPassword
	}
	return nil
}

func (uc *ProfileUsecase) ensureEmailIsFree(ctx context.Context, email string, owner uuid.UUID) error {
	if email == "" {
		return nil
	}
	existing, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != owner {
		return ErrEmailAlreadyExists
	}
	return nil
}

func (uc *ProfileUsecase) checkNewPhone(ctx context.Context, user *entities.User, raw string) (string, error) {
	newPhone := phone.Normalize(raw)
	if !phone.IsValid(newPhone) {
		return "", customerrors.FieldError("phone", "O número de telefone é inválido (ex: 923000000 ou +244923000000).")
	}
	if newPhone == user.Phone {
		return "", ErrPhoneUnchanged
	}
	existing, err := uc.userRepo.FindByPhone(ctx, newPhone)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", ErrPhoneAlreadyExists
	}
	return newPhone, nil
}

func phoneChangeSubject(userID uuid.UUID, phoneNumber string) string {
	return userID.String() + ":" + phoneNumber
}
