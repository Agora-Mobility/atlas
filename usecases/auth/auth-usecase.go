package usecases

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

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
	ErrEmailAlreadyExists     = repository.ErrEmailAlreadyExists
	ErrPhoneAlreadyExists     = repository.ErrPhoneAlreadyExists
	ErrRegistrationNotAllowed = errors.New("client does not allow self registration")
)

var (
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrInvalidCurrentPassword = errors.New("current password is incorrect")
	ErrAccountBlocked         = errors.New("account blocked")
	ErrAccountInactive        = errors.New("account inactive")
	ErrPhoneNotVerified       = errors.New("phone not verified")
	ErrClientNotAllowed       = errors.New("account cannot sign in through this client")
	ErrUserNotFound           = errors.New("user not found")
)

var (
	ErrCurrentPasswordRequired   = errors.New("current password is required")
	ErrPhoneUnchanged            = errors.New("phone is the current one")
	ErrCodeRecentlySent          = services.ErrOTPCooldown
	ErrAccountDeletionNotAllowed = errors.New("staff accounts are removed by an admin")
	ErrPasswordNotSet            = errors.New("account has no password")
	ErrPasswordAlreadySet        = errors.New("account already has a password")
)

const dummyPassword = "agora-dummy-password-for-constant-time-login"

type AuthUsecase struct {
	userRepo      repository.IUserRepository
	outboxRepo    repository.IOutboxRepository
	txManager     repository.ITransactionManager
	hasher        services.IPasswordHasher
	codeSender    *VerificationCodeSender
	sessions      *SessionIssuer
	staff         *StaffSignIn
	revoker       *SessionRevoker
	passwords     passwordReplacer
	dummyHashOnce sync.Once
	dummyHash     string
}

func NewAuthUsecase(
	userRepo repository.IUserRepository,
	outboxRepo repository.IOutboxRepository,
	txManager repository.ITransactionManager,
	hasher services.IPasswordHasher,
	codeSender *VerificationCodeSender,
	sessions *SessionIssuer,
	staff *StaffSignIn,
	revoker *SessionRevoker,
) *AuthUsecase {
	return &AuthUsecase{
		userRepo:   userRepo,
		outboxRepo: outboxRepo,
		txManager:  txManager,
		hasher:     hasher,
		codeSender: codeSender,
		sessions:   sessions,
		staff:      staff,
		revoker:    revoker,
		passwords:  passwordReplacer{userRepo: userRepo, revoker: revoker, txManager: txManager, hasher: hasher},
	}
}

func (uc *AuthUsecase) Register(ctx context.Context, req dtos.RegisterInput, client *entities.Client) (*dtos.UserView, error) {
	role, allowed := client.SignUpRole()
	if !allowed {
		return nil, ErrRegistrationNotAllowed
	}

	phoneNumber := phone.Normalize(req.Phone)
	existing, err := uc.userRepo.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return uc.resumeRegistration(ctx, existing, role)
	}

	user, err := entities.NewSignUpUser(phoneNumber, role)
	if err != nil {
		return nil, err
	}

	registered, err := events.NewEnvelope(events.UserRegisteredV1{
		ID:        user.ID,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.userRepo.Create(ctx, user); err != nil {
			return err
		}
		return uc.outboxRepo.Save(ctx, registered)
	})
	if err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}

	uc.sendVerificationCode(ctx, user)
	view := dtos.ToUserView(user)
	return &view, nil
}

func (uc *AuthUsecase) resumeRegistration(ctx context.Context, user *entities.User, role enums.Role) (*dtos.UserView, error) {
	if user.IsVerified || user.Role != role {
		return nil, ErrPhoneAlreadyExists
	}
	uc.sendVerificationCode(ctx, user)
	view := dtos.ToUserView(user)
	return &view, nil
}

func (uc *AuthUsecase) sendVerificationCode(ctx context.Context, user *entities.User) {
	if err := uc.codeSender.SendSilently(ctx, enums.OTPPurposeVerifyPhone, user.Phone); err != nil {
		logger.FromContext(ctx).Warn("verification code not sent after registration", zap.Error(err))
	}
}

