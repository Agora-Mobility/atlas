package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/shared/phone"
	"github.com/Agora-Mobility/atlas/usecases/fleet/dtos"
	"github.com/Agora-Mobility/atlas/usecases/providers"
	providerdtos "github.com/Agora-Mobility/atlas/usecases/providers/dtos"
)

var (
	ErrPartnerNotFound         = errors.New("partner not found")
	ErrDriverNotFound          = errors.New("driver not found")
	ErrVehicleNotFound         = errors.New("vehicle not found")
	ErrWorkshopNotFound        = errors.New("workshop not found")
	ErrWorkshopServiceNotFound = errors.New("workshop service not found")
	ErrVehicleInUse            = errors.New("release the vehicle from its driver first")
	ErrDriverBusy              = errors.New("the driver has an active job")
)

const (
	auditEntityDriver   = "fleet_driver"
	auditEntityVehicle  = "fleet_vehicle"
	auditEntityWorkshop = "workshop"
)

type CodeSender interface {
	SendSilently(ctx context.Context, purpose enums.OTPPurpose, phone string) error
}

type Deps struct {
	Users         repository.IUserRepository
	Providers     repository.IProviderRepository
	Documents     repository.IProviderDocumentRepository
	Requests      repository.IRequestRepository
	Organizations repository.IOrganizationRepository
	Fleet         repository.IFleetRepository
	Locator       repository.IProviderLocator
	Outbox        repository.IOutboxRepository
	Audit         repository.IAuditLogRepository
	Tx            repository.ITransactionManager
	Codes         CodeSender
	Profiles      *providers.Usecase
	Business      services.IBusinessEvents
}

type Usecase struct {
	Deps
	now func() time.Time
}

func NewUsecase(deps Deps) *Usecase {
	return &Usecase{Deps: deps, now: func() time.Time { return time.Now().UTC() }}
}

func (u *Usecase) audit(ctx context.Context, actor entities.Actor, action, entityType, id string, after any) error {
	entry, err := entities.NewAuditEntry(actor, action, entityType, id, nil, after, "", logger.CorrelationID(ctx))
	if err != nil {
		return err
	}
	return u.Audit.Record(ctx, entry)
}

func (u *Usecase) requirePartner(ctx context.Context, organizationID uuid.UUID) error {
	organization, err := u.Organizations.FindByID(ctx, organizationID)
	if err != nil {
		return err
	}
	if organization == nil || !organization.IsPartner() {
		return ErrPartnerNotFound
	}
	return nil
}

