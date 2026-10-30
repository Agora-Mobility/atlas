package usecases

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

var (
	ErrInvalidMFAToken      = errors.New("mfa token is invalid or expired")
	ErrInvalidMFACode       = errors.New("mfa code is incorrect")
	ErrMFASetupRequired     = errors.New("mfa must be set up first")
	ErrMFAAlreadyConfigured = errors.New("mfa is already set up")
	ErrMFANotConfigured     = errors.New("mfa is not set up")
)

const (
	recoveryCodeCount        = 10
	recoveryCodeGroups       = 3
	recoveryCodeGroupLength  = 4
	recoveryAlphabet         = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	auditActionRecoveryUsed  = "mfa.recovery-code-used"
	auditActionRecoveryReset = "mfa.recovery-codes-regenerated"
)

const (
	MFAStatusSetupRequired = "setup_required"
	MFAStatusCodeRequired  = "code_required"
)

type StaffSignIn struct {
	userRepo       repository.IUserRepository
	membershipRepo repository.IMembershipRepository
	mfaRepo        repository.IUserMFARepository
	challenges     services.IMFAChallengeStore
	totp           services.ITOTP
	sealer         services.ISecretSealer
	opaqueTokens   services.IOpaqueTokenGenerator
	auditRepo      repository.IAuditLogRepository
	sessions       *SessionIssuer
	challengeTTL   time.Duration
}

func NewStaffSignIn(
	userRepo repository.IUserRepository,
	membershipRepo repository.IMembershipRepository,
	mfaRepo repository.IUserMFARepository,
	challenges services.IMFAChallengeStore,
	totp services.ITOTP,
	sealer services.ISecretSealer,
	opaqueTokens services.IOpaqueTokenGenerator,
	auditRepo repository.IAuditLogRepository,
	sessions *SessionIssuer,
	challengeTTL time.Duration,
) *StaffSignIn {
	return &StaffSignIn{
		userRepo:       userRepo,
		membershipRepo: membershipRepo,
		mfaRepo:        mfaRepo,
		challenges:     challenges,
		totp:           totp,
		sealer:         sealer,
		opaqueTokens:   opaqueTokens,
		auditRepo:      auditRepo,
		sessions:       sessions,
		challengeTTL:   challengeTTL,
	}
}

