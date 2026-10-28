package notifications

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const welcomeTemplate = "Bem-vindo(a), %s! A sua conta está confirmada."

type NotificationEventsHandler struct {
	userRepo repository.IUserRepository
	sms      services.ISMSSender
}

func NewNotificationEventsHandler(userRepo repository.IUserRepository, sms services.ISMSSender) *NotificationEventsHandler {
	return &NotificationEventsHandler{userRepo: userRepo, sms: sms}
}

func (h *NotificationEventsHandler) OnPhoneVerified(ctx context.Context, env events.Envelope) error {
	var evt events.UserPhoneVerifiedV1
	if err := env.DecodePayload(&evt); err != nil {
		return err
	}

	user, err := h.userRepo.FindByID(ctx, evt.ID)
	if err != nil {
		return fmt.Errorf("find verified user: %w", err)
	}
	if user == nil || user.Phone == "" {
		logger.FromContext(ctx).Warn("welcome sms skipped: user not found or without phone", zap.String("user_id", evt.ID.String()))
		return nil
	}

	if err := h.sms.Send(ctx, user.Phone, fmt.Sprintf(welcomeTemplate, firstName(user.Name))); err != nil {
		return fmt.Errorf("send welcome sms to user %s: %w", evt.ID, err)
	}
	return nil
}

func firstName(fullName string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(fullName), " ")
	return name
}