func (u *Usecase) driverOf(ctx context.Context, organizationID, driverID uuid.UUID) (*entities.Provider, error) {
	driver, err := u.Providers.Find(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if driver == nil || driver.OrganizationID == nil || *driver.OrganizationID != organizationID {
		return nil, ErrDriverNotFound
	}
	return driver, nil
}

func (u *Usecase) vehicleOf(ctx context.Context, organizationID, vehicleID uuid.UUID) (*entities.FleetVehicle, error) {
	vehicle, err := u.Fleet.FindVehicle(ctx, vehicleID)
	if err != nil {
		return nil, err
	}
	if vehicle == nil || vehicle.OrganizationID == nil || *vehicle.OrganizationID != organizationID {
		return nil, ErrVehicleNotFound
	}
	return vehicle, nil
}

func (u *Usecase) requireIdle(ctx context.Context, driverID uuid.UUID) error {
	active, err := u.Requests.ActiveOfProvider(ctx, driverID)
	if err != nil {
		return err
	}
	if active != nil {
		return ErrDriverBusy
	}
	return nil
}

func serviceOf(driver *entities.Provider) *uuid.UUID {
	if driver.ServiceID != nil {
		return driver.ServiceID
	}
	return driver.RequestedServiceID
}

func (u *Usecase) checkFit(ctx context.Context, serviceID *uuid.UUID, vehicle entities.Vehicle) error {
	if serviceID == nil {
		return nil
	}
	requirements, err := u.Documents.Requirements(ctx, *serviceID)
	if err != nil {
		return err
	}
	if requirements == nil {
		return providers.ErrServiceNotOpen
	}
	if gaps := requirements.VehicleGaps(vehicle); len(gaps) > 0 {
		return &providers.IncompleteError{Code: "VEHICLE_NOT_SUITABLE", Fields: gaps}
	}
	return nil
}

func (u *Usecase) ListDrivers(ctx context.Context, organizationID uuid.UUID, status string, page pagination.Params) (*dtos.DriverList, error) {
	if err := u.requirePartner(ctx, organizationID); err != nil {
		return nil, err
	}
	page = page.Normalize()
	drivers, total, err := u.Fleet.ListDrivers(ctx, organizationID, status, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.DriverList{Items: make([]dtos.DriverView, 0, len(drivers)), Total: total, Page: page.Page, Size: page.Size}
	for i := range drivers {
		driver := &drivers[i]
		list.Items = append(list.Items, dtos.DriverView{
			UserID: driver.UserID, Name: driver.Name, Phone: driver.Phone, IsVerified: driver.IsVerified, Status: string(driver.Status), RequestedServiceID: driver.RequestedServiceID,
			ServiceID: driver.ServiceID, ServiceName: driver.ServiceName, VehicleID: driver.VehicleID, VehiclePlate: driver.VehiclePlate,
		})
	}
	return list, nil
}

func (u *Usecase) RegisterDriver(ctx context.Context, actor entities.Actor, organizationID uuid.UUID, in dtos.RegisterDriverInput) (*providerdtos.ProviderView, error) {
	if err := u.requirePartner(ctx, organizationID); err != nil {
		return nil, err
	}
	requirements, err := u.Documents.Requirements(ctx, in.ServiceID)
	if err != nil {
		return nil, err
	}
	if requirements == nil {
		return nil, providers.ErrServiceNotOpen
	}
	var vehicle *entities.FleetVehicle
	if in.VehicleID != nil {
		if vehicle, err = u.availableVehicle(ctx, organizationID, *in.VehicleID); err != nil {
			return nil, err
		}
		if err := u.checkFit(ctx, &in.ServiceID, vehicle.Vehicle); err != nil {
			return nil, err
		}
	}
	phoneNumber := phone.Normalize(in.Phone)
	existing, err := u.Users.FindByPhone(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, providers.ErrPhoneTaken
	}
	user, err := entities.NewUser(in.Name, in.Email, phoneNumber, entities.UnusablePassword, enums.RoleProvider)
	if err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Users.Create(ctx, user); err != nil {
			return err
		}
		envelope, err := events.NewEnvelope(events.UserRegisteredV1{ID: user.ID, Role: user.Role, CreatedAt: user.CreatedAt}, logger.CorrelationID(ctx))
		if err != nil {
			return err
		}
		services.BusinessOrNoop(u.Business).UserRegistered(ctx, services.UserRegistration{UserID: user.ID, Role: string(user.Role), Channel: string(enums.ClientTypePartnerPanel)})
		if err := u.Outbox.Save(ctx, envelope); err != nil {
			return err
		}
		if err := u.Providers.Create(ctx, user.ID); err != nil {
			return err
		}
		driver, err := u.Providers.Find(ctx, user.ID)
		if err != nil {
			return err
		}
		driver.OrganizationID = &organizationID
		if err := driver.RequestService(in.ServiceID); err != nil {
			return err
		}
		if err := u.Providers.Save(ctx, driver); err != nil {
			return err
		}
		if vehicle != nil {
			if err := u.Fleet.AssignVehicle(ctx, vehicle.ID, user.ID); err != nil {
				return err
			}
		}
		return u.audit(ctx, actor, "fleet.driver.register", auditEntityDriver, user.ID.String(), map[string]any{"organization_id": organizationID, "service_id": in.ServiceID, "vehicle_id": in.VehicleID})
	})
	if err != nil {
		return nil, err
	}
	if err := u.Codes.SendSilently(ctx, enums.OTPPurposeResetPassword, user.Phone); err != nil {
		logger.FromContext(ctx).Warn("access code not sent to fleet driver", zap.Error(err))
	}
	return u.Profiles.Profile(ctx, user.ID)
}

