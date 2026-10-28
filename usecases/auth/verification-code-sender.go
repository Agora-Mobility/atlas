package usecases

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var smsTemplateByPurpose = map[enums.OTPPurpose]string{
	enums.OTPPurposeVerifyPhone:   "AGORA: o seu código de verificação é %s. Não o partilhe com ninguém.",
	enums.OTPPurposeResetPassword: "AGORA: o seu código para redefinir a palavra-passe é %s. Não o partilhe com ninguém.",
}

type VerificationCodeSender struct {
	otp services.IOTPService
	sms services.ISMSSender
}

func NewVerificationCodeSender(otp services.IOTPService, sms services.ISMSSender) *VerificationCodeSender {
	return &VerificationCodeSender{otp: otp, sms: sms}
}

func (s *VerificationCodeSender) Send(ctx context.Context, purpose enums.OTPPurpose, phone string) error {
	code, err := s.otp.Issue(ctx, purpose, phone)
	if err != nil {
		return err
	}
	return s.sms.Send(ctx, phone, fmt.Sprintf(smsTemplateByPurpose[purpose], code))
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
