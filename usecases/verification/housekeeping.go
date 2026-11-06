package verification

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
)

func (u *Usecase) RunHousekeeping(ctx context.Context) {
	ticker := time.NewTicker(u.Settings.HousekeepingInterval)
	defer ticker.Stop()
	for {
		if err := u.Housekeep(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.FromContext(ctx).Warn("verification housekeeping failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (u *Usecase) Housekeep(ctx context.Context) error {
	return errors.Join(u.dropStaleUploads(ctx), u.scheduleExpiryNotices(ctx), u.sendDueReminders(ctx))
}

func (u *Usecase) dropStaleUploads(ctx context.Context) error {
	stale, err := u.Documents.StalePendingFiles(ctx, u.now().Add(-u.Settings.PendingUploadTTL), batchSize)
	if err != nil {
		return err
	}
	for i := range stale {
		u.discard(ctx, stale[i].URL)
		if err := u.Documents.DeletePendingFile(ctx, stale[i].ID); err != nil {
			return err
		}
	}
	return nil
}

func (u *Usecase) scheduleExpiryNotices(ctx context.Context) error {
	now := u.now()
	today := entities.DateOnly(now)
	expiring, err := u.Verifications.ExpiringDocuments(ctx, today, today.AddDate(0, 0, u.Settings.ExpiryNoticeDays), batchSize)
	if err != nil {
		return err
	}
	for i := range expiring {
		document := &expiring[i]
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		documentID, expiresOn := document.DocumentID, document.ExpiresOn
		if err := u.Verifications.ScheduleReminder(ctx, &entities.VerificationReminder{
			ID: id, ProviderID: document.ProviderID, Kind: entities.ReminderDocumentExpiry, DocumentID: &documentID, ExpiresOn: &expiresOn, DueAt: now, CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (u *Usecase) sendDueReminders(ctx context.Context) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		now := u.now()
		due, err := u.Verifications.DueReminders(ctx, now, batchSize)
		if err != nil {
			return err
		}
		for i := range due {
			reminder := &due[i]
			if err := u.publish(ctx, events.ProviderVerificationReminderV1{
				ReminderID: reminder.ID, ProviderID: reminder.ProviderID, Kind: reminder.Kind, DocumentType: reminder.DocumentType.String(), ExpiresOn: reminder.ExpiresOn, OccurredAt: now,
			}); err != nil {
				return err
			}
			if err := u.Verifications.MarkReminderSent(ctx, reminder.ID, now); err != nil {
				return err
			}
		}
		return nil
	})
}
