package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type FleetRepository struct {
	pool *pgxpool.Pool
	tx   *postgres.TxManager
}

var _ repository.IFleetRepository = (*FleetRepository)(nil)

func NewFleetRepository(pool *pgxpool.Pool) *FleetRepository {
	return &FleetRepository{pool: pool, tx: postgres.NewTxManager(pool)}
}

func toInt32Pointer(value *int) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(*value) // #nosec G115 -- validated by the entity
	return &converted
}

func fromInt32Pointer(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func vehicleFromRow(kind, brand, model, color, plate string, year, seats, capacity *int32) entities.Vehicle {
	vehicle := entities.Vehicle{Kind: kind, Brand: brand, Model: model, Color: color, Plate: plate, CapacityKg: fromInt32Pointer(capacity)}
	if year != nil {
		vehicle.Year = int(*year)
	}
	if seats != nil {
		vehicle.Seats = int(*seats)
	}
	return vehicle
}

func (r *FleetRepository) ListDrivers(ctx context.Context, organizationID uuid.UUID, status string, limit, offset int) ([]repository.FleetDriver, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListFleetDrivers(ctx, gen.ListFleetDriversParams{OrganizationID: &organizationID, Status: status, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list fleet drivers: %w", err)
	}
	total, err := queries.CountFleetDrivers(ctx, gen.CountFleetDriversParams{OrganizationID: &organizationID, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("count fleet drivers: %w", err)
	}
	out := make([]repository.FleetDriver, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		driver := repository.FleetDriver{
			UserID: row.UserID, Name: row.Name, IsVerified: row.IsVerified, Status: entities.ProviderStatus(row.Status), RequestedServiceID: row.RequestedServiceID,
			ServiceID: row.ServiceID, VehicleID: row.VehicleID,
		}
		if row.Phone != nil {
			driver.Phone = *row.Phone
		}
		if row.ServiceName != nil {
			driver.ServiceName = *row.ServiceName
		}
		if row.VehiclePlate != nil {
			driver.VehiclePlate = *row.VehiclePlate
		}
		out = append(out, driver)
	}
	return out, total, nil
}

func plateError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return repository.ErrPlateTaken
	}
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (r *FleetRepository) CreateVehicle(ctx context.Context, v *entities.FleetVehicle) error {
	year, seats := int32(v.Vehicle.Year), int32(v.Vehicle.Seats) // #nosec G115 -- validated by the entity
	return plateError("create vehicle", postgres.Queries(ctx, r.pool).CreateFleetVehicle(ctx, gen.CreateFleetVehicleParams{
		ID: v.ID, OrganizationID: v.OrganizationID, Kind: v.Vehicle.Kind, Brand: v.Vehicle.Brand, Model: v.Vehicle.Model, Year: &year, Color: v.Vehicle.Color,
		Plate: v.Vehicle.Plate, Seats: &seats, CapacityKg: toInt32Pointer(v.Vehicle.CapacityKg),
	}))
}

func (r *FleetRepository) UpdateVehicle(ctx context.Context, v *entities.FleetVehicle) error {
	year, seats := int32(v.Vehicle.Year), int32(v.Vehicle.Seats) // #nosec G115 -- validated by the entity
	return plateError("update vehicle", postgres.Queries(ctx, r.pool).UpdateFleetVehicle(ctx, gen.UpdateFleetVehicleParams{
		ID: v.ID, Kind: v.Vehicle.Kind, Brand: v.Vehicle.Brand, Model: v.Vehicle.Model, Year: &year, Color: v.Vehicle.Color, Plate: v.Vehicle.Plate, Seats: &seats,
		CapacityKg: toInt32Pointer(v.Vehicle.CapacityKg), IsActive: v.IsActive,
	}))
}

func (r *FleetRepository) FindVehicle(ctx context.Context, id uuid.UUID) (*entities.FleetVehicle, error) {
	row, err := postgres.Queries(ctx, r.pool).GetVehicle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get vehicle: %w", err)
	}
	vehicle := &entities.FleetVehicle{
		ID: row.ID, OrganizationID: row.OrganizationID, ProviderID: row.ProviderID, IsActive: row.IsActive, CreatedAt: row.CreatedAt,
		Vehicle: vehicleFromRow(row.Kind, row.Brand, row.Model, row.Color, row.Plate, row.Year, row.Seats, row.CapacityKg),
	}
	if row.ProviderName != nil {
		vehicle.ProviderName = *row.ProviderName
	}
	return vehicle, nil
}

func (r *FleetRepository) ListVehicles(ctx context.Context, f repository.VehicleFilter) ([]entities.FleetVehicle, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	search := likePattern(f.Search)
	rows, err := queries.ListVehicles(ctx, gen.ListVehiclesParams{OrganizationID: f.OrganizationID, Kind: f.Kind, Search: search, PageLimit: int32(f.Limit), PageOffset: int32(f.Offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list vehicles: %w", err)
	}
	total, err := queries.CountVehicles(ctx, gen.CountVehiclesParams{OrganizationID: f.OrganizationID, Kind: f.Kind, Search: search})
	if err != nil {
		return nil, 0, fmt.Errorf("count vehicles: %w", err)
	}
	out := make([]entities.FleetVehicle, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		vehicle := entities.FleetVehicle{
			ID: row.ID, OrganizationID: row.OrganizationID, ProviderID: row.ProviderID, IsActive: row.IsActive, CreatedAt: row.CreatedAt,
			Vehicle: vehicleFromRow(row.Kind, row.Brand, row.Model, row.Color, row.Plate, row.Year, row.Seats, row.CapacityKg),
		}
		if row.ProviderName != nil {
			vehicle.ProviderName = *row.ProviderName
		}
		out = append(out, vehicle)
	}
	return out, total, nil
}

func (r *FleetRepository) AssignVehicle(ctx context.Context, vehicleID, providerID uuid.UUID) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		if err := queries.ReleaseVehicleOfProvider(ctx, &providerID); err != nil {
			return fmt.Errorf("release vehicle: %w", err)
		}
		rows, err := queries.AssignVehicleToProvider(ctx, gen.AssignVehicleToProviderParams{ID: vehicleID, ProviderID: &providerID})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return repository.ErrVehicleTaken
		}
		if err != nil {
			return fmt.Errorf("assign vehicle: %w", err)
		}
		if rows == 0 {
			return repository.ErrVehicleTaken
		}
		return nil
	})
}

