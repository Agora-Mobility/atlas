package dispatch

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	zoneCacheTTL       = time.Minute
	zoneCellResolution = 6
)

type ZoneCache struct {
	config repository.IDispatchConfigRepository
	geo    services.IGeoIndex
	now    func() time.Time

	mu       sync.RWMutex
	zones    map[uuid.UUID]*entities.ServiceZone
	cells    map[int64]uuid.UUID
	loadedAt time.Time
}

func NewZoneCache(config repository.IDispatchConfigRepository, geo services.IGeoIndex) *ZoneCache {
	return &ZoneCache{config: config, geo: geo, now: time.Now}
}

func (c *ZoneCache) Invalidate() {
	c.mu.Lock()
	c.loadedAt = time.Time{}
	c.mu.Unlock()
}

func (c *ZoneCache) Resolve(ctx context.Context, point entities.GeoPoint) (*entities.ServiceZone, error) {
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	cell, err := c.geo.Cell(point, zoneCellResolution)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	zoneID, interior := c.cells[cell]
	var zone *entities.ServiceZone
	if interior {
		zone = c.zones[zoneID]
	}
	c.mu.RUnlock()
	if zone != nil {
		return zone, nil
	}

	id, err := c.config.ZoneCoveringPoint(ctx, point)
	if err != nil || id == nil {
		return nil, err
	}
	c.mu.RLock()
	zone = c.zones[*id]
	c.mu.RUnlock()
	if zone != nil {
		return zone, nil
	}
	return c.config.FindZone(ctx, *id)
}

func (c *ZoneCache) refresh(ctx context.Context) error {
	c.mu.RLock()
	fresh := !c.loadedAt.IsZero() && c.now().Sub(c.loadedAt) < zoneCacheTTL
	c.mu.RUnlock()
	if fresh {
		return nil
	}
	zones, err := c.config.ListZones(ctx, true)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]*entities.ServiceZone, len(zones))
	cells := map[int64]uuid.UUID{}
	ambiguous := map[int64]struct{}{}
	for _, zone := range zones {
		byID[zone.ID] = zone
		interior, err := c.geo.InteriorCells(zone.Polygons, zoneCellResolution)
		if err != nil {
			return err
		}
		for _, cell := range interior {
			if _, taken := cells[cell]; taken {
				ambiguous[cell] = struct{}{}
				continue
			}
			cells[cell] = zone.ID
		}
	}
	for cell := range ambiguous {
		delete(cells, cell)
	}
	c.mu.Lock()
	c.zones, c.cells, c.loadedAt = byID, cells, c.now()
	c.mu.Unlock()
	return nil
}
