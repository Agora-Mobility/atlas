package dispatch

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	hotWindow     = time.Hour
	hotRadiusKm   = 10
	hotCacheTTL   = time.Minute
	hotFloor      = 2 // a cell with a single request would point at one customer
	hotLevelCount = 3
)

var ErrPositionUnknown = errors.New("the position of the provider is not known")

type hotCells struct {
	demand    map[int64]int64
	peak      int64
	expiresAt time.Time
}

// hotCellCache keeps the demand of each category for a minute, so every provider of a city asking at once costs one query.
type hotCellCache struct {
	mu         sync.Mutex
	byCategory map[string]hotCells
}

// ProviderDemand shows the provider where its own category was asked for in the last hour, around it, as levels and never counts.
func (u *Usecase) ProviderDemand(ctx context.Context, providerID uuid.UUID, query dtos.DemandQuery) (*dtos.DemandView, error) {
	profile, err := u.Providers.Find(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != entities.ProviderApproved || profile.ServiceID == nil {
		return nil, ErrProviderNotApproved
	}
	category, err := u.Providers.CategoryOf(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if category == nil {
		return nil, ErrProviderNotApproved
	}
	position, err := u.demandPosition(ctx, providerID, query.Position)
	if err != nil {
		return nil, err
	}
	hot, err := u.hotCellsOf(ctx, category.Slug)
	if err != nil {
		return nil, err
	}
	around, err := u.Geo.Covering(position, hotRadiusKm, heatResolution)
	if err != nil {
		return nil, err
	}
	view := &dtos.DemandView{Category: category.Slug, WindowMinutes: int(hotWindow / time.Minute), RadiusKm: hotRadiusKm, ExpiresAt: hot.expiresAt, Cells: []dtos.DemandCell{}}
	for _, cell := range around {
		requests := hot.demand[cell]
		if requests < hotFloor {
			continue
		}
		boundary, err := u.Geo.Boundary(cell)
		if err != nil {
			return nil, err
		}
		vertices := make([][2]float64, 0, len(boundary))
		for _, vertex := range boundary {
			vertices = append(vertices, [2]float64{vertex.Lat, vertex.Lng})
		}
		view.Cells = append(view.Cells, dtos.DemandCell{Cell: u.Geo.Format(cell), Level: hotLevel(requests, hot.peak), Boundary: vertices})
	}
	sort.Slice(view.Cells, func(i, j int) bool { return view.Cells[i].Cell < view.Cells[j].Cell })
	return view, nil
}

func (u *Usecase) demandPosition(ctx context.Context, providerID uuid.UUID, given *entities.GeoPoint) (entities.GeoPoint, error) {
	if given != nil {
		return *given, nil
	}
	last, err := u.Locator.LastPosition(ctx, providerID)
	if err != nil {
		return entities.GeoPoint{}, err
	}
	if last == nil {
		return entities.GeoPoint{}, ErrPositionUnknown
	}
	return last.Point, nil
}

func (u *Usecase) hotCellsOf(ctx context.Context, category string) (hotCells, error) {
	now := u.now()
	u.hotCells.mu.Lock()
	cached, ok := u.hotCells.byCategory[category]
	u.hotCells.mu.Unlock()
	if ok && now.Before(cached.expiresAt) {
		return cached, nil
	}
	demand, err := u.Requests.DemandByCell(ctx, repository.HeatFilter{From: now.Add(-hotWindow), To: now, Category: category})
	if err != nil {
		return hotCells{}, err
	}
	fresh := hotCells{demand: demand, expiresAt: now.Add(hotCacheTTL)}
	for _, requests := range demand {
		if requests >= hotFloor && requests > fresh.peak {
			fresh.peak = requests
		}
	}
	u.hotCells.mu.Lock()
	u.hotCells.byCategory[category] = fresh
	u.hotCells.mu.Unlock()
	return fresh, nil
}

// hotLevel splits the cells in thirds of the busiest cell of the city, so the level of a place does not change as the provider moves.
func hotLevel(requests, peak int64) int {
	if peak <= 0 {
		return 1
	}
	level := int((requests*hotLevelCount + peak - 1) / peak)
	return min(max(level, 1), hotLevelCount)
}
