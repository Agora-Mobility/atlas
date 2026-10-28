// Package seeders inserts development data. Every seeder must be idempotent.
package seeders

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type seeder struct {
	name string
	run  func(ctx context.Context, pool *pgxpool.Pool) error
}

var all = []seeder{}

// Run executes every seeder in order.
func Run(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range all {
		if err := s.run(ctx, pool); err != nil {
			return fmt.Errorf("seeder %s: %w", s.name, err)
		}
	}
	return nil
}
