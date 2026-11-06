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
	{name: "partners", run: seedPartners},
	{name: "memberships", run: seedMemberships},
	{name: "dispatch", run: seedDispatch},
}

const partnerOrganizationID = "00000000-0000-0000-0000-0000000000b1"

const DevelopmentPassword = "Agora@2026"

func Run(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range all {
		if err := s.run(ctx, pool); err != nil {
			return fmt.Errorf("seeder %s: %w", s.name, err)
		}
	}
	return nil
}

const luandaApproximateArea = "MULTIPOLYGON(((12.9 -9.4,13.8 -9.4,13.8 -8.5,12.9 -8.5,12.9 -9.4)))"

func seedDispatch(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO service_zones (id, country_code, name, geom, timezone, currency)
		  VALUES ('00000000-0000-0000-0000-0000000000c1', 'AO', 'Luanda', ST_GeogFromText($1), 'Africa/Luanda', 'AOA')
		  ON CONFLICT (id) DO NOTHING`, []any{luandaApproximateArea}},
		{`INSERT INTO service_tariffs (service_id, currency, base_fee_cents, per_km_fee_cents, per_minute_fee_cents, minimum_fee_cents, commission_bps)
		  SELECT s.id, 'AOA',
		         CASE s.slug WHEN 'taxi-comfort' THEN 45000 WHEN 'taxi-xl' THEN 60000 ELSE 30000 END,
		         CASE s.slug WHEN 'taxi-comfort' THEN 18000 WHEN 'taxi-xl' THEN 22000 ELSE 13000 END,
		         CASE s.slug WHEN 'taxi-comfort' THEN 2000 WHEN 'taxi-xl' THEN 2500 ELSE 1500 END,
		         CASE s.slug WHEN 'taxi-comfort' THEN 70000 WHEN 'taxi-xl' THEN 90000 ELSE 45000 END,
		         0 FROM services s
		  WHERE NOT EXISTS (SELECT 1 FROM service_tariffs t WHERE t.service_id = s.id AND t.valid_until IS NULL)`, nil},
		{`INSERT INTO vouchers (id, code, description, category_id, discount_bps, valid_from, valid_until, per_customer_limit, is_public)
		  SELECT '00000000-0000-0000-0000-0000000000d1', 'TAXI10', '10% no táxi', c.id, 1000, now() - interval '1 day', now() + interval '365 days', 5, true
		  FROM service_categories c WHERE c.slug = 'taxi'
		  ON CONFLICT DO NOTHING`, nil},
		{`INSERT INTO providers (user_id, status, service_id)
		  SELECT '00000000-0000-0000-0000-0000000000a4', 'approved', s.id FROM services s WHERE s.slug = 'taxi'
		  ON CONFLICT (user_id) DO NOTHING`, nil},
		{`INSERT INTO vehicles (provider_id, brand, model, year, color, plate, seats)
		  VALUES ('00000000-0000-0000-0000-0000000000a4', 'Toyota', 'Corolla', 2020, 'Branco', 'LD-45-21-AF', 5)
		  ON CONFLICT (provider_id) DO NOTHING`, nil},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			return err
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
		{"00000000-0000-0000-0000-0000000000a1", "Admin AGORA", "admin@agora.ao", "+244923000101", "staff"},
		{"00000000-0000-0000-0000-0000000000a2", "Operador AGORA", "operador@agora.ao", "+244923000102", "staff"},
		{"00000000-0000-0000-0000-0000000000a3", "Cliente Teste", "", "+244923000001", "customer"},
		{"00000000-0000-0000-0000-0000000000a4", "Prestador Teste", "", "+244923000002", "provider"},
		{"00000000-0000-0000-0000-0000000000a5", "Parceiro Teste", "parceiro@agora.ao", "+244923000105", "staff"},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx, q, r.id, r.name, r.email, r.phone, passwordHash, r.role); err != nil {
			return err
		}
	}
	return nil
}

func seedPartners(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO organizations (id, type, name) VALUES ($1, 'partner', 'Parceiro Teste')
		ON CONFLICT (id) DO NOTHING`, partnerOrganizationID)
	return err
}

func seedMemberships(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `
		WITH membership AS (
			INSERT INTO memberships (id, user_id, organization_id, status, activated_at)
			SELECT $1::uuid, $2::uuid, o.id, 'active', now()
			FROM organizations o
			WHERE ($3 = 'platform' AND o.type = 'platform') OR o.id = NULLIF($4, '')::uuid
			ON CONFLICT (user_id, organization_id) DO NOTHING
			RETURNING id
		)
		INSERT INTO membership_roles (membership_id, role_id)
		SELECT membership.id, r.id FROM membership, roles r
		WHERE r.organization_id IS NULL AND r.scope = $3::organization_type AND r.code = $5`

	rows := []struct {
		id, userID, scope, organizationID, role string
	}{
		{"00000000-0000-0000-0000-0000000000c1", "00000000-0000-0000-0000-0000000000a1", "platform", "", "super_admin"},
		{"00000000-0000-0000-0000-0000000000c2", "00000000-0000-0000-0000-0000000000a2", "platform", "", "operations"},
		{"00000000-0000-0000-0000-0000000000c5", "00000000-0000-0000-0000-0000000000a5", "partner", partnerOrganizationID, "owner"},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx, q, r.id, r.userID, r.scope, r.organizationID, r.role); err != nil {
			return err
		}
	}
	return nil
}
