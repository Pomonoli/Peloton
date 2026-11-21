package segment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type discoveryCacheEntry struct {
	at        time.Time
	saturated bool
}

type testDiscoveryCache struct {
	windows map[port.SegmentDiscoveryWindow]discoveryCacheEntry
	saved   []domain.Segment
}

func (c *testDiscoveryCache) SaveSegment(_ context.Context, seg domain.Segment) error {
	c.saved = append(c.saved, seg)
	return nil
}

func (c *testDiscoveryCache) DiscoveryWindow(_ context.Context, _ string, window port.SegmentDiscoveryWindow, since time.Time) (bool, bool, error) {
	entry := c.windows[window]
	return !entry.at.Before(since), entry.saturated, nil
}

func (c *testDiscoveryCache) SaveDiscoveryWindow(_ context.Context, _ string, window port.SegmentDiscoveryWindow, at time.Time, saturated bool) error {
	c.windows[window] = discoveryCacheEntry{at: at, saturated: saturated}
	return nil
}

func (*testDiscoveryCache) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type discoveryFunc func(context.Context, port.BoundingBox) (port.SegmentDiscoveryPage, error)

func (discoveryFunc) Name() string { return "test" }
func (f discoveryFunc) Discover(ctx context.Context, box port.BoundingBox) (port.SegmentDiscoveryPage, error) {
	return f(ctx, box)
}

func TestDiscoveryCacheSubdividesAndRetriesPartialWindows(t *testing.T) {
	cache := &testDiscoveryCache{windows: make(map[port.SegmentDiscoveryWindow]discoveryCacheEntry)}
	root := port.SegmentDiscoveryWindow{X: 4602, Y: 3505}
	var boxes []port.BoundingBox
	id := "123"
	d := DiscoverSegments{Cache: cache, Tx: cache, Provider: discoveryFunc(func(_ context.Context, box port.BoundingBox) (port.SegmentDiscoveryPage, error) {
		boxes = append(boxes, box)
		switch len(boxes) {
		case 1:
			return port.SegmentDiscoveryPage{Saturated: true}, nil
		case 2:
			return port.SegmentDiscoveryPage{}, nil
		case 3:
			return port.SegmentDiscoveryPage{}, errors.New("temporarily unavailable")
		default:
			return port.SegmentDiscoveryPage{Segments: []domain.Segment{{ExternalID: &id}, {ExternalID: &id}}}, nil
		}
	})}

	if _, err := d.discover(t.Context(), []port.SegmentDiscoveryWindow{root}); err == nil || len(cache.windows) != 2 {
		t.Fatalf("failed request should leave its successful siblings cached: %v, %d cached", err, len(cache.windows))
	}
	if _, err := d.discover(t.Context(), []port.SegmentDiscoveryWindow{root}); err != nil {
		t.Fatal(err)
	}
	if len(cache.windows) != 5 || len(boxes) != 6 || boxes[2] != boxes[3] {
		t.Fatalf("expected four subdivisions and a retry of only the failed cell: %d windows, %d requests", len(cache.windows), len(boxes))
	}
	if len(cache.saved) != 1 || *cache.saved[0].ExternalID != id {
		t.Fatalf("expected one definition for the repeated ID, got %+v", cache.saved)
	}
	first := cache.saved[0].ID
	if result, err := d.discover(t.Context(), []port.SegmentDiscoveryWindow{root}); err != nil || len(boxes) != 6 || result.CachedWindows != 5 || result.ProviderRequests != 0 {
		t.Fatalf("fresh cache should avoid provider requests: %+v, %d, %v", result, len(boxes), err)
	}
	for cell, entry := range cache.windows {
		entry.at = entry.at.Add(-discoveryCacheTTL)
		cache.windows[cell] = entry
	}
	if _, err := d.discover(t.Context(), []port.SegmentDiscoveryWindow{root}); err != nil || len(boxes) != 7 ||
		len(cache.saved) != 2 || cache.saved[1].ID != first {
		t.Fatalf("expired cells should refresh the same definition: %d boxes, %+v, %v", len(boxes), cache.saved, err)
	}
}

func TestRouteWindowsUseOnlyDistinctCellsOnTheRoute(t *testing.T) {
	lat, first, nearby, second := 50.81, 4.306, 4.307, 4.35
	sample := func(lon *float64) domain.StreamSample { return domain.StreamSample{Latitude: &lat, Longitude: lon} }
	windows := routeWindows([]domain.StreamSample{sample(&first), sample(&nearby), {}, sample(&second), sample(&first)})
	if len(windows) != 2 {
		t.Fatalf("expected two distinct discovery regions, got %+v", windows)
	}
	if box := discoveryBounds(windows[0]); !(box.MinLat < lat && lat < box.MaxLat && box.MinLon < first && first < box.MaxLon && box.MaxLon < second) {
		t.Fatal("first cell does not cover the first region only")
	}
	if windows := routeWindows([]domain.StreamSample{sample(&first), {}}); len(windows) != 0 {
		t.Fatal("a single GPS sample should not trigger discovery")
	}
}
