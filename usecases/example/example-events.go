package example

import (
	"context"

	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/events"
)

// ExampleEventsHandler reacts to example events. It only logs: it exists to
// prove the outbox → RabbitMQ → consumer path.
type ExampleEventsHandler struct{}

func NewExampleEventsHandler() *ExampleEventsHandler {
	return &ExampleEventsHandler{}
}

func (h *ExampleEventsHandler) OnExampleCreated(ctx context.Context, env events.Envelope) error {
	var evt events.ExampleCreatedV1
	if err := env.DecodePayload(&evt); err != nil {
		return err
	}
	logger.Info(ctx, "example created event received",
		zap.String("event_id", env.EventID.String()),
		zap.String("example_id", evt.ID.String()),
		zap.String("name", evt.Name),
	)
	return nil
}
