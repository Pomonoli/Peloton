// Package tailwindkompass reads the segment definitions available on the
// signed-out Tailwind KOMpass map.
package tailwindkompass

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const (
	mapOrigin        = "https://tailwindkompass.com"
	apiOrigin        = "https://api.tailwindkompass.com"
	resultLimit      = 1000
	definitionFields = "id,name,distance,polyline,start_latitude,start_longitude,end_latitude,end_longitude,total_elevation_gain,average_grade"
)

var (
	mainAsset = regexp.MustCompile(`/assets/main-[a-zA-Z0-9_-]+\.js`)
	anonToken = regexp.MustCompile(`"(eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)"`)
)

type Provider struct {
	client         *http.Client
	mapURL, apiURL string

	mu         sync.Mutex
	key        string
	keyExpires time.Time
}

func New() *Provider {
	return &Provider{
		client: &http.Client{
			Timeout:       12 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		mapURL: mapOrigin,
		apiURL: apiOrigin,
	}
}

func (*Provider) Name() string { return "tailwind-kompass" }

type viewportRow struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	Distance      float64     `json:"distance"`
	Polyline      string      `json:"polyline"`
	StartLat      *float64    `json:"start_latitude"`
	StartLon      *float64    `json:"start_longitude"`
	EndLat        *float64    `json:"end_latitude"`
	EndLon        *float64    `json:"end_longitude"`
	ElevationGain *float64    `json:"total_elevation_gain"`
	AverageGrade  *float64    `json:"average_grade"`
}

func (p *Provider) Discover(ctx context.Context, box port.BoundingBox) (port.SegmentDiscoveryPage, error) {
	key, err := p.browserKey(ctx)
	if err != nil {
		return port.SegmentDiscoveryPage{}, err
	}
	body, err := json.Marshal(struct {
		West       float64 `json:"west"`
		South      float64 `json:"south"`
		East       float64 `json:"east"`
		North      float64 `json:"north"`
		Intersect  bool    `json:"include_intersecting"`
		MaxResults int     `json:"max_results"`
	}{box.MinLon, box.MinLat, box.MaxLon, box.MaxLat, true, resultLimit})
	if err != nil {
		return port.SegmentDiscoveryPage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.apiURL+"/rest/v1/rpc/get_segments_in_bbox?select="+definitionFields, bytes.NewReader(body))
	if err != nil {
		return port.SegmentDiscoveryPage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", key)
	req.Header.Set("Origin", mapOrigin)
	data, status, err := p.read(req)
	if status == http.StatusUnauthorized {
		p.mu.Lock()
		p.keyExpires = time.Time{}
		p.mu.Unlock()
	}
	if err != nil {
		return port.SegmentDiscoveryPage{}, err
	}
	var rows []viewportRow
	if err := json.Unmarshal(data, &rows); err != nil || rows == nil {
		return port.SegmentDiscoveryPage{}, errors.New("invalid segment viewport response")
	}
	result := port.SegmentDiscoveryPage{Saturated: len(rows) >= resultLimit}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		seg, ok := row.segment(p.Name())
		if ok && !seen[*seg.ExternalID] {
			seen[*seg.ExternalID] = true
			result.Segments = append(result.Segments, seg)
		}
	}
	if len(rows) > 0 && len(result.Segments) == 0 {
		return port.SegmentDiscoveryPage{}, errors.New("viewport contains no valid segment geometry")
	}
	return result, nil
}

// The browser map serves its anonymous client key in its public bundle.
// Only accept the anonymous role. No account session is used.
func (p *Provider) browserKey(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.keyExpires) {
		return p.key, nil
	}
	get := func(url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		data, _, err := p.read(req)
		return data, err
	}
	html, err := get(p.mapURL + "/explore")
	if err != nil {
		return "", err
	}
	asset := mainAsset.Find(html)
	if asset == nil {
		return "", errors.New("public map bundle not found")
	}
	script, err := get(p.mapURL + string(asset))
	if err != nil {
		return "", err
	}
	for _, match := range anonToken.FindAllSubmatch(script, -1) {
		key := string(match[1])
		claims, err := base64.RawURLEncoding.DecodeString(strings.Split(key, ".")[1])
		var token struct {
			Role string `json:"role"`
		}
		if err == nil && json.Unmarshal(claims, &token) == nil && token.Role == "anon" {
			p.key, p.keyExpires = key, time.Now().Add(time.Hour)
			return key, nil
		}
	}
	return "", errors.New("public map anonymous key not found")
}

func (p *Provider) read(req *http.Request) ([]byte, int, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Tailwind KOMpass request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("Tailwind KOMpass returned HTTP %d", resp.StatusCode)
	}
	const maxBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read Tailwind KOMpass response: %w", err)
	}
	if len(data) > maxBytes {
		return nil, resp.StatusCode, errors.New("Tailwind KOMpass response exceeds 8 MiB")
	}
	return data, resp.StatusCode, nil
}

func (r viewportRow) segment(provider string) (domain.Segment, bool) {
	id, name := r.ID.String(), strings.TrimSpace(r.Name)
	seg := domain.Segment{
		Source: domain.SegmentSourceStravaCatalog, Scope: domain.SegmentScopeInstance,
		ExternalID: &id, CatalogProvider: &provider, Name: name, ActivityType: domain.ActivityTypeRide,
		Polyline: r.Polyline, PolylinePrecision: 5, DistanceM: r.Distance,
		ElevationGainM: r.ElevationGain, AvgGrade: r.AverageGrade,
	}
	if name == "" || len(name) > 512 || r.Distance > 100_000 || len(r.Polyline) > 96<<10 || seg.Validate() != nil ||
		r.StartLat == nil || r.StartLon == nil || r.EndLat == nil || r.EndLon == nil {
		return domain.Segment{}, false
	}
	points, err := seg.DecodedPolyline()
	if err != nil || len(points) < 2 {
		return domain.Segment{}, false
	}
	start, end := domain.GeoPoint{Lat: *r.StartLat, Lon: *r.StartLon}, domain.GeoPoint{Lat: *r.EndLat, Lon: *r.EndLon}
	for _, point := range append([]domain.GeoPoint{start, end}, points...) {
		if !(math.Abs(point.Lat) <= 90 && math.Abs(point.Lon) <= 180) {
			return domain.Segment{}, false
		}
	}
	length := domain.PolylineDistanceM(points)
	return seg, domain.HaversineMeters(start, points[0]) <= 10 &&
		domain.HaversineMeters(end, points[len(points)-1]) <= 10 &&
		length >= r.Distance/2 && length <= r.Distance*1.5
}
