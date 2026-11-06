package usecases

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
	"github.com/Agora-Mobility/atlas/usecases/emails"
)

type RecoveryUsecase struct {
	userRepo       repository.IUserRepository
	outboxRepo     repository.IOutboxRepository
	otp            services.IOTPService
	codeSender     *VerificationCodeSender
	passwords      passwordReplacer
	hasher         services.IPasswordHasher
	tickets        services.IRecoveryTicketService
	email          services.IEmailSender
	resendCooldown time.Duration
}

func NewRecoveryUsecase(
	userRepo repository.IUserRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	otp services.IOTPService,
	codeSender *VerificationCodeSender,
	revoker *SessionRevoker,
	tickets services.IRecoveryTicketService,
	email services.IEmailSender,
	resendCooldown time.Duration,
) *RecoveryUsecase {
	return &RecoveryUsecase{
		userRepo:       userRepo,
		outboxRepo:     outboxRepo,
		otp:            otp,
		codeSender:     codeSender,
		passwords:      passwordReplacer{userRepo: userRepo, revoker: revoker, txManager: txManager, hasher: hasher},
		hasher:         hasher,
		tickets:        tickets,
		email:          email,
		resendCooldown: resendCooldown,
	}
}

func (uc *RecoveryUsecase) ResendAfterSeconds() int {
	return int(math.Ceil(uc.resendCooldown.Seconds()))
}

func (uc *RecoveryUsecase) RequestRecovery(ctx context.Context, req dtos.RecoveryRequestInput, client *entities.Client) error {
	subject := recoverySubject(req.Phone, req.Email)
	user, err := uc.findAccount(ctx, subject)
	if err != nil {
		return err
	}
	if user == nil || !user.CanLogin() || !client.Allows(user.Role) {
		return nil
	}
	if isEmailSubject(subject) {
		return uc.sendCodeByEmail(ctx, user)
	}
	return uc.codeSender.SendSilently(ctx, enums.OTPPurposeResetPassword, user.Phone)
}

func (uc *RecoveryUsecase) VerifyRecovery(ctx context.Context, req dtos.RecoveryVerifyInput, client *entities.Client) (*dtos.RecoveryVerifyOutput, error) {
	subject := recoverySubject(req.Phone, req.Email)
	if err := uc.otp.Verify(ctx, enums.OTPPurposeResetPassword, subject, req.Code); err != nil {
		return nil, err
	}

	user, err := uc.findAccount(ctx, subject)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidOTPCode
	}
	if !client.Allows(user.Role) {
		return nil, ErrClientNotAllowed
	}

	ticket, err := uc.tickets.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &dtos.RecoveryVerifyOutput{RecoveryToken: ticket}, nil
}

func (uc *RecoveryUsecase) ConfirmRecovery(ctx context.Context, req dtos.RecoveryConfirmInput, client *entities.Client) error {
	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}

	subject, err := uc.tickets.Peek(ctx, req.RecoveryToken)
	if err != nil {
		return err
	}

	user, err := uc.findAccount(ctx, subject)
	if err != nil {
		return err
	}
	if user == nil {
		return services.ErrRecoveryTicketInvalid
	}
	if !client.Allows(user.Role) {
		return ErrClientNotAllowed
	}
	if err := uc.rejectCurrentPassword(user, req.NewPassword); err != nil {
		return err
	}

	if _, err := uc.tickets.Consume(ctx, req.RecoveryToken); err != nil {
		return err
	}

	return uc.passwords.replace(ctx, user, req.NewPassword, func(ctx context.Context) error {
		if user.IsVerified || isEmailSubject(subject) {
			return nil
		}
		return markPhoneVerified(ctx, uc.userRepo, uc.outboxRepo, user)
	})
}

func (uc *RecoveryUsecase) findAccount(ctx context.Context, subject string) (*entities.User, error) {
	if isEmailSubject(subject) {
		return uc.userRepo.FindByEmail(ctx, subject)
	}
	return uc.userRepo.FindByPhone(ctx, subject)
}

func (uc *RecoveryUsecase) rejectCurrentPassword(user *entities.User, password string) error {
	if !user.HasPassword() {
		return nil
	}
	matches, _, err := uc.hasher.Verify(password, user.Password)
	if err != nil {
		return err
	}
	if matches {
		return ErrPasswordReused
	}
	return nil
}

func (uc *RecoveryUsecase) sendCodeByEmail(ctx context.Context, user *entities.User) error {
	code, err := uc.otp.Issue(ctx, enums.OTPPurposeResetPassword, user.Email)
	if errors.Is(err, services.ErrOTPCooldown) {
		logger.FromContext(ctx).Warn("recovery code not sent: resend cooldown")
		return nil
	}
	if err != nil {
		return err
	}

	message, err := emails.PasswordRecoveryCode(user.Name, code, uc.codeSender.validityMinutes()).
		Render(user.Email, "recovery-code/"+uuid.NewString())
	if err != nil {
		return err
	}
	if err := uc.email.Send(ctx, message); err != nil {
		logger.FromContext(ctx).Error("recovery code not sent: email provider failed", zap.Error(err))
	}
	return nil
}

func recoverySubject(phoneNumber, email string) string {
	if email != "" {
		return normalizeEmail(email)
	}
	return phone.Normalize(phoneNumber)
}

func isEmailSubject(subject string) bool {
	return strings.Contains(subject, "@")
}
