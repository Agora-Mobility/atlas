package containers

import (
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/example/controllers"
	"github.com/Agora-Mobility/atlas/usecases/example"
)

type ExampleModule struct {
	UseCase    *example.ExampleUseCase
	Controller *controllers.ExampleController
	Events     *example.ExampleEventsHandler
}

func newExampleModule(c *Container) *ExampleModule {
	repo := repository.NewExampleRepository(c.Pool, c.Cache)
	uc := example.NewExampleUseCase(repo, c.Outbox, c.TxManager)
	return &ExampleModule{
		UseCase:    uc,
		Controller: controllers.NewExampleController(uc),
		Events:     example.NewExampleEventsHandler(),
	}
}

// Consumers declares the queues of the example module.
func (m *ExampleModule) Consumers() []queue.ConsumerConfig {
	return []queue.ConsumerConfig{
		{
			Queue:       "example.example-created",
			BindingKeys: []string{events.RoutingKey(events.ExampleCreated, 1)},
			Handler:     m.Events.OnExampleCreated,
		},
	}
}
