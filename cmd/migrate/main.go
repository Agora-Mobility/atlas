package main

import (
	"log"
	"os"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
)

const (
	directionUp   = "up"
	directionDown = "down"
)

func main() {
	direction, ok := migrationDirection(os.Args[1:])
	if !ok {
		log.Fatal("usage: migrate [up|down]")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if err := postgres.Migrate(cfg.Database.URL, direction); err != nil {
		log.Fatalf("migrate %s: %v", direction, err)
	}
	log.Printf("migrations applied (%s)", direction)
}

func migrationDirection(args []string) (string, bool) {
	if len(args) == 0 {
		return directionUp, true
	}
	switch args[0] {
	case directionUp:
		return directionUp, true
	case directionDown:
		return directionDown, true
	default:
		return "", false
	}
}
