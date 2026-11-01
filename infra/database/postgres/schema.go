package postgres

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
)

// Schema holds every table, type and function of the application. Nothing
// of ours lives in public, which keeps only extension objects (PostGIS).
const Schema = "atlas"

// searchPath resolves our objects first and extension objects (public) next.
// Unqualified CREATE statements in migrations land in Schema.
const searchPath = Schema + ",public"

// ensureSchema creates Schema and, on databases migrated before it existed,
// moves the migrations table into it so the version history is kept.
func ensureSchema(ctx context.Context, databaseURL string) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect for schema setup: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regnamespace($1) IS NOT NULL", Schema).Scan(&exists); err != nil {
		return fmt.Errorf("check schema %s: %w", Schema, err)
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{Schema}.Sanitize()); err != nil {
			return fmt.Errorf("create schema %s: %w", Schema, err)
		}
	}

	var legacy bool
	err = conn.QueryRow(ctx, `
		SELECT to_regclass('public.schema_migrations') IS NOT NULL
		   AND to_regclass($1 || '.schema_migrations') IS NULL`, Schema).Scan(&legacy)
	if err != nil {
		return fmt.Errorf("check migrations table: %w", err)
	}
	if legacy {
		if _, err := conn.Exec(ctx, "ALTER TABLE public.schema_migrations SET SCHEMA "+pgx.Identifier{Schema}.Sanitize()); err != nil {
			return fmt.Errorf("move migrations table to %s: %w", Schema, err)
		}
	}
	return nil
}

// withSearchPath returns databaseURL with the search_path of the application.
func withSearchPath(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	q := u.Query()
	q.Set("search_path", searchPath)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
