package segment

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const (
	discoveryTileDegrees = 0.04
	discoveryCacheTTL    = 7 * 24 * time.Hour
	discoveryMaxDepth    = 2
	discoveryMaxRequests = 48
)

type DiscoverSegments struct {
	Provider   port.SegmentDiscoveryProvider
	Cache      port.SegmentDiscoveryCache
	Activities port.ActivityAggregateReader
	Streams    port.StreamRepo
	Tx         port.TxManager
	Matcher    *MatchSegmentsForActivity
	Garmin     port.GarminSegmentBackfillRepo
}

// DiscoveryResult describes one scan without exposing points, bounds or times.
type DiscoveryResult struct {
	RouteWindows         int
	ProviderRequests     int
	ProviderResponses    int
	CachedWindows        int
	ValidatedDefinitions int
	CandidatesEvaluated  int
	EffortsWritten       int
	RequestLimitReached  bool
}

// ForSource discovers definitions near a live cycling activity and then
// matches locally, including any definitions already cached if the provider
// is down. The caller can retry a partial discovery.
func (d *DiscoverSegments) ForSource(ctx context.Context, user domain.UserID, sourceID domain.SourceID) (DiscoveryResult, error) {
	source, err := d.Activities.GetSource(ctx, sourceID)
	if errors.Is(err, domain.ErrNotFound) {
		return DiscoveryResult{}, nil
	}
	if err != nil {
		return DiscoveryResult{}, err
	}
	act, err := d.Activities.GetActivity(ctx, source.ActivityID)
	if errors.Is(err, domain.ErrNotFound) {
		return DiscoveryResult{}, nil
	}
	if err != nil {
		return DiscoveryResult{}, err
	}
	if act.UserID != user || act.IsDeleted() || act.HiddenByAdmin || act.Type != domain.ActivityTypeRide || source.Status != domain.SourceStatusActive {
		return DiscoveryResult{}, nil
	}
	stream, err := d.Streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: sourceID, Resolution: domain.StreamResolutionRaw})
	if err != nil {
		return DiscoveryResult{}, err
	}
	windows := routeWindows(stream.Samples)
	if len(windows) == 0 {
		return DiscoveryResult{}, d.completeGarminScan(ctx, source)
	}
	result, lookupErr := d.discover(ctx, windows)
	match, matchErr := d.Matcher.Execute(ctx, Input{ActivitySourceID: sourceID})
	result.CandidatesEvaluated, result.EffortsWritten = match.CandidatesEvaluated, match.EffortsWritten
	if errors.Is(matchErr, domain.ErrNotFound) {
		matchErr = nil
	}
	if err := errors.Join(lookupErr, matchErr); err != nil || result.RequestLimitReached {
		return result, err
	}
	return result, d.completeGarminScan(ctx, source)
}

func (d *DiscoverSegments) completeGarminScan(ctx context.Context, source domain.ActivitySource) error {
	if d.Garmin == nil || source.Provider != "garmin" || !source.Parsed.HasStream {
		return nil
	}
	return d.Garmin.CompleteGarminSegmentScan(ctx, source.ID, source.UpdatedAt)
}

// BackfillGarmin scans a bounded page of existing Garmin rides. It advances
// past per-ride failures, which remain pending for the next sweep.
func (d *DiscoverSegments) BackfillGarmin(ctx context.Context, after domain.SourceID, limit int) (domain.SourceID, error) {
	scans, err := d.Garmin.ListPendingGarminSegmentScans(ctx, after, limit)
	if err != nil {
		return after, err
	}
	var scanErrors error
	for _, scan := range scans {
		if ctx.Err() != nil {
			return after, errors.Join(scanErrors, ctx.Err())
		}
		after = scan.SourceID
		scanCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
		_, err := d.ForSource(scanCtx, scan.UserID, scan.SourceID)
		cancel()
		if err != nil {
			scanErrors = errors.Join(scanErrors, fmt.Errorf("scan Garmin source %s: %w", scan.SourceID, err))
		}
	}
	if len(scans) < limit {
		after = domain.SourceID{}
	}
	return after, scanErrors
}

