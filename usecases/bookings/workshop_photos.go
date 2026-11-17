package bookings

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/bookings/dtos"
)

type PhotoChecker interface {
	CheckPhotos(ctx context.Context, ownerID uuid.UUID, ids []uuid.UUID, limitKey string, already int) ([]entities.MediaUpload, error)
}

func (u *Usecase) photoURL(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	signed, err := u.Storage.PresignDownload(ctx, key)
	if errors.Is(err, services.ErrStorageUnavailable) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}

func (u *Usecase) photoViews(ctx context.Context, uploads []entities.MediaUpload) ([]dtos.WorkshopPhotoView, error) {
	views := make([]dtos.WorkshopPhotoView, 0, len(uploads))
	for i := range uploads {
		url, err := u.photoURL(ctx, uploads[i].ObjectKey)
		if err != nil {
			return nil, err
		}
		if url != "" {
			views = append(views, dtos.WorkshopPhotoView{ID: uploads[i].ID, URL: url})
		}
	}
	return views, nil
}

func (u *Usecase) WorkshopPhotos(ctx context.Context, organizationID, workshopID uuid.UUID) (*dtos.WorkshopPhotoList, error) {
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	uploads, err := u.Bookings.WorkshopPhotos(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	views, err := u.photoViews(ctx, uploads)
	if err != nil {
		return nil, err
	}
	return &dtos.WorkshopPhotoList{Items: views}, nil
}

func (u *Usecase) SetWorkshopPhotos(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.WorkshopPhotosInput) (*dtos.WorkshopPhotoList, error) {
	if actor.ID == nil {
		return nil, entities.ErrPhotoNotFound
	}
	if _, err := u.ownWorkshop(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Bookings.LockWorkshop(ctx, workshopID); err != nil {
			return err
		}
		current, err := u.Bookings.WorkshopPhotos(ctx, workshopID)
		if err != nil {
			return err
		}
		kept := make(map[uuid.UUID]bool, len(current))
		for i := range current {
			kept[current[i].ID] = true
		}
		fresh := make([]uuid.UUID, 0, len(in.PhotoIDs))
		for _, id := range in.PhotoIDs {
			if !kept[id] {
				fresh = append(fresh, id)
			}
		}
		if _, err := u.Photos.CheckPhotos(ctx, *actor.ID, fresh, entities.SettingMaxWorkshopPhotos, len(in.PhotoIDs)-len(fresh)); err != nil {
			return err
		}
		if err := u.Bookings.ReplaceWorkshopPhotos(ctx, workshopID, in.PhotoIDs, u.now()); err != nil {
			return err
		}
		return u.audit(ctx, actor, "workshop.photos.set", "workshop", workshopID.String(), in)
	})
	if err != nil {
		return nil, err
	}
	return u.WorkshopPhotos(ctx, organizationID, workshopID)
}