func (u *Usecase) GetDriver(ctx context.Context, organizationID, driverID uuid.UUID) (*providerdtos.ProviderView, error) {
	if _, err := u.driverOf(ctx, organizationID, driverID); err != nil {
		return nil, err
	}
	return u.Profiles.Profile(ctx, driverID)
}

func (u *Usecase) availableVehicle(ctx context.Context, organizationID, vehicleID uuid.UUID) (*entities.FleetVehicle, error) {
	vehicle, err := u.vehicleOf(ctx, organizationID, vehicleID)
	if err != nil {
		return nil, err
	}
	if !vehicle.IsActive || vehicle.ProviderID != nil {
		return nil, repository.ErrVehicleTaken
	}
	return vehicle, nil
}

func (u *Usecase) AssignVehicle(ctx context.Context, actor entities.Actor, organizationID, driverID, vehicleID uuid.UUID) (*providerdtos.ProviderView, error) {
	driver, err := u.driverOf(ctx, organizationID, driverID)
	if err != nil {
		return nil, err
	}
	vehicle, err := u.vehicleOf(ctx, organizationID, vehicleID)
	if err != nil {
		return nil, err
	}
	if vehicle.ProviderID != nil && *vehicle.ProviderID == driverID {
		return u.Profiles.Profile(ctx, driverID)
	}
	if !vehicle.IsActive || vehicle.ProviderID != nil {
		return nil, repository.ErrVehicleTaken
	}
	if err := u.requireIdle(ctx, driverID); err != nil {
		return nil, err
	}
	if err := u.checkFit(ctx, serviceOf(driver), vehicle.Vehicle); err != nil {
		return nil, err
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.AssignVehicle(ctx, vehicleID, driverID); err != nil {
			return err
		}
		return u.audit(ctx, actor, "fleet.vehicle.assign", auditEntityVehicle, vehicleID.String(), map[string]any{"driver_id": driverID})
	})
	if err != nil {
		return nil, err
	}
	return u.Profiles.Profile(ctx, driverID)
}

func (u *Usecase) ReleaseVehicle(ctx context.Context, actor entities.Actor, organizationID, driverID uuid.UUID) error {
	if _, err := u.driverOf(ctx, organizationID, driverID); err != nil {
		return err
	}
	return u.release(ctx, actor, driverID)
}

func (u *Usecase) release(ctx context.Context, actor entities.Actor, driverID uuid.UUID) error {
	if err := u.requireIdle(ctx, driverID); err != nil {
		return err
	}
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.ReleaseVehicle(ctx, driverID); err != nil {
			return err
		}
		return u.audit(ctx, actor, "fleet.vehicle.release", auditEntityDriver, driverID.String(), nil)
	})
	if err != nil {
		return err
	}
	return u.Locator.SetOffline(ctx, driverID)
}

func toVehicleView(vehicle *entities.FleetVehicle) dtos.FleetVehicleView {
	v := vehicle.Vehicle
	return dtos.FleetVehicleView{
		ID: vehicle.ID, OrganizationID: vehicle.OrganizationID, DriverID: vehicle.ProviderID, DriverName: vehicle.ProviderName, Kind: v.Kind, Brand: v.Brand, Model: v.Model,
		Year: v.Year, Color: v.Color, Plate: v.Plate, Seats: v.Seats, CapacityKg: v.CapacityKg, IsActive: vehicle.IsActive,
	}
}

func (u *Usecase) CreateVehicle(ctx context.Context, actor entities.Actor, organizationID uuid.UUID, in dtos.FleetVehicleInput) (*dtos.FleetVehicleView, error) {
	if err := u.requirePartner(ctx, organizationID); err != nil {
		return nil, err
	}
	details, err := entities.Vehicle{Kind: in.Kind, Brand: in.Brand, Model: in.Model, Year: in.Year, Color: in.Color, Plate: in.Plate, Seats: in.Seats, CapacityKg: in.CapacityKg}.Normalized(u.now())
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	vehicle := &entities.FleetVehicle{ID: id, OrganizationID: &organizationID, Vehicle: details, IsActive: true}
	if in.DriverID != nil {
		driver, err := u.driverOf(ctx, organizationID, *in.DriverID)
		if err != nil {
			return nil, err
		}
		if err := u.requireIdle(ctx, *in.DriverID); err != nil {
			return nil, err
		}
		if err := u.checkFit(ctx, serviceOf(driver), details); err != nil {
			return nil, err
		}
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.CreateVehicle(ctx, vehicle); err != nil {
			return err
		}
		if in.DriverID != nil {
			if err := u.Fleet.AssignVehicle(ctx, id, *in.DriverID); err != nil {
				return err
			}
		}
		return u.audit(ctx, actor, "fleet.vehicle.create", auditEntityVehicle, id.String(), map[string]any{"organization_id": organizationID, "plate": details.Plate, "driver_id": in.DriverID})
	})
	if err != nil {
		return nil, err
	}
	return u.GetVehicle(ctx, organizationID, id)
}

