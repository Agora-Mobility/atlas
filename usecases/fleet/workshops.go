package fleet

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	addressdtos "github.com/Agora-Mobility/atlas/usecases/addresses/dtos"
	"github.com/Agora-Mobility/atlas/usecases/fleet/dtos"
)

func toWorkshopServiceView(service *entities.WorkshopService) dtos.WorkshopServiceView {
	return dtos.WorkshopServiceView{
		ID: service.ID, Name: service.Name, Description: service.Description, PriceCents: service.PriceCents, DurationMinutes: service.DurationMinutes, IsActive: service.IsActive, CategoryID: service.CategoryID,
	}
}

func toWorkshopView(workshop *entities.Workshop) dtos.WorkshopView {
	view := dtos.WorkshopView{
		ID: workshop.ID, OrganizationID: workshop.OrganizationID, Name: workshop.Name, Phone: workshop.Phone, OpeningHours: workshop.OpeningHours, OffersHomeService: workshop.OffersHomeService,
		HomeServiceFeeCents: workshop.HomeServiceFeeCents, Currency: workshop.Currency, IsActive: workshop.IsActive, Address: addressdtos.ToAddressView(&workshop.Address),
		Services: make([]dtos.WorkshopServiceView, 0, len(workshop.Services)),
	}
	for i := range workshop.Services {
		view.Services = append(view.Services, toWorkshopServiceView(&workshop.Services[i]))
	}
	return view
}

func activeOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func applyWorkshop(workshop *entities.Workshop, in dtos.WorkshopInput) {
	addressID := workshop.Address.ID
	workshop.Name, workshop.Phone, workshop.OpeningHours = in.Name, in.Phone, in.OpeningHours
	workshop.OffersHomeService, workshop.HomeServiceFeeCents, workshop.Currency = in.OffersHomeService, in.HomeServiceFeeCents, in.Currency
	workshop.IsActive = activeOr(in.IsActive, workshop.IsActive)
	workshop.Address = in.Address.ToAddress()
	workshop.Address.ID = addressID
}

func (u *Usecase) saveWorkshop(ctx context.Context, actor entities.Actor, action string, workshop *entities.Workshop) (*dtos.WorkshopView, error) {
	if err := workshop.Normalize(); err != nil {
		return nil, err
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.SaveWorkshop(ctx, workshop); err != nil {
			return err
		}
		return u.audit(ctx, actor, action, auditEntityWorkshop, workshop.ID.String(), map[string]any{"organization_id": workshop.OrganizationID, "name": workshop.Name, "is_active": workshop.IsActive})
	})
	if err != nil {
		return nil, err
	}
	return u.GetWorkshop(ctx, workshop.OrganizationID, workshop.ID)
}

func (u *Usecase) CreateWorkshop(ctx context.Context, actor entities.Actor, organizationID uuid.UUID, in dtos.WorkshopInput) (*dtos.WorkshopView, error) {
	if err := u.requirePartner(ctx, organizationID); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	workshop := &entities.Workshop{ID: id, OrganizationID: organizationID, IsActive: true}
	applyWorkshop(workshop, in)
	return u.saveWorkshop(ctx, actor, "workshop.create", workshop)
}

