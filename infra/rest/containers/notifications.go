package containers

import (
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type NotificationsModule struct {
	Events *notifications.NotificationEventsHandler
	Emails *notifications.EmailEventsHandler
}

func newNotificationsModule(c *Container) *NotificationsModule {
	users := repository.NewUserRepository(c.Pool)
	return &NotificationsModule{
		Events: notifications.NewNotificationEventsHandler(users, c.SMS),
		Emails: notifications.NewEmailEventsHandler(users, repository.NewOrganizationRepository(c.Pool), c.Email),
	}
}

func (m *NotificationsModule) Consumers() []queue.ConsumerConfig {
	return []queue.ConsumerConfig{
		{
			Queue:       "notifications.welcome-sms",
			BindingKeys: []string{events.RoutingKey(events.UserPhoneVerified, 1)},
			Handler:     m.Events.OnPhoneVerified,
		},
		{
			Queue:       "notifications.email",
			BindingKeys: emailBindingKeys(),
			Handler:     m.Emails.OnEvent,
		},
	}
}

func emailBindingKeys() []string {
	types := notifications.EmailEventTypes()
	keys := make([]string, 0, len(types))
	for _, eventType := range types {
		keys = append(keys, events.RoutingKey(eventType, 1))
	}
	return keys
}
