package postgres

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
)

// Schema holds everything of ours, PostGIS included. The database has no
// other schema: migration 000001 removes public.
const Schema = "atlas"

const searchPath = Schema

// ensureSchema creates Schema, which the first migration installs PostGIS in.
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
