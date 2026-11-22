package waas

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type source struct{ calls int }

func (s *source) Open(context.Context, string) (port.RouteAsset, error) {
	s.calls++
	return port.RouteAsset{Body: io.NopCloser(strings.NewReader("data"))}, nil
}
func TestCatalogWhitelist(t *testing.T) {
	for _, name := range []string{"../secret", "https://example.com/terrain.png", "profile-unknown.js", "focus-climb-admin.bin", "", "terrain.png?url=private"} {
		t.Run(name, func(t *testing.T) {
			s := &source{}
			_, err := (&Catalog{Source: s}).Open(context.Background(), name)
			if err != domain.ErrNotFound || s.calls != 0 {
				t.Fatalf("invalid name reached source: %v, %d calls", err, s.calls)
			}
		})
	}
	for _, name := range []string{"terrain.png", "route-spines.js", "circuits-climb-sn.js", "max-climbs-sn.bin", "extended-gravel-te.bin", "schelde-kanaal.js"} {
		s := &source{}
		asset, err := (&Catalog{Source: s}).Open(context.Background(), name)
		if err != nil || s.calls != 1 {
			t.Fatalf("valid name %s rejected: %v", name, err)
		}
		_ = asset.Body.Close()
	}
}
