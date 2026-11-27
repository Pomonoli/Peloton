package routes

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type Catalog struct {
	Repo  port.RecordedRouteReader
	Zones port.PrivacyZoneRepo
}

func (c *Catalog) List(ctx context.Context, viewer domain.UserID, query port.RecordedRouteQuery) (port.RecordedRouteListing, error) {
	out, err := c.Repo.List(ctx, viewer, query)
	if err != nil {
		return out, err
	}
	zonesByRider := make(map[domain.UserID][]domain.PrivacyZone)
	for i := range out.Routes {
		if err := ctx.Err(); err != nil {
			return port.RecordedRouteListing{}, err
		}
		route := &out.Routes[i]
		zones, cached := zonesByRider[route.Owner]
		if !cached {
			zones, err = c.zones(ctx, viewer, route.Owner)
			if err != nil {
				return port.RecordedRouteListing{}, err
			}
			zonesByRider[route.Owner] = zones
		}
		route.Track = route.Track.Mask(zones)
		if query.Map {
			route.Track = route.Track.MapGeometry().Mask(zones)
		}
	}
	return out, nil
}

func (c *Catalog) Detail(ctx context.Context, viewer domain.UserID, id domain.RecordedRouteID, offset, limit int) (port.RecordedRouteDetail, error) {
	out, err := c.Repo.Detail(ctx, viewer, id, offset, limit)
	if err != nil {
		return out, err
	}
	zones, err := c.zones(ctx, viewer, out.Owner)
	if err != nil {
		return port.RecordedRouteDetail{}, err
	}
	out.Track = out.Track.Mask(zones)
	return out, nil
}

func (c *Catalog) zones(ctx context.Context, viewer, owner domain.UserID) ([]domain.PrivacyZone, error) {
	if viewer == owner {
		return nil, nil
	}
	return c.Zones.ListPrivacyZones(ctx, owner)
}
