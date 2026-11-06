package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrNIFTaken = errors.New("an organization already has this NIF")

type IVerificationRepository interface {
	Find(ctx context.Context, providerID uuid.UUID) (*entities.ProviderVerification, error)
	Save(ctx context.Context, verification *entities.ProviderVerification) error
	Banks(ctx context.Context, search string) ([]entities.Bank, error)
	Bank(ctx context.Context, id uuid.UUID) (*entities.Bank, error)
	BankByCode(ctx context.Context, code string) (*entities.Bank, error)
	Insurers(ctx context.Context, search string) ([]entities.Insurer, error)
	Insurer(ctx context.Context, id uuid.UUID) (*entities.Insurer, error)
	WorkshopCategories(ctx context.Context) ([]entities.WorkshopCategory, error)
	ScheduleReminder(ctx context.Context, reminder *entities.VerificationReminder) error
	DueReminders(ctx context.Context, now time.Time, limit int) ([]entities.VerificationReminder, error)
	MarkReminderSent(ctx context.Context, id uuid.UUID, now time.Time) error
	ExpiringDocuments(ctx context.Context, from, until time.Time, limit int) ([]entities.ExpiringDocument, error)
	CreateWorkshop(ctx context.Context, workshop entities.NewWorkshop, now time.Time) (uuid.UUID, error)
}
