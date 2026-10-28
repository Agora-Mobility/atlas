package usecases

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type RecoveryUsecase struct {
	userRepo   repository.IUserRepository
	outboxRepo repository.IOutboxRepository
	otp        services.IOTPService
	codeSender *VerificationCodeSender
	passwords  passwordReplacer
}

func NewRecoveryUsecase(
	userRepo repository.IUserRepository,
	refreshTokenRepo repository.IRefreshTokenRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	otp services.IOTPService,
	codeSender *VerificationCodeSender,
) *RecoveryUsecase {
	return &RecoveryUsecase{
		userRepo:   userRepo,
		outboxRepo: outboxRepo,
		otp:        otp,
		codeSender: codeSender,
		passwords:  passwordReplacer{userRepo: userRepo, refreshTokenRepo: refreshTokenRepo, txManager: txManager, hasher: hasher},
	}
}

func (uc *RecoveryUsecase) RequestRecovery(ctx context.Context, req dtos.RecoveryRequestInput, client *entities.Client) error {
	user, err := uc.userRepo.FindByPhone(ctx, phone.Normalize(req.Phone))
	if err != nil {
		return err
	}
	if user == nil || !user.CanLogin() || !client.Allows(user.Role) {
		return nil
	}
	return uc.codeSender.SendSilently(ctx, enums.OTPPurposeResetPassword, user.Phone)
}

func (uc *RecoveryUsecase) ConfirmRecovery(ctx context.Context, req dtos.RecoveryConfirmInput, client *entities.Client) error {
	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}

	phoneNumber := phone.Normalize(req.Phone)
	if err := uc.otp.Verify(ctx, enums.OTPPurposeResetPassword, phoneNumber, req.Code); err != nil {
		return err
	}

	user, err := uc.userRepo.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrInvalidOTPCode
	}
	if !client.Allows(user.Role) {
		return ErrClientNotAllowed
	}

	return uc.passwords.replace(ctx, user, req.NewPassword, func(ctx context.Context) error {
		if user.IsVerified {
			return nil
		}
		return markPhoneVerified(ctx, uc.userRepo, uc.outboxRepo, user)
	})
}