func (r *FleetRepository) ReleaseVehicle(ctx context.Context, providerID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).ReleaseVehicleOfProvider(ctx, &providerID); err != nil {
		return fmt.Errorf("release vehicle: %w", err)
	}
	return nil
}

func (r *FleetRepository) SaveWorkshop(ctx context.Context, w *entities.Workshop) error {
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		queries := postgres.Queries(ctx, r.pool)
		if err := saveAddress(ctx, queries, &w.Address); err != nil {
			return err
		}
		err := queries.SaveWorkshop(ctx, gen.SaveWorkshopParams{
			ID: w.ID, OrganizationID: w.OrganizationID, AddressID: w.Address.ID, Name: w.Name, Phone: w.Phone, OpeningHours: w.OpeningHours,
			OffersHomeService: w.OffersHomeService, HomeServiceFeeCents: w.HomeServiceFeeCents, Currency: w.Currency, IsActive: w.IsActive,
		})
		if err != nil {
			return fmt.Errorf("save workshop: %w", err)
		}
		return nil
	})
}

func (r *FleetRepository) hydrateWorkshop(ctx context.Context, queries *gen.Queries, row *gen.Workshop) (*entities.Workshop, error) {
	address, err := findAddress(ctx, queries, row.AddressID)
	if err != nil {
		return nil, err
	}
	workshop := &entities.Workshop{
		ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name, Phone: row.Phone, OpeningHours: row.OpeningHours, OffersHomeService: row.OffersHomeService,
		HomeServiceFeeCents: row.HomeServiceFeeCents, Currency: row.Currency, IsActive: row.IsActive, Services: []entities.WorkshopService{},
	}
	if address != nil {
		workshop.Address = *address
	}
	services, err := queries.ListWorkshopServices(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("list workshop services: %w", err)
	}
	for i := range services {
		workshop.Services = append(workshop.Services, toWorkshopService(&services[i]))
	}
	return workshop, nil
}

func toWorkshopService(row *gen.WorkshopService) entities.WorkshopService {
	return entities.WorkshopService{
		ID: row.ID, WorkshopID: row.WorkshopID, Name: row.Name, Description: row.Description, PriceCents: row.PriceCents,
		DurationMinutes: int(row.DurationMinutes), IsActive: row.IsActive,
	}
}

func (r *FleetRepository) FindWorkshop(ctx context.Context, id uuid.UUID) (*entities.Workshop, error) {
	queries := postgres.Queries(ctx, r.pool)
	row, err := queries.GetWorkshop(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workshop: %w", err)
	}
	return r.hydrateWorkshop(ctx, queries, &row)
}

func (r *FleetRepository) ListWorkshops(ctx context.Context, organizationID *uuid.UUID, limit, offset int) ([]entities.Workshop, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListWorkshops(ctx, gen.ListWorkshopsParams{OrganizationID: organizationID, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list workshops: %w", err)
	}
	total, err := queries.CountWorkshops(ctx, organizationID)
	if err != nil {
		return nil, 0, fmt.Errorf("count workshops: %w", err)
	}
	out := make([]entities.Workshop, 0, len(rows))
	for i := range rows {
		workshop, err := r.hydrateWorkshop(ctx, queries, &rows[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *workshop)
	}
	return out, total, nil
}

func (r *FleetRepository) SaveWorkshopService(ctx context.Context, s *entities.WorkshopService) error {
	err := postgres.Queries(ctx, r.pool).SaveWorkshopService(ctx, gen.SaveWorkshopServiceParams{
		ID: s.ID, WorkshopID: s.WorkshopID, Name: s.Name, Description: s.Description, PriceCents: s.PriceCents, DurationMinutes: int32(s.DurationMinutes), IsActive: s.IsActive, // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("save workshop service: %w", err)
	}
	return nil
}

func (r *FleetRepository) FindWorkshopService(ctx context.Context, id uuid.UUID) (*entities.WorkshopService, error) {
	row, err := postgres.Queries(ctx, r.pool).GetWorkshopService(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workshop service: %w", err)
	}
	service := toWorkshopService(&row)
	return &service, nil
}

func likePattern(search string) string {
	if search == "" {
		return ""
	}
	return "%" + search + "%"
}
