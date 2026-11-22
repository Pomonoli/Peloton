package waas

import (
	"context"
	"regexp"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Catalog exposes only known public Waasland datasets. It never accepts URLs.
type Catalog struct{ Source port.RouteCatalog }

var assetName = regexp.MustCompile(`^(terrain-data\.js|route-spines\.js|terrain\.png|schelde-kanaal\.js|long-flow\.js|profile-(flow|cobbles|gravel)\.js|(circuits|priority)-(climb|flow|cobbles|gravel)-(sn|wm|ti|te)\.js|(climb-zones|long-climbs)-(sn|wm|ti|te)\.js|(focus|extended)-(climb|flow|cobbles|gravel)-(sn|wm|ti|te)\.bin|max-climbs-(sn|wm|ti|te)\.bin)$`)

func (c *Catalog) Open(ctx context.Context, name string) (port.RouteAsset, error) {
	if !assetName.MatchString(name) {
		return port.RouteAsset{}, domain.ErrNotFound
	}
	return c.Source.Open(ctx, name)
}
