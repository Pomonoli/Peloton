package port

import (
	"context"
	"io"
)

// RouteCatalog provides public regional planning data, independent of members.
type RouteCatalog interface {
	Open(context.Context, string) (RouteAsset, error)
}

type RouteAsset struct {
	Body io.ReadCloser
	Size int64
	ETag string
}
