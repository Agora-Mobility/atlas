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
	enums.OTPPurposeVerifyPhone:     "Código de verificação: %s. Válido por %d min. Não o partilhe.",
	enums.OTPPurposeLogin:           "Código de acesso: %s. Válido por %d min. Não o partilhe.",
	enums.OTPPurposeResetPassword:   "Recovery code: %s. Valid for %d min. Do not share it.",
	enums.OTPPurposeChangePhone:     "Código de novo número: %s. Válido por %d min. Não o partilhe.",
	enums.OTPPurposeInvitation:      "Código de activação: %s. Válido por %d min. Não o partilhe.",
	enums.OTPPurposeConfirmIdentity: "Código de confirmação: %s. Válido por %d min. Não o partilhe.",
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
	return s.SendTo(ctx, purpose, phone, phone)
}

// SendTo issues a code for subject and sends it to phone.
func (s *VerificationCodeSender) SendTo(ctx context.Context, purpose enums.OTPPurpose, subject, phone string) error {
	code, err := s.otp.Issue(ctx, purpose, subject)
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
