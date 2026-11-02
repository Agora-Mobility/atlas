package usecases

import (
	"context"
	"fmt"
	"time"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrInvalidOTPCode = services.ErrOTPInvalid
	ErrOTPExpired     = services.ErrOTPExpired
	ErrOTPMaxAttempts = services.ErrOTPMaxAttempts
)

type PhoneVerificationUsecase struct {
	userRepo   repository.IUserRepository
	outboxRepo repository.IOutboxRepository
	txManager  repository.ITransactionManager
	otp        services.IOTPService
	codeSender *VerificationCodeSender
	sessions   *SessionIssuer
}

func NewPhoneVerificationUsecase(
	userRepo repository.IUserRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	otp services.IOTPService,
	codeSender *VerificationCodeSender,
	sessions *SessionIssuer,
) *PhoneVerificationUsecase {
	return &PhoneVerificationUsecase{
		userRepo:   userRepo,
		outboxRepo: outboxRepo,
		txManager:  txManager,
		otp:        otp,
		codeSender: codeSender,
		sessions:   sessions,
	}
}

func (uc *PhoneVerificationUsecase) RequestCode(ctx context.Context, req dtos.PhoneVerificationRequestInput, client *entities.Client) error {
	if _, ok := client.SignUpRole(); !ok {
		return nil
	}
	user, err := uc.userRepo.FindByPhone(ctx, phone.Normalize(req.Phone))
	if err != nil {
		return err
	}
	if user == nil || user.IsVerified || !client.Allows(user.Role) {
		return nil
	}
	return uc.codeSender.SendSilently(ctx, enums.OTPPurposeVerifyPhone, user.Phone)
}

// RequestLoginCode answers the same for every phone, so it does not reveal
// which accounts exist. Staff clients are closed: an SMS code alone must not
// replace the password and the MFA of a staff account.
func (uc *PhoneVerificationUsecase) RequestLoginCode(ctx context.Context, req dtos.LoginCodeRequestInput, client *entities.Client) error {
	if _, ok := client.SignUpRole(); !ok {
		return nil
	}
	user, err := uc.userRepo.FindByPhone(ctx, phone.Normalize(req.Phone))
	if err != nil {
		return err
	}
	if user == nil || ensureCanSignIn(user, client) != nil {
		return nil
	}
	return uc.codeSender.SendSilently(ctx, enums.OTPPurposeLogin, user.Phone)
}

func (uc *PhoneVerificationUsecase) VerifyLoginCode(ctx context.Context, req dtos.LoginCodeVerifyInput, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	return uc.startSessionWithCode(ctx, enums.OTPPurposeLogin, req.Phone, req.Code, client, ip, userAgent)
}

func (uc *PhoneVerificationUsecase) Confirm(ctx context.Context, req dtos.PhoneVerificationConfirmInput, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	return uc.startSessionWithCode(ctx, enums.OTPPurposeVerifyPhone, req.Phone, req.Code, client, ip, userAgent)
}

// startSessionWithCode marks an unverified phone as verified, since a valid
// code proves that the person holds the number.
func (uc *PhoneVerificationUsecase) startSessionWithCode(ctx context.Context, purpose enums.OTPPurpose, rawPhone, code string, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	if _, ok := client.SignUpRole(); !ok {
		return nil, ErrClientNotAllowed
	}
	phoneNumber := phone.Normalize(rawPhone)
	if err := uc.otp.Verify(ctx, purpose, phoneNumber, code); err != nil {
		return nil, err
	}

	user, err := uc.userRepo.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidOTPCode
	}
	if err := ensureCanSignIn(user, client); err != nil {
		return nil, err
	}

	if !user.IsVerified {
		err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
			return markPhoneVerified(ctx, uc.userRepo, uc.outboxRepo, user)
		})
		if err != nil {
			return nil, fmt.Errorf("verify phone: %w", err)
		}
	}

	return uc.sessions.Start(ctx, user, client, nil, ip, userAgent)
}

func markPhoneVerified(ctx context.Context, userRepo repository.IUserRepository, outboxRepo repository.IOutboxRepository, user *entities.User) error {
	user.MarkVerified()
	verified, err := events.NewEnvelope(events.UserPhoneVerifiedV1{
		ID:         user.ID,
		VerifiedAt: time.Now().UTC(),
	}, logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	if err := userRepo.Update(ctx, user); err != nil {
		return err
	}
	return outboxRepo.Save(ctx, verified)
}