func (d *DiscoverSegments) discover(ctx context.Context, windows []port.SegmentDiscoveryWindow) (DiscoveryResult, error) {
	now := time.Now().UTC()
	provider := d.Provider.Name()
	seen := make(map[string]bool)
	result := DiscoveryResult{RouteWindows: len(windows)}
	for len(windows) > 0 {
		window := windows[0]
		windows = windows[1:]
		fresh, saturated, err := d.Cache.DiscoveryWindow(ctx, provider, window, now.Add(-discoveryCacheTTL))
		if err != nil {
			return result, fmt.Errorf("read segment discovery cache: %w", err)
		}
		if fresh {
			result.CachedWindows++
		} else {
			if result.ProviderRequests >= discoveryMaxRequests {
				result.RequestLimitReached = true
				break
			}
			result.ProviderRequests++
			page, err := d.Provider.Discover(ctx, discoveryBounds(window))
			if err != nil {
				return result, fmt.Errorf("discover Strava segments: %w", err)
			}
			result.ProviderResponses++
			saturated = page.Saturated
			if err := d.cacheWindow(ctx, provider, window, now, page, seen); err != nil {
				return result, fmt.Errorf("cache Strava segments: %w", err)
			}
			result.ValidatedDefinitions = len(seen)
		}
		if saturated && window.Depth < discoveryMaxDepth {
			for x := 0; x < 2; x++ {
				for y := 0; y < 2; y++ {
					windows = append(windows, port.SegmentDiscoveryWindow{X: 2*window.X + x, Y: 2*window.Y + y, Depth: window.Depth + 1})
				}
			}
		}
	}
	return result, nil
}

func (d *DiscoverSegments) cacheWindow(ctx context.Context, provider string, window port.SegmentDiscoveryWindow, now time.Time, page port.SegmentDiscoveryPage, seen map[string]bool) error {
	// Save each cell atomically, including empty results. Sort the writes so
	// two instances refreshing overlapping cells acquire row locks in order.
	slices.SortFunc(page.Segments, func(a, b domain.Segment) int {
		return cmp.Compare(*a.ExternalID, *b.ExternalID)
	})
	return d.Tx.InTx(ctx, func(ctx context.Context) error {
		for _, seg := range page.Segments {
			id := *seg.ExternalID
			if seen[id] {
				continue
			}
			seg.ID = domain.SegmentID(uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://www.strava.com/segments/"+id)))
			if err := d.Cache.SaveSegment(ctx, seg); err != nil {
				return err
			}
			seen[id] = true
		}
		return d.Cache.SaveDiscoveryWindow(ctx, provider, window, now, page.Saturated)
	})
}

func routeWindows(samples []domain.StreamSample) []port.SegmentDiscoveryWindow {
	seen := make(map[port.SegmentDiscoveryWindow]bool)
	var windows []port.SegmentDiscoveryWindow
	valid := 0
	for _, sample := range samples {
		if sample.Latitude == nil || sample.Longitude == nil {
			continue
		}
		lat, lon := *sample.Latitude, *sample.Longitude
		if !(math.Abs(lat) <= 90 && math.Abs(lon) <= 180) {
			continue
		}
		valid++
		window := port.SegmentDiscoveryWindow{
			X: min(int((lon+180)/discoveryTileDegrees), 8999),
			Y: min(int((lat+90)/discoveryTileDegrees), 4499),
		}
		if !seen[window] {
			seen[window] = true
			windows = append(windows, window)
		}
	}
	if valid < 2 {
		return nil
	}
	// Use geographic order so request order does not encode ride direction.
	slices.SortFunc(windows, func(a, b port.SegmentDiscoveryWindow) int {
		if a.X == b.X {
			return cmp.Compare(a.Y, b.Y)
		}
		return cmp.Compare(a.X, b.X)
	})
	return windows
}

func discoveryBounds(w port.SegmentDiscoveryWindow) port.BoundingBox {
	step := discoveryTileDegrees / float64(int(1)<<w.Depth)
	lat, lon := float64(w.Y)*step-90, float64(w.X)*step-180
	const latPad = 0.0025
	lonPad := min(0.02, latPad/math.Cos((lat+step/2)*math.Pi/180))
	return port.BoundingBox{
		MinLat: max(-90, lat-latPad), MaxLat: min(90, lat+step+latPad),
		MinLon: max(-180, lon-lonPad), MaxLon: min(180, lon+step+lonPad),
	}
}
