package addresses

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/addresses/dtos"
)

var ErrAddressNotFound = errors.New("address not found")

type Usecase struct {
	repo repository.IAddressRepository
}

func NewUsecase(repo repository.IAddressRepository) *Usecase {
	return &Usecase{repo: repo}
}

func (u *Usecase) List(ctx context.Context, userID uuid.UUID) (*dtos.UserAddressList, error) {
	addresses, err := u.repo.ListOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	list := &dtos.UserAddressList{Items: make([]dtos.UserAddressView, 0, len(addresses))}
	for i := range addresses {
		list.Items = append(list.Items, toView(&addresses[i]))
	}
	return list, nil
}

func (u *Usecase) Create(ctx context.Context, userID uuid.UUID, in dtos.UserAddressInput) (*dtos.UserAddressView, error) {
	return u.save(ctx, userID, uuid.Nil, in)
}

func (u *Usecase) Update(ctx context.Context, userID, addressID uuid.UUID, in dtos.UserAddressInput) (*dtos.UserAddressView, error) {
	current, err := u.repo.FindOfUser(ctx, userID, addressID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrAddressNotFound
	}
	return u.save(ctx, userID, addressID, in)
}

func (u *Usecase) save(ctx context.Context, userID, addressID uuid.UUID, in dtos.UserAddressInput) (*dtos.UserAddressView, error) {
	address := in.ToAddress()
	address.ID = addressID
	normalized, err := entities.UserAddress{Address: address, Kind: entities.AddressKind(in.Kind), Label: in.Label}.Normalized()
	if err != nil {
		return nil, err
	}
	if err := u.repo.SaveForUser(ctx, userID, normalized); err != nil {
		return nil, err
	}
	view := toView(&normalized)
	return &view, nil
}

func (u *Usecase) Delete(ctx context.Context, userID, addressID uuid.UUID) error {
	current, err := u.repo.FindOfUser(ctx, userID, addressID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrAddressNotFound
	}
	return u.repo.Delete(ctx, addressID)
}

func toView(address *entities.UserAddress) dtos.UserAddressView {
	return dtos.UserAddressView{AddressView: dtos.ToAddressView(&address.Address), Kind: string(address.Kind), Label: address.Label}
}
