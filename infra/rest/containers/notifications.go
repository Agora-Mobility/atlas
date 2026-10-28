package containers

import (
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type NotificationsModule struct {
	Events *notifications.NotificationEventsHandler
}

func newNotificationsModule(c *Container) *NotificationsModule {
	return &NotificationsModule{
		Events: notifications.NewNotificationEventsHandler(repository.NewUserRepository(c.Pool), c.SMS),
	}
}

func (m *NotificationsModule) Consumers() []queue.ConsumerConfig {
	return []queue.ConsumerConfig{
		{
			Queue:       "notifications.welcome-sms",
			BindingKeys: []string{events.RoutingKey(events.UserPhoneVerified, 1)},
			Handler:     m.Events.OnPhoneVerified,
		},
	}
}
