package experience

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func toVehicleView(v *entities.CustomerVehicle) dtos.CustomerVehicleView {
	return dtos.CustomerVehicleView{ID: v.ID, Brand: v.Brand, Model: v.Model, Plate: v.Plate, Year: v.Year, Fuel: v.Fuel}
}

func (u *Usecase) MyVehicles(ctx context.Context, customerID uuid.UUID) (*dtos.CustomerVehicleList, error) {
	vehicles, err := u.Garage.Vehicles(ctx, customerID)
	if err != nil {
		return nil, err
	}
	list := &dtos.CustomerVehicleList{Items: make([]dtos.CustomerVehicleView, 0, len(vehicles))}
	for i := range vehicles {
		list.Items = append(list.Items, toVehicleView(&vehicles[i]))
	}
	return list, nil
}

func (u *Usecase) AddVehicle(ctx context.Context, customerID uuid.UUID, in dtos.CustomerVehicleInput) (*dtos.CustomerVehicleView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := u.now()
	vehicle := &entities.CustomerVehicle{ID: id, CustomerID: customerID, Brand: in.Brand, Model: in.Model, Plate: in.Plate, Year: in.Year, Fuel: in.Fuel, CreatedAt: now}
	if err := vehicle.Normalize(now); err != nil {
		return nil, err
	}
	if err := u.Garage.CreateVehicle(ctx, vehicle); err != nil {
		return nil, err
	}
	view := toVehicleView(vehicle)
	return &view, nil
}

func (u *Usecase) UpdateVehicle(ctx context.Context, customerID, id uuid.UUID, in dtos.CustomerVehicleInput) (*dtos.CustomerVehicleView, error) {
	now := u.now()
	vehicle := &entities.CustomerVehicle{ID: id, CustomerID: customerID, Brand: in.Brand, Model: in.Model, Plate: in.Plate, Year: in.Year, Fuel: in.Fuel}
	if err := vehicle.Normalize(now); err != nil {
		return nil, err
	}
	found, err := u.Garage.UpdateVehicle(ctx, vehicle, now)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, entities.ErrCustomerVehicleNotFound
	}
	view := toVehicleView(vehicle)
	return &view, nil
}

func (u *Usecase) RemoveVehicle(ctx context.Context, customerID, id uuid.UUID) error {
	found, err := u.Garage.DeactivateVehicle(ctx, id, customerID, u.now())
	if err != nil {
		return err
	}
	if !found {
		return entities.ErrCustomerVehicleNotFound
	}
	return nil
}

func (u *Usecase) NewPhotoUpload(ctx context.Context, ownerID uuid.UUID, in dtos.PhotoUploadInput) (*dtos.PhotoUploadView, error) {
	types, err := u.Reference.Setting(ctx, entities.SettingPhotoTypes)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(strings.Split(types, ","), in.ContentType) {
		return nil, entities.ErrPhotoTypeNotAllowed
	}
	maxBytes, err := u.settingInt(ctx, entities.SettingPhotoMaxBytes)
	if err != nil {
		return nil, err
	}
	if in.SizeBytes > maxBytes {
		return nil, entities.ErrPhotoTooLarge
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	key := "media/" + ownerID.String() + "/" + id.String()
	presigned, err := u.Storage.PresignUpload(ctx, key, in.ContentType, in.SizeBytes)
	if err != nil {
		return nil, err
	}
	upload := &entities.MediaUpload{ID: id, OwnerID: ownerID, ObjectKey: key, ContentType: in.ContentType, SizeBytes: in.SizeBytes, CreatedAt: u.now()}
	if err := u.Garage.CreateUpload(ctx, upload); err != nil {
		return nil, err
	}
	return &dtos.PhotoUploadView{ID: id, UploadURL: presigned.URL, Headers: presigned.Headers, ExpiresAt: presigned.ExpiresAt}, nil
}

// CheckPhotos validates that the photos are the caller's, free, within the
// limit and really uploaded with the announced type.
func (u *Usecase) CheckPhotos(ctx context.Context, ownerID uuid.UUID, ids []uuid.UUID, limitKey string, already int) ([]entities.MediaUpload, error) {
	limit, err := u.settingInt(ctx, limitKey)
	if err != nil {
		return nil, err
	}
	if int64(len(ids)+already) > limit {
		return nil, entities.ErrPhotoLimit
	}
	uploads, err := u.Garage.Uploads(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(uploads) != len(ids) {
		return nil, entities.ErrPhotoNotFound
	}
	for i := range uploads {
		upload := &uploads[i]
		if upload.OwnerID != ownerID || upload.RequestID != nil {
			return nil, entities.ErrPhotoNotFound
		}
		stored, err := u.Storage.Stat(ctx, upload.ObjectKey)
		if errors.Is(err, services.ErrStorageUnavailable) {
			return nil, err
		}
		if err != nil || stored == nil || stored.SizeBytes != upload.SizeBytes || stored.ContentType != upload.ContentType {
			return nil, entities.ErrPhotoNotUploaded
		}
	}
	return uploads, nil
}

func (u *Usecase) AttachRequestPhotos(ctx context.Context, customerID, requestID uuid.UUID, in dtos.AttachPhotosInput) (*dtos.PhotoList, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || !request.IsCustomer(customerID) {
		return nil, ErrRequestNotFound
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		locked, err := u.Requests.FindForUpdate(ctx, requestID)
		if err != nil {
			return err
		}
		if locked == nil {
			return ErrRequestNotFound
		}
		existing, err := u.Garage.OfRequest(ctx, requestID)
		if err != nil {
			return err
		}
		if _, err := u.CheckPhotos(ctx, customerID, in.PhotoIDs, entities.SettingMaxPhotos, len(existing)); err != nil {
			return err
		}
		attached, err := u.Garage.AttachToRequest(ctx, in.PhotoIDs, customerID, requestID)
		if err != nil {
			return err
		}
		if attached != int64(len(in.PhotoIDs)) {
			return entities.ErrPhotoNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return u.RequestPhotos(ctx, customerID, requestID)
}

func (u *Usecase) RequestPhotos(ctx context.Context, userID, requestID uuid.UUID) (*dtos.PhotoList, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || (!request.IsCustomer(userID) && !request.IsProvider(userID)) {
		return nil, ErrRequestNotFound
	}
	uploads, err := u.Garage.OfRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return u.photoList(ctx, uploads)
}

func (u *Usecase) photoList(ctx context.Context, uploads []entities.MediaUpload) (*dtos.PhotoList, error) {
	list := &dtos.PhotoList{Items: make([]dtos.PhotoView, 0, len(uploads))}
	for i := range uploads {
		link, err := u.Storage.PresignDownload(ctx, uploads[i].ObjectKey)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, dtos.PhotoView{ID: uploads[i].ID, ContentType: uploads[i].ContentType, URL: link.URL, ExpiresAt: link.ExpiresAt})
	}
	return list, nil
}
