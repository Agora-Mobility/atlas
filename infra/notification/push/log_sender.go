package push

import (
	"context"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type LogSender struct{}

var _ services.IPushSender = LogSender{}

func (LogSender) Send(ctx context.Context, message services.PushMessage) (string, error) {
	logger.FromContext(ctx).Info("push not sent, fcm is not configured", zap.String("title", message.Title))
	return "", nil
}

type LiveActivityLogSender struct{}

var _ services.ILiveActivitySender = LiveActivityLogSender{}

func (LiveActivityLogSender) Send(ctx context.Context, message services.LiveActivityMessage) (string, error) {
	logger.FromContext(ctx).Info("live activity push not sent, apns is not configured", zap.String("event", message.Event))
	return "", nil
}