func (u *Usecase) workshopOf(ctx context.Context, organizationID, workshopID uuid.UUID) (*entities.Workshop, error) {
	workshop, err := u.Fleet.FindWorkshop(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	if workshop == nil || workshop.OrganizationID != organizationID {
		return nil, ErrWorkshopNotFound
	}
	return workshop, nil
}

func (u *Usecase) GetWorkshop(ctx context.Context, organizationID, workshopID uuid.UUID) (*dtos.WorkshopView, error) {
	workshop, err := u.workshopOf(ctx, organizationID, workshopID)
	if err != nil {
		return nil, err
	}
	view := toWorkshopView(workshop)
	return &view, nil
}

func (u *Usecase) UpdateWorkshop(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.WorkshopInput) (*dtos.WorkshopView, error) {
	workshop, err := u.workshopOf(ctx, organizationID, workshopID)
	if err != nil {
		return nil, err
	}
	applyWorkshop(workshop, in)
	return u.saveWorkshop(ctx, actor, "workshop.update", workshop)
}

func (u *Usecase) DeactivateWorkshop(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID) error {
	workshop, err := u.workshopOf(ctx, organizationID, workshopID)
	if err != nil {
		return err
	}
	workshop.IsActive = false
	_, err = u.saveWorkshop(ctx, actor, "workshop.deactivate", workshop)
	return err
}

func (u *Usecase) ListWorkshops(ctx context.Context, organizationID *uuid.UUID, page pagination.Params) (*dtos.WorkshopList, error) {
	if organizationID != nil {
		if err := u.requirePartner(ctx, *organizationID); err != nil {
			return nil, err
		}
	}
	page = page.Normalize()
	workshops, total, err := u.Fleet.ListWorkshops(ctx, organizationID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.WorkshopList{Items: make([]dtos.WorkshopView, 0, len(workshops)), Total: total, Page: page.Page, Size: page.Size}
	for i := range workshops {
		list.Items = append(list.Items, toWorkshopView(&workshops[i]))
	}
	return list, nil
}

func (u *Usecase) saveWorkshopService(ctx context.Context, actor entities.Actor, action string, service *entities.WorkshopService) (*dtos.WorkshopServiceView, error) {
	if err := service.Normalize(); err != nil {
		return nil, err
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.SaveWorkshopService(ctx, service); err != nil {
			return err
		}
		return u.audit(ctx, actor, action, auditEntityWorkshop, service.WorkshopID.String(), map[string]any{"service_id": service.ID, "name": service.Name, "price_cents": service.PriceCents, "is_active": service.IsActive})
	})
	if err != nil {
		return nil, err
	}
	view := toWorkshopServiceView(service)
	return &view, nil
}

func (u *Usecase) CreateWorkshopService(ctx context.Context, actor entities.Actor, organizationID, workshopID uuid.UUID, in dtos.WorkshopServiceInput) (*dtos.WorkshopServiceView, error) {
	if _, err := u.workshopOf(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return u.saveWorkshopService(ctx, actor, "workshop.service.create", &entities.WorkshopService{
		ID: id, WorkshopID: workshopID, Name: in.Name, Description: in.Description, PriceCents: in.PriceCents, DurationMinutes: in.DurationMinutes, IsActive: activeOr(in.IsActive, true), CategoryID: in.CategoryID,
	})
}

func (u *Usecase) workshopServiceOf(ctx context.Context, organizationID, workshopID, serviceID uuid.UUID) (*entities.WorkshopService, error) {
	if _, err := u.workshopOf(ctx, organizationID, workshopID); err != nil {
		return nil, err
	}
	service, err := u.Fleet.FindWorkshopService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if service == nil || service.WorkshopID != workshopID {
		return nil, ErrWorkshopServiceNotFound
	}
	return service, nil
}

func (u *Usecase) UpdateWorkshopService(ctx context.Context, actor entities.Actor, organizationID, workshopID, serviceID uuid.UUID, in dtos.WorkshopServiceInput) (*dtos.WorkshopServiceView, error) {
	service, err := u.workshopServiceOf(ctx, organizationID, workshopID, serviceID)
	if err != nil {
		return nil, err
	}
	service.Name, service.Description, service.PriceCents, service.DurationMinutes = in.Name, in.Description, in.PriceCents, in.DurationMinutes
	service.IsActive, service.CategoryID = activeOr(in.IsActive, service.IsActive), in.CategoryID
	return u.saveWorkshopService(ctx, actor, "workshop.service.update", service)
}

func (u *Usecase) DeactivateWorkshopService(ctx context.Context, actor entities.Actor, organizationID, workshopID, serviceID uuid.UUID) error {
	service, err := u.workshopServiceOf(ctx, organizationID, workshopID, serviceID)
	if err != nil {
		return err
	}
	service.IsActive = false
	_, err = u.saveWorkshopService(ctx, actor, "workshop.service.deactivate", service)
	return err
}
