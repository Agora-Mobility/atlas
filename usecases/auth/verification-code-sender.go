package usecases

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var smsTemplateByPurpose = map[enums.OTPPurpose]string{
	enums.OTPPurposeVerifyPhone:   "Código de verificação: %s. Válido por %d min. Não o partilhe.",
	enums.OTPPurposeResetPassword: "Código de recuperação: %s. Válido por %d min. Não o partilhe.",
}

type VerificationCodeSender struct {
	otp     services.IOTPService
	sms     services.ISMSSender
	codeTTL time.Duration
}

func NewVerificationCodeSender(otp services.IOTPService, sms services.ISMSSender, codeTTL time.Duration) *VerificationCodeSender {
	return &VerificationCodeSender{otp: otp, sms: sms, codeTTL: codeTTL}
}

func (s *VerificationCodeSender) Send(ctx context.Context, purpose enums.OTPPurpose, phone string) error {
	code, err := s.otp.Issue(ctx, purpose, phone)
	if err != nil {
		return err
	}
	return s.sms.Send(ctx, phone, fmt.Sprintf(smsTemplateByPurpose[purpose], code, s.validityMinutes()))
}

func (s *VerificationCodeSender) SendSilently(ctx context.Context, purpose enums.OTPPurpose, phone string) error {
	err := s.Send(ctx, purpose, phone)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, services.ErrOTPCooldown):
		logger.FromContext(ctx).Warn("verification code not sent: resend cooldown", zap.String("purpose", purpose.String()))
		return nil
	case errors.Is(err, services.ErrSMSUnavailable):
		logger.FromContext(ctx).Error("verification code not sent: sms provider failed", zap.String("purpose", purpose.String()), zap.Error(err))
		return nil
	default:
		return err
	}
}

func (s *VerificationCodeSender) validityMinutes() int {
	return max(1, int(math.Ceil(s.codeTTL.Minutes())))
}
