package sms

import (
	"context"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type LogSender struct{}

var _ services.ISMSSender = LogSender{}

func (LogSender) Send(ctx context.Context, phone, message string) error {
	logger.FromContext(ctx).Info("sms sent to log (development only)", zap.String("phone", phone), zap.String("text", message))
	return nil
}
