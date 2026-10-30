package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/Agora-Mobility/atlas/migrations"
)

// Migrate applies all pending migrations ("up") or rolls back the most
// recent one ("down"). Rolling back one step at a time is deliberate: a full
// "down" would drop every table. Migrations run inside Schema, which is
// created first, and the migrations table lives there too.
func Migrate(databaseURL, direction string) error {
	if err := ensureSchema(context.Background(), databaseURL); err != nil {
		return err
	}
	migrationURL, err := withSearchPath(databaseURL)
	if err != nil {
		return err
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, toPgx5URL(migrationURL))
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1)
	default:
		return fmt.Errorf("unknown migrate direction %q (use up or down)", direction)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %s: %w", direction, err)
	}
	return nil
}

// toPgx5URL rewrites postgres:// URLs to the scheme of the pgx v5 driver.
func toPgx5URL(url string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(url, prefix) {
			return "pgx5://" + strings.TrimPrefix(url, prefix)
		}
	}
	return url
}