func (uc *AuthUsecase) Login(ctx context.Context, req dtos.LoginInput, client *entities.Client, ip, userAgent string) (*dtos.LoginOutput, error) {
	user, err := uc.resolveUser(ctx, req.Identifier)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.HasPassword() {
		uc.spendPasswordCheck(req.Password)
		return nil, ErrInvalidCredentials
	}

	matches, needsRehash, err := uc.hasher.Verify(req.Password, user.Password)
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, ErrInvalidCredentials
	}
	if err := ensureCanSignIn(user, client); err != nil {
		return nil, err
	}
	if !user.IsVerified {
		return nil, ErrPhoneNotVerified
	}

	if needsRehash {
		uc.upgradePasswordHash(ctx, user, req.Password)
	}

	if user.IsStaff() {
		return uc.staff.Begin(ctx, user, client, req.OrganizationID, ip, userAgent)
	}
	session, err := uc.sessions.Start(ctx, user, client, nil, ip, userAgent)
	if err != nil {
		return nil, err
	}
	return &dtos.LoginOutput{Session: session}, nil
}

func (uc *AuthUsecase) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return uc.revoker.RevokeSession(ctx, sessionID)
}

func (uc *AuthUsecase) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return uc.revoker.RevokeUser(ctx, userID)
}

func (uc *AuthUsecase) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if !user.HasPassword() {
		return ErrPasswordNotSet
	}

	matches, _, err := uc.hasher.Verify(currentPassword, user.Password)
	if err != nil {
		return err
	}
	if !matches {
		return ErrInvalidCurrentPassword
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	return uc.passwords.replace(ctx, user, newPassword, nil)
}

func (uc *AuthUsecase) SetPassword(ctx context.Context, userID uuid.UUID, password string) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.HasPassword() {
		return ErrPasswordAlreadySet
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	hash, err := uc.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err := user.ChangePassword(hash); err != nil {
		return err
	}
	if err := uc.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

func (uc *AuthUsecase) findUser(ctx context.Context, userID uuid.UUID) (*entities.User, error) {
	user, err := uc.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (uc *AuthUsecase) Me(ctx context.Context, userID uuid.UUID) (*dtos.UserView, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	view := dtos.ToUserView(user)
	return &view, nil
}

func ensureIdentityIsFree(ctx context.Context, userRepo repository.IUserRepository, email, phoneNumber string) error {
	if email != "" {
		existing, err := userRepo.FindByEmail(ctx, email)
		if err != nil {
			return err
		}
		if existing != nil {
			return ErrEmailAlreadyExists
		}
	}

	if phoneNumber == "" {
		return nil
	}
	existing, err := userRepo.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrPhoneAlreadyExists
	}
	return nil
}

func (uc *AuthUsecase) resolveUser(ctx context.Context, identifier string) (*entities.User, error) {
	identifier = strings.TrimSpace(identifier)
	if strings.Contains(identifier, "@") {
		return uc.userRepo.FindByEmail(ctx, normalizeEmail(identifier))
	}
	return uc.userRepo.FindByPhone(ctx, phone.Normalize(identifier))
}

func (uc *AuthUsecase) spendPasswordCheck(password string) {
	uc.dummyHashOnce.Do(func() {
		uc.dummyHash, _ = uc.hasher.Hash(dummyPassword)
	})
	if uc.dummyHash != "" {
		_, _, _ = uc.hasher.Verify(password, uc.dummyHash)
	}
}

func (uc *AuthUsecase) upgradePasswordHash(ctx context.Context, user *entities.User, password string) {
	upgraded, err := uc.hasher.Hash(password)
	if err == nil {
		err = user.ChangePassword(upgraded)
	}
	if err == nil {
		err = uc.userRepo.Update(ctx, user)
	}
	if err != nil {
		logger.FromContext(ctx).Warn("password hash upgrade failed", zap.Error(err))
	}
}

func ensureCanSignIn(user *entities.User, client *entities.Client) error {
	if !client.Allows(user.Role) {
		return ErrClientNotAllowed
	}

	if user.IsBlocked() {
		return ErrAccountBlocked
	}

	if user.IsInactive() {
		return ErrAccountInactive
	}

	if !user.CanLogin() {
		return ErrAccountInactive
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
