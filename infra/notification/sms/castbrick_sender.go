package sms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/IldySilva/castbrick-go/castbrick"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
)

var failureReasonByStatus = map[int]string{
	http.StatusUnauthorized:        "invalid or revoked api key",
	http.StatusPaymentRequired:     "insufficient credits",
	http.StatusUnprocessableEntity: "rejected by provider validation",
	http.StatusTooManyRequests:     "provider rate limit",
}

type CastBrickSender struct {
	client   *castbrick.CastBrick
	senderID string
}

var _ services.ISMSSender = (*CastBrickSender)(nil)

func NewCastBrickSender(apiKey, baseURL, senderID string, timeout time.Duration) *CastBrickSender {
	return &CastBrickSender{
		client:   castbrick.NewWithOptions(apiKey, baseURL, tracing.HTTPClient(timeout)),
		senderID: senderID,
	}
}

func (s *CastBrickSender) Send(ctx context.Context, phone, message string) error {
	result, err := s.client.SMS.Send(ctx, castbrick.SendSmsOptions{
		To:       []string{phone},
		Content:  message,
		SenderID: s.senderID,
	})
	if err != nil {
		return fmt.Errorf("%w: castbrick: %s", services.ErrSMSUnavailable, failureReason(err))
	}
	if result.Error != "" {
		return fmt.Errorf("%w: castbrick: %s", services.ErrSMSUnavailable, result.Error)
	}

	logger.FromContext(ctx).Info("sms sent",
		zap.String("provider", "castbrick"),
		zap.String("phone", maskPhone(phone)),
		zap.String("message_id", result.MessageID),
		zap.String("status", result.Status),
	)
	return nil
}

func failureReason(err error) string {
	var apiErr *castbrick.APIError
	if !errors.As(err, &apiErr) {
		return err.Error()
	}
	if reason, ok := failureReasonByStatus[apiErr.StatusCode]; ok {
		return fmt.Sprintf("status %d (%s): %s", apiErr.StatusCode, reason, apiErr.Body)
	}
	return fmt.Sprintf("status %d: %s", apiErr.StatusCode, apiErr.Body)
}

func maskPhone(phone string) string {
	const visibleDigits = 3
	if len(phone) <= visibleDigits {
		return phone
	}
	masked := []byte(phone)
	for i := 4; i < len(masked)-visibleDigits; i++ {
		masked[i] = '*'
	}
	return string(masked)
}