func (s *StaffSignIn) Begin(ctx context.Context, user *entities.User, client *entities.Client, organizationID *uuid.UUID, ip, userAgent string) (*dtos.LoginOutput, error) {
	scope, ok := client.StaffScope()
	if !ok {
		return nil, ErrClientNotAllowed
	}
	ctx = tenant.WithUser(ctx, user.ID)
	memberships, err := s.membershipRepo.ListActiveForSignIn(ctx, user.ID, scope)
	if err != nil {
		return nil, err
	}
	membership := pickMembership(memberships, organizationID)
	if membership == nil {
		return nil, ErrInvalidCredentials
	}
	ctx = tenant.WithOrganization(ctx, membership.OrganizationID)
	access, err := s.membershipRepo.Access(ctx, membership.ID)
	if err != nil {
		return nil, err
	}
	if access == nil || !access.IsActive() {
		return nil, ErrInvalidCredentials
	}

	mfa, err := s.mfaRepo.Find(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if !access.RequiresMFA && !mfa.IsConfirmed() {
		session, err := s.sessions.Start(ctx, user, client, access, ip, userAgent)
		if err != nil {
			return nil, err
		}
		return &dtos.LoginOutput{Session: session}, nil
	}

	token, err := s.challenges.Create(ctx, services.MFAChallenge{
		UserID: user.ID, ClientID: client.ID, MembershipID: membership.ID, IPAddress: ip, UserAgent: userAgent,
	})
	if err != nil {
		return nil, err
	}
	status := MFAStatusSetupRequired
	if mfa.IsConfirmed() {
		status = MFAStatusCodeRequired
	}
	return &dtos.LoginOutput{MFA: &dtos.MFAChallengeOutput{
		Status: status, MFAToken: token, ExpiresIn: int64(s.challengeTTL.Seconds()),
	}}, nil
}

func (s *StaffSignIn) Setup(ctx context.Context, client *entities.Client, req dtos.MFATokenInput) (*dtos.MFASetupOutput, error) {
	challenge, err := s.challenge(ctx, client, req.MFAToken)
	if err != nil {
		return nil, err
	}
	current, err := s.mfaRepo.Find(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if current.IsConfirmed() {
		return nil, ErrMFAAlreadyConfigured
	}
	user, err := s.userRepo.FindByID(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidMFAToken
	}

	secret, err := s.totp.NewSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal(secret)
	if err != nil {
		return nil, err
	}
	if err := s.mfaRepo.SavePending(ctx, user.ID, sealed, time.Now().UTC()); err != nil {
		return nil, err
	}
	account := user.Email
	if account == "" {
		account = user.Phone
	}
	return &dtos.MFASetupOutput{Secret: secret, OTPAuthURI: s.totp.URI(secret, account)}, nil
}

func (s *StaffSignIn) Verify(ctx context.Context, client *entities.Client, req dtos.MFAVerifyInput) (*dtos.SessionOutput, error) {
	challenge, err := s.challenge(ctx, client, req.MFAToken)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithUser(ctx, challenge.UserID)
	mfa, err := s.mfaRepo.Find(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if mfa == nil {
		return nil, ErrMFASetupRequired
	}
	firstConfirmation := !mfa.IsConfirmed()
	ok, err := s.useCode(ctx, challenge.UserID, mfa, req.Code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.failure(ctx, req.MFAToken)
	}
	session, err := s.finish(ctx, client, challenge, req.MFAToken)
	if err != nil || !firstConfirmation {
		return session, err
	}
	codes, err := s.issueRecoveryCodes(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	session.MFARecoveryCodes = codes
	return session, nil
}

func (s *StaffSignIn) Recover(ctx context.Context, client *entities.Client, req dtos.MFARecoverInput, ip, userAgent string) (*dtos.SessionOutput, error) {
	challenge, err := s.challenge(ctx, client, req.MFAToken)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithUser(ctx, challenge.UserID)
	mfa, err := s.mfaRepo.Find(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if !mfa.IsConfirmed() {
		return nil, ErrMFASetupRequired
	}
	used, err := s.mfaRepo.UseRecoveryCode(ctx, challenge.UserID, s.opaqueTokens.Hash(normalizeRecoveryCode(req.RecoveryCode)), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !used {
		return nil, s.failure(ctx, req.MFAToken)
	}
	left, err := s.mfaRepo.CountUnusedRecoveryCodes(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.auditUser(ctx, challenge.UserID, auditActionRecoveryUsed, map[string]int64{"codes_left": left}, ip, userAgent); err != nil {
		return nil, err
	}
	return s.finish(ctx, client, challenge, req.MFAToken)
}

func (s *StaffSignIn) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, req dtos.MFACodeInput, ip, userAgent string) (*dtos.RecoveryCodesOutput, error) {
	mfa, err := s.mfaRepo.Find(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !mfa.IsConfirmed() {
		return nil, ErrMFANotConfigured
	}
	ok, err := s.useCode(ctx, userID, mfa, req.Code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidMFACode
	}
	codes, err := s.issueRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.auditUser(ctx, userID, auditActionRecoveryReset, nil, ip, userAgent); err != nil {
		return nil, err
	}
	return &dtos.RecoveryCodesOutput{RecoveryCodes: codes}, nil
}

func (s *StaffSignIn) useCode(ctx context.Context, userID uuid.UUID, mfa *entities.UserMFA, code string) (bool, error) {
	secret, err := s.sealer.Open(mfa.Secret)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	step, ok := s.totp.Match(secret, code, now)
	if !ok {
		return false, nil
	}
	return s.mfaRepo.UseStep(ctx, userID, step, now)
}

func (s *StaffSignIn) failure(ctx context.Context, token string) error {
	exhausted, err := s.challenges.RecordFailure(ctx, token)
	if err != nil {
		return err
	}
	if exhausted {
		return ErrInvalidMFAToken
	}
	return ErrInvalidMFACode
}

func (s *StaffSignIn) issueRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, s.opaqueTokens.Hash(normalizeRecoveryCode(c)))
	}
	if err := s.mfaRepo.ReplaceRecoveryCodes(ctx, userID, hashes, time.Now().UTC()); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *StaffSignIn) auditUser(ctx context.Context, userID uuid.UUID, action string, after any, ip, userAgent string) error {
	actor := entities.Actor{ID: &userID, Role: enums.RoleStaff, IPAddress: ip, UserAgent: userAgent}
	entry, err := entities.NewAuditEntry(actor, action, auditEntityUser, userID.String(), nil, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return s.auditRepo.Record(ctx, entry)
}

func (s *StaffSignIn) finish(ctx context.Context, client *entities.Client, challenge *services.MFAChallenge, token string) (*dtos.SessionOutput, error) {
	if err := s.challenges.Delete(ctx, token); err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByID(ctx, challenge.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidMFAToken
	}
	if err := ensureCanSignIn(user, client); err != nil {
		return nil, err
	}
	access, err := s.membershipRepo.Access(ctx, challenge.MembershipID)
	if err != nil {
		return nil, err
	}
	if access == nil || !access.IsActive() || access.UserID != user.ID {
		return nil, ErrInvalidMFAToken
	}
	return s.sessions.Start(ctx, user, client, access, challenge.IPAddress, challenge.UserAgent)
}

func (s *StaffSignIn) challenge(ctx context.Context, client *entities.Client, token string) (*services.MFAChallenge, error) {
	challenge, err := s.challenges.Find(ctx, token)
	if errors.Is(err, services.ErrMFAChallengeNotFound) {
		return nil, ErrInvalidMFAToken
	}
	if err != nil {
		return nil, err
	}
	if challenge.ClientID != client.ID {
		return nil, ErrInvalidMFAToken
	}
	return challenge, nil
}

func newRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	raw := make([]byte, recoveryCodeGroups*recoveryCodeGroupLength)
	for range recoveryCodeCount {
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		var b strings.Builder
		for i, v := range raw {
			if i > 0 && i%recoveryCodeGroupLength == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
		}
		codes = append(codes, b.String())
	}
	return codes, nil
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}

func pickMembership(memberships []*entities.Membership, organizationID *uuid.UUID) *entities.Membership {
	for _, m := range memberships {
		if organizationID == nil || m.OrganizationID == *organizationID {
			return m
		}
	}
	return nil
}
