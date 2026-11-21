package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const (
	flandersURL = "https://www.cyclinginflanders.cc"
	stravaURL   = "https://www.strava.com/segments/"
	pageLimit   = 2 << 20
)

// Small, geographically diverse sample from the Flandrien Challenge.
var catalogPaths = []string{"/koppenberg", "/oude-kwaremont", "/kemmelberg-belvedere"}

var (
	segmentIDPattern     = regexp.MustCompile(`^[1-9][0-9]*$`)
	linkPattern          = regexp.MustCompile(`field--name-field-strava-segment-link[^"]*"[^>]*>\s*<a\s+href="https://www\.strava\.com/segments/([1-9][0-9]*)"`)
	embedDistancePattern = regexp.MustCompile(`(?s)<ul class='list-stats'>\s*<li>\s*<div class='stat'>\s*<div class='span stat-subtext'>\s*Distance\s*</div>\s*<b class='stat-text'>\s*([0-9]+(?:[.,][0-9]+)?)\s+km\s*</b>`)
	mapPattern           = regexp.MustCompile(`<section\s+id="([^"]+-field-full-polyline-map)"`)
	settingsPattern      = regexp.MustCompile(`(?s)<script[^>]*data-drupal-selector="drupal-settings-json"[^>]*>(.*?)</script>`)
	embedNamePattern     = regexp.MustCompile(`(?s)<h1>\s*<a\s+[^>]*href="/segments/([1-9][0-9]*)(?:\?[^"]*)?">([^<]+)</a>`)
)

func fetchCatalog() (catalog, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	data := catalog{}
	seen := make(map[string]bool)
	for _, path := range catalogPaths {
		sourceURL := flandersURL + path
		page, err := fetchPage(client, sourceURL)
		if err != nil {
			return catalog{}, err
		}
		segment, err := parseDefinition(page, sourceURL)
		if err != nil {
			return catalog{}, fmt.Errorf("%s: %w", sourceURL, err)
		}
		if seen[segment.ID] {
			continue
		}
		page, err = fetchPage(client, stravaURL+segment.ID+"/embed")
		if err != nil {
			return catalog{}, err
		}
		err = applyStravaEmbed(page, &segment)
		if err != nil {
			return catalog{}, fmt.Errorf("Strava segment %s: %w", segment.ID, err)
		}
		if err := segment.validate(); err != nil {
			return catalog{}, err
		}
		data.Segments = append(data.Segments, segment)
		seen[segment.ID] = true
	}
	data.RetrievedAt = time.Now().UTC()
	return data, nil
}

func fetchPage(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if retry := resp.Header.Get("Retry-After"); retry != "" {
			return nil, fmt.Errorf("%s returned HTTP %d (Retry-After: %s)", url, resp.StatusCode, retry)
		}
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, pageLimit+1))
	if len(page) > pageLimit {
		return nil, fmt.Errorf("%s exceeded the response size limit", url)
	}
	return page, err
}

func parseDefinition(page []byte, sourceURL string) (definition, error) {
	segment := definition{SourceURL: sourceURL}
	link := linkPattern.FindSubmatch(page)
	if len(link) != 2 {
		return segment, errors.New("missing public Strava segment link")
	}
	segment.ID = string(link[1])

	mapID := mapPattern.FindSubmatch(page)
	settings := settingsPattern.FindSubmatch(page)
	if len(mapID) != 2 || len(settings) != 2 {
		return segment, errors.New("missing public segment map")
	}
	var maps struct {
		Leaflet map[string]struct {
			Features []struct {
				Type   string            `json:"type"`
				Points []domain.GeoPoint `json:"points"`
			} `json:"features"`
		} `json:"leaflet"`
	}
	if err := json.Unmarshal(settings[1], &maps); err != nil {
		return segment, fmt.Errorf("read public map settings: %w", err)
	}
	for _, feature := range maps.Leaflet[string(mapID[1])].Features {
		if feature.Type == "linestring" {
			if segment.Geometry != nil {
				return segment, errors.New("ambiguous segment geometry")
			}
			segment.Geometry = feature.Points
		}
	}
	return segment, nil
}

func (segment definition) validate() error {
	if !segmentIDPattern.MatchString(segment.ID) || segment.Name == "" ||
		segment.DistanceM <= 0 || len(segment.Geometry) < 2 {
		return fmt.Errorf("incomplete definition for Strava segment %q", segment.ID)
	}
	for _, point := range segment.Geometry {
		if !validPoint(point) {
			return fmt.Errorf("invalid geometry for Strava segment %s", segment.ID)
		}
	}
	return nil
}

func applyStravaEmbed(page []byte, segment *definition) error {
	linked := embedNamePattern.FindSubmatch(page)
	distance := embedDistancePattern.FindSubmatch(page)
	if len(linked) != 3 || string(linked[1]) != segment.ID || len(distance) != 2 {
		return errors.New("public embed did not verify the expected segment ID and distance")
	}
	km, err := strconv.ParseFloat(strings.ReplaceAll(string(distance[1]), ",", "."), 64)
	if err != nil {
		return err
	}
	segment.Name = strings.TrimSpace(html.UnescapeString(string(linked[2])))
	segment.DistanceM = math.Round(km * 1000)
	return nil
}
