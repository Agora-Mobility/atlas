package email

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/resend/resend-go/v3"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type ResendSender struct {
	client  *resend.Client
	from    string
	replyTo string
}

var _ services.IEmailSender = (*ResendSender)(nil)

func NewResendSender(apiKey, baseURL, from, replyTo string, timeout time.Duration) (*ResendSender, error) {
	endpoint, err := url.Parse(strings.TrimSuffix(baseURL, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("parse resend base url: %w", err)
	}
	client := resend.NewCustomClient(&http.Client{Timeout: timeout}, apiKey)
	client.BaseURL = endpoint
	return &ResendSender{client: client, from: from, replyTo: replyTo}, nil
}

func (s *ResendSender) Send(ctx context.Context, email services.Email) error {
	request := &resend.SendEmailRequest{
		From:    s.from,
		To:      []string{email.To},
		Subject: email.Subject,
		Html:    email.HTML,
		Text:    email.Text,
		ReplyTo: s.replyTo,
	}
	sent, err := s.client.Emails.SendWithOptions(ctx, request, &resend.SendEmailOptions{IdempotencyKey: email.IdempotencyKey})
	if err != nil {
		return fmt.Errorf("%w: resend: %w", services.ErrEmailUnavailable, err)
	}

	logger.FromContext(ctx).Info("email sent",
		zap.String("provider", "resend"),
		zap.String("to", maskAddress(email.To)),
		zap.String("message_id", sent.Id),
	)
	return nil
}

func maskAddress(address string) string {
	local, domain, found := strings.Cut(address, "@")
	if !found || local == "" {
		return "***"
	}
	return local[:1] + "***@" + domain
}
