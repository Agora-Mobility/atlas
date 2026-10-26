// Package repository declares the persistence interfaces the usecases depend
// on. Implementations live in infra/.
package repository

import "context"

// ITransactionManager runs fn atomically. Repositories called with the ctx
// passed to fn take part in the same transaction.
type ITransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
