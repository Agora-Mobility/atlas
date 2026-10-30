package usecases_test

import (
	"context"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	servicemocks "github.com/Agora-Mobility/atlas/domain/services/mocks"
	"github.com/Agora-Mobility/atlas/shared/enums"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

const unicodeSingleSMSLimit = 70

func sentText(t *testing.T, purpose enums.OTPPurpose, ttl time.Duration) string {
	t.Helper()
	ctrl := gomock.NewController(t)
	otp := servicemocks.NewMockIOTPService(ctrl)
	sms := servicemocks.NewMockISMSSender(ctrl)
	var text string
	otp.EXPECT().Issue(gomock.Any(), purpose, phone).Return("726564", nil)
	sms.EXPECT().Send(gomock.Any(), phone, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, content string) error { text = content; return nil })

	require.NoError(t, usecases.NewVerificationCodeSender(otp, sms, ttl).Send(context.Background(), purpose, phone))
	return text
}

func TestVerificationCodeSender_Texts(t *testing.T) {
	cases := []struct {
		purpose enums.OTPPurpose
		ttl     time.Duration
		want    string
	}{
		{enums.OTPPurposeVerifyPhone, 5 * time.Minute, "Código de verificação: 726564. Válido por 5 min. Não o partilhe."},
		{enums.OTPPurposeResetPassword, 10 * time.Minute, "Código de recuperação: 726564. Válido por 10 min. Não o partilhe."},
		{enums.OTPPurposeChangePhone, 10 * time.Minute, "Código de novo número: 726564. Válido por 10 min. Não o partilhe."},
		{enums.OTPPurposeVerifyPhone, 90 * time.Second, "Código de verificação: 726564. Válido por 2 min. Não o partilhe."},
		{enums.OTPPurposeVerifyPhone, 30 * time.Second, "Código de verificação: 726564. Válido por 1 min. Não o partilhe."},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			text := sentText(t, tc.purpose, tc.ttl)

			require.Equal(t, tc.want, text)
			require.LessOrEqual(t, utf8.RuneCountInString(text), unicodeSingleSMSLimit)
		})
	}
}
