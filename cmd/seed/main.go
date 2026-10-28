package main

import (
	"context"
	"errors"
	"log"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/postgres/seeders"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed: %v", err)
	}
	log.Print("seed completed")
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return errors.New("seed is disabled in production")
	}

	ctx := context.Background()
	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	return seeders.Run(ctx, pool)
}
