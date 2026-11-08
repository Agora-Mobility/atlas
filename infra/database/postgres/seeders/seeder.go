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
	{name: "workshops", run: seedWorkshops},
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

// A zona Luanda vem da migration 000043 (é dado real, também de produção); o seed só põe dados de teste.
func seedDispatch(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []struct {
		sql  string
		args []any
	}{
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

type seedWorkshop struct {
	id, addressID, name, line, phone string
	lat, lng                         float64
	homeService                      bool
}

// Oficinas de teste à volta do Futungo de Belas, dentro da zona Luanda do seed, para o fluxo de manutenção.
var futungoWorkshops = []seedWorkshop{
	{"00000000-0000-0000-0000-0000000000e1", "00000000-0000-0000-0000-0000000000f1", "Oficina Futungo Auto", "Estrada do Futungo, Belas", "+244923000201", -8.8990, 13.1780, true},
	{"00000000-0000-0000-0000-0000000000e2", "00000000-0000-0000-0000-0000000000f2", "Mussulo Motors", "Via Expressa, Futungo de Belas", "+244923000202", -8.9060, 13.1850, false},
	{"00000000-0000-0000-0000-0000000000e3", "00000000-0000-0000-0000-0000000000f3", "Belas Pneus & Travões", "Rua do Futungo, Belas", "+244923000203", -8.9130, 13.1920, true},
}

var workshopSeedServices = []struct {
	category, name string
	priceCents     int64
	minutes        int
}{
	{"review", "Revisão geral", 3_500_000, 120},
	{"oil", "Mudança de óleo e filtro", 1_800_000, 45},
	{"tires", "Troca de pneus", 1_200_000, 40},
	{"brakes", "Pastilhas de travão", 2_500_000, 90},
}

func seedWorkshops(ctx context.Context, pool *pgxpool.Pool) error {
	for _, w := range futungoWorkshops {
		if _, err := pool.Exec(ctx, `INSERT INTO addresses (id, lat, lng, line, neighborhood, city, province, country_code)
			VALUES ($1, $2, $3, $4, 'Futungo de Belas', 'Luanda', 'Luanda', 'AO') ON CONFLICT (id) DO NOTHING`, w.addressID, w.lat, w.lng, w.line); err != nil {
			return err
		}
		var homeFee int64
		if w.homeService {
			homeFee = 300_000
		}
		if _, err := pool.Exec(ctx, `INSERT INTO workshops (id, organization_id, address_id, name, phone, opening_hours, offers_home_service, home_service_fee_cents, currency, capacity, about, warranty)
			VALUES ($1, $2, $3, $4, $5, 'Todos os dias 07:00-19:00', $6, $7, 'AOA', 2, 'Oficina de teste do seed', '3 meses') ON CONFLICT (id) DO NOTHING`,
			w.id, partnerOrganizationID, w.addressID, w.name, w.phone, w.homeService, homeFee); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO workshop_hours (workshop_id, weekday, open_minute, close_minute)
			SELECT $1, d, 420, 1140 FROM generate_series(0, 6) AS d ON CONFLICT DO NOTHING`, w.id); err != nil {
			return err
		}
		for _, service := range workshopSeedServices {
			if _, err := pool.Exec(ctx, `INSERT INTO workshop_services (id, workshop_id, name, price_cents, duration_minutes, category_id)
				SELECT gen_random_uuid(), $1, $2, $3, $4, c.id FROM catalog_items c
				WHERE c.kind = 'workshop_category' AND c.code = $5
				  AND NOT EXISTS (SELECT 1 FROM workshop_services s WHERE s.workshop_id = $1 AND s.name = $2)`,
				w.id, service.name, service.priceCents, service.minutes, service.category); err != nil {
				return err
			}
		}
	}
	return nil
}
