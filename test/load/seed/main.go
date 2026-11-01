package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	luanda        = "MULTIPOLYGON(((12.9 -9.4,13.8 -9.4,13.8 -8.5,12.9 -8.5,12.9 -9.4)))"
	centerLat     = -8.84
	centerLng     = 13.25
	spreadDegrees = 0.03
	tokenLifetime = 2 * time.Hour
)

type person struct {
	ID    string  `json:"id"`
	Token string  `json:"token"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
}

type fixtures struct {
	Customers []person `json:"customers"`
	Providers []person `json:"providers"`
	ServiceID string   `json:"service_id"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("load seed: %v", err)
	}
}

func run() error {
	customers := flag.Int("customers", 500, "customer accounts")
	providers := flag.Int("providers", 1000, "taxi providers")
	output := flag.String("out", "fixtures.json", "output file")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return fmt.Errorf("the load seed never runs in production")
	}
	ctx := context.Background()
	pool, err := postgres.OwnerConnection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `INSERT INTO service_zones (country_code, name, geom, timezone, currency) VALUES ('AO', 'Luanda', ST_GeogFromText($1), 'Africa/Luanda', 'AOA')`, luanda); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `INSERT INTO service_tariffs (service_id, currency, base_fee_cents, per_km_fee_cents, per_minute_fee_cents, minimum_fee_cents, commission_bps) SELECT id, 'AOA', 30000, 13000, 1500, 45000, 1000 FROM services WHERE slug = 'taxi'`); err != nil {
		return err
	}
	out := fixtures{}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM services WHERE slug = 'taxi'`).Scan(&out.ServiceID); err != nil {
		return err
	}
	tokens := authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, tokenLifetime)
	create := func(role enums.Role, client string, count int) ([]person, error) {
		people := make([]person, 0, count)
		for i := range count {
			id := uuid.New()
			phone := fmt.Sprintf("+24492%d%06d", roleDigit(role), i)
			if _, err := pool.Exec(ctx, `INSERT INTO users (id, name, phone, password, is_verified, status, role) VALUES ($1, 'Carga', $2, 'x', true, 'active', $3)`, id, phone, role.String()); err != nil {
				return nil, err
			}
			token, _, err := tokens.Generate(services.AccessTokenClaims{UserID: id, SessionID: uuid.New(), Role: role, ClientID: client})
			if err != nil {
				return nil, err
			}
			people = append(people, person{
				ID: id.String(), Token: token,
				Lat: centerLat + (rand.Float64()-0.5)*spreadDegrees, Lng: centerLng + (rand.Float64()-0.5)*spreadDegrees, // #nosec G404 -- load test positions
			})
		}
		return people, nil
	}
	if out.Customers, err = create(enums.RoleCustomer, "agora-customer-app", *customers); err != nil {
		return err
	}
	if out.Providers, err = create(enums.RoleProvider, "agora-provider-app", *providers); err != nil {
		return err
	}
	for i := range out.Providers {
		provider := out.Providers[i]
		if _, err := pool.Exec(ctx, `INSERT INTO provider_profiles (user_id, status, vehicle_plate, vehicle_description) VALUES ($1, 'approved', 'LD-00-00-AA', 'Carga')`, provider.ID); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO provider_services (provider_id, service_id) VALUES ($1, $2)`, provider.ID, out.ServiceID); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, encoded, 0o600)
}

func roleDigit(role enums.Role) int {
	if role == enums.RoleProvider {
		return 2
	}
	return 1
}
