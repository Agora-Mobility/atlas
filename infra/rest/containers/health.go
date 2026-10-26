package containers

import (
	"context"
	"errors"

	"github.com/Agora-Mobility/atlas/infra/rest/health/controllers"
)

type HealthModule struct {
	Controller *controllers.HealthController
}

func newHealthModule(c *Container, version string) *HealthModule {
	checks := map[string]controllers.Check{
		"postgres": func(ctx context.Context) error { return c.Pool.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return c.Redis.Ping(ctx).Err() },
		"rabbitmq": func(context.Context) error {
			if !c.Queue.Healthy() {
				return errors.New("connection closed")
			}
			return nil
		},
	}
	return &HealthModule{Controller: controllers.NewHealthController(version, checks)}
}
