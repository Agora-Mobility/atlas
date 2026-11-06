package notifications

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/emails"
)

type memberMessage func(name, organization string) emails.Message

type accountMessage func(name string) emails.Message

var memberMessages = map[string]memberMessage{
	events.MemberActivated:    emails.MemberActivated,
	events.MemberRolesChanged: emails.MemberRolesChanged,
	events.MemberSuspended:    emails.MemberSuspended,
	events.MemberReactivated:  emails.MemberReactivated,
	events.MemberDeactivated:  emails.MemberDeactivated,
}

var accountMessages = map[string]accountMessage{
	events.UserBlocked:   emails.AccountBlocked,
	events.UserUnblocked: emails.AccountUnblocked,
}

type EmailEventsHandler struct {
	userRepo repository.IUserRepository
	orgRepo  repository.IOrganizationRepository
	email    services.IEmailSender
}

func NewEmailEventsHandler(userRepo repository.IUserRepository, orgRepo repository.IOrganizationRepository, email services.IEmailSender) *EmailEventsHandler {
	return &EmailEventsHandler{userRepo: userRepo, orgRepo: orgRepo, email: email}
}

func EmailEventTypes() []string {
	types := make([]string, 0, len(memberMessages)+len(accountMessages))
	for eventType := range memberMessages {
		types = append(types, eventType)
	}
	for eventType := range accountMessages {
		types = append(types, eventType)
	}
	return types
}

func (h *EmailEventsHandler) OnEvent(ctx context.Context, env events.Envelope) error {
	if build, ok := memberMessages[env.EventType]; ok {
		return h.notifyMember(ctx, env, build)
	}
	if build, ok := accountMessages[env.EventType]; ok {
		return h.notifyAccount(ctx, env, build)
	}
	logger.FromContext(ctx).Warn("email skipped: no message for the event", zap.String("event_type", env.EventType))
	return nil
}

func (h *EmailEventsHandler) notifyMember(ctx context.Context, env events.Envelope, build memberMessage) error {
	var change events.MemberChange
	if err := env.DecodePayload(&change); err != nil {
		return err
	}
	org, err := h.orgRepo.FindByID(ctx, change.OrganizationID)
	if err != nil {
		return fmt.Errorf("find organization of %s: %w", env.EventType, err)
	}
	if org == nil {
		logger.FromContext(ctx).Warn("email skipped: organization not found", zap.String("event_type", env.EventType))
		return nil
	}
	return h.send(ctx, env, change.UserID, func(name string) emails.Message { return build(name, org.Name) })
}

func (h *EmailEventsHandler) notifyAccount(ctx context.Context, env events.Envelope, build accountMessage) error {
	var account struct {
		ID uuid.UUID `json:"id"`
	}
	if err := env.DecodePayload(&account); err != nil {
		return err
	}
	return h.send(ctx, env, account.ID, build)
}

func (h *EmailEventsHandler) send(ctx context.Context, env events.Envelope, userID uuid.UUID, build accountMessage) error {
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("find user of %s: %w", env.EventType, err)
	}
	if user == nil || user.Email == "" {
		return nil
	}
	message, err := build(user.Name).Render(user.Email, env.EventType+"/"+env.EventID.String())
	if err != nil {
		return err
	}
	if err := h.email.Send(ctx, message); err != nil {
		return fmt.Errorf("send %s email to user %s: %w", env.EventType, user.ID, err)
	}
	return nil
}
