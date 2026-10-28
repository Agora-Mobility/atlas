package seeders

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/infra/security/password"
)

type seeder struct {
	name string
	run  func(ctx context.Context, pool *pgxpool.Pool) error
}

var all = []seeder{
	{name: "users", run: seedUsers},
}

const DevelopmentPassword = "Agora@2026"

func Run(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range all {
		if err := s.run(ctx, pool); err != nil {
			return fmt.Errorf("seeder %s: %w", s.name, err)
		}
	}
	return nil
}

func seedUsers(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `
		INSERT INTO users (id, name, email, phone, password, is_verified, status, role)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, true, 'active', $6)
		ON CONFLICT (id) DO NOTHING`

	passwordHash, err := password.NewArgon2Hasher(password.DefaultParams).Hash(DevelopmentPassword)
	if err != nil {
		return err
	}
	rows := []struct {
		id, name, email, phone, role string
	}{
		{"00000000-0000-0000-0000-0000000000a1", "Admin AGORA", "admin@agora.ao", "", "admin"},
		{"00000000-0000-0000-0000-0000000000a2", "Operador AGORA", "operador@agora.ao", "", "operator"},
		{"00000000-0000-0000-0000-0000000000a3", "Cliente Teste", "", "+244923000001", "customer"},
		{"00000000-0000-0000-0000-0000000000a4", "Prestador Teste", "", "+244923000002", "provider"},
		{"00000000-0000-0000-0000-0000000000a5", "Parceiro Teste", "parceiro@agora.ao", "", "partner"},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx, q, r.id, r.name, r.email, r.phone, passwordHash, r.role); err != nil {
			return err
		}
	}
	return nil
}
