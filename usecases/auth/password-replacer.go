package usecases

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 128
)

var (
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 128 characters")
	ErrPasswordReused   = errors.New("new password is the current one")
)

type passwordReplacer struct {
	userRepo  repository.IUserRepository
	revoker   *SessionRevoker
	txManager repository.ITransactionManager
	hasher    services.IPasswordHasher
}

func (r passwordReplacer) replace(ctx context.Context, user *entities.User, newPassword string, alsoInTransaction func(context.Context) error) error {
	hash, err := r.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := user.ChangePassword(hash); err != nil {
		return err
	}

	err = r.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		if alsoInTransaction != nil {
			if err := alsoInTransaction(ctx); err != nil {
				return err
			}
		}
		if err := r.userRepo.Update(ctx, user); err != nil {
			return err
		}
		return r.revoker.RevokeUser(ctx, user.ID)
	})
	if err != nil {
		return fmt.Errorf("replace password: %w", err)
	}
	return nil
}

func ValidatePassword(password string) error {
	return validatePassword(password)
}

func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < minPasswordLength {
		return ErrPasswordTooShort
	}
	if length > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}
