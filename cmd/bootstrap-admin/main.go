// Command bootstrap-admin creates the first Super Admin of an environment, so the
// backoffice can be reached where the development seed does not run. It does
// nothing when an admin already exists, so it is safe to run on every deploy.
//
// The credentials come from the environment, never from arguments, so they
// do not end up in the shell history or the process list:
//
//	BOOTSTRAP_ADMIN_NAME      (default "Administrador AGORA")
//	BOOTSTRAP_ADMIN_EMAIL     (required)
//	BOOTSTRAP_ADMIN_PASSWORD  (required, at least 12 characters)
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/security/password"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
)

const (
	defaultName       = "Administrador AGORA"
	minPasswordLength = 12
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("bootstrap-admin: %v", err)
	}
}

func run() error {
	name := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME"))
	if name == "" {
		name = defaultName
	}
	email := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	secret := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || secret == "" {
		return errors.New("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	if utf8.RuneCountInString(secret) < minPasswordLength {
		return errors.New("BOOTSTRAP_ADMIN_PASSWORD must have at least 12 characters")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	bootstrap := usecases.NewAdminBootstrap(
		repository.NewUserRepository(pool),
		repository.NewOrganizationRepository(pool),
		repository.NewRoleRepository(pool),
		repository.NewMembershipRepository(pool),
		outbox.NewRepository(pool),
		repository.NewAuditLogRepository(pool),
		postgres.NewTxManager(pool),
		password.NewArgon2Hasher(password.DefaultParams),
	)
	created, err := bootstrap.Run(ctx, name, email, secret)
	if err != nil {
		return err
	}
	if created {
		log.Print("super admin created")
	} else {
		log.Print("an active super admin already exists, nothing to do")
	}
	return nil
}
