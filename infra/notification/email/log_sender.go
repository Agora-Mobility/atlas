package email

import (
	"context"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type LogSender struct{}

var _ services.IEmailSender = LogSender{}

func (LogSender) Send(ctx context.Context, email services.Email) error {
	logger.FromContext(ctx).Info("email sent to log (development only)",
		zap.String("to", email.To),
		zap.String("subject", email.Subject),
		zap.String("text", email.Text),
	)
	return nil
}