func (u *Usecase) GetVehicle(ctx context.Context, organizationID, vehicleID uuid.UUID) (*dtos.FleetVehicleView, error) {
	vehicle, err := u.vehicleOf(ctx, organizationID, vehicleID)
	if err != nil {
		return nil, err
	}
	view := toVehicleView(vehicle)
	return &view, nil
}

func (u *Usecase) UpdateVehicle(ctx context.Context, actor entities.Actor, organizationID, vehicleID uuid.UUID, in dtos.UpdateVehicleInput) (*dtos.FleetVehicleView, error) {
	vehicle, err := u.vehicleOf(ctx, organizationID, vehicleID)
	if err != nil {
		return nil, err
	}
	details, err := entities.Vehicle{Kind: in.Kind, Brand: in.Brand, Model: in.Model, Year: in.Year, Color: in.Color, Plate: in.Plate, Seats: in.Seats, CapacityKg: in.CapacityKg}.Normalized(u.now())
	if err != nil {
		return nil, err
	}
	if vehicle.ProviderID != nil {
		if !in.IsActive {
			return nil, ErrVehicleInUse
		}
		driver, err := u.driverOf(ctx, organizationID, *vehicle.ProviderID)
		if err != nil {
			return nil, err
		}
		if err := u.checkFit(ctx, serviceOf(driver), details); err != nil {
			return nil, err
		}
	}
	vehicle.Vehicle, vehicle.IsActive = details, in.IsActive
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.UpdateVehicle(ctx, vehicle); err != nil {
			return err
		}
		return u.audit(ctx, actor, "fleet.vehicle.update", auditEntityVehicle, vehicleID.String(), map[string]any{"plate": details.Plate, "is_active": in.IsActive})
	})
	if err != nil {
		return nil, err
	}
	return u.GetVehicle(ctx, organizationID, vehicleID)
}

func (u *Usecase) DeactivateVehicle(ctx context.Context, actor entities.Actor, organizationID, vehicleID uuid.UUID) error {
	vehicle, err := u.vehicleOf(ctx, organizationID, vehicleID)
	if err != nil {
		return err
	}
	if vehicle.ProviderID != nil {
		if err := u.release(ctx, actor, *vehicle.ProviderID); err != nil {
			return err
		}
	}
	vehicle.IsActive = false
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Fleet.UpdateVehicle(ctx, vehicle); err != nil {
			return err
		}
		return u.audit(ctx, actor, "fleet.vehicle.deactivate", auditEntityVehicle, vehicleID.String(), nil)
	})
}

func (u *Usecase) ListVehicles(ctx context.Context, filter repository.VehicleFilter, page pagination.Params) (*dtos.VehicleList, error) {
	if filter.OrganizationID != nil {
		if err := u.requirePartner(ctx, *filter.OrganizationID); err != nil {
			return nil, err
		}
	}
	page = page.Normalize()
	filter.Limit, filter.Offset = int(page.Limit()), int(page.Offset())
	vehicles, total, err := u.Fleet.ListVehicles(ctx, filter)
	if err != nil {
		return nil, err
	}
	list := &dtos.VehicleList{Items: make([]dtos.FleetVehicleView, 0, len(vehicles)), Total: total, Page: page.Page, Size: page.Size}
	for i := range vehicles {
		list.Items = append(list.Items, toVehicleView(&vehicles[i]))
	}
	return list, nil
}
