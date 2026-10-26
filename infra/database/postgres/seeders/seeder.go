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

var all = []seeder{
	{name: "examples", run: seedExamples},
}

// Run executes every seeder in order.
func Run(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range all {
		if err := s.run(ctx, pool); err != nil {
			return fmt.Errorf("seeder %s: %w", s.name, err)
		}
	}
	return nil
}

func seedExamples(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `
		INSERT INTO examples (id, name, description, location)
		VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4, $5), 4326))
		ON CONFLICT (id) DO NOTHING`

	rows := []struct {
		id, name, description string
		lng, lat              float64
	}{
		{"00000000-0000-0000-0000-000000000001", "Marginal de Luanda", "Seed example", 13.2343, -8.8130},
		{"00000000-0000-0000-0000-000000000002", "Talatona", "Seed example", 13.1855, -8.9167},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx, q, r.id, r.name, r.description, r.lng, r.lat); err != nil {
			return err
		}
	}
	return nil
}
