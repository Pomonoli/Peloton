package tailwindkompass

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func TestPublicViewportRequestAndDefinitionValidation(t *testing.T) {
	token := func(role string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"test"}`)) + "." +
			base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"role":%q}`, role)) + ".test"
	}
	anon := token("anon")
	// A known polyline encoding from the Google polyline format example.
	row := map[string]any{
		"id": 1234, "name": " Test segment ", "polyline": "_p~iF~ps|Uo}@o}@", "distance": 1800,
		"start_latitude": 38.5, "start_longitude": -120.2, "end_latitude": 38.51, "end_longitude": -120.19,
		"total_elevation_gain": 60, "average_grade": 3.3,
	}
	invalid := map[string]any{}
	for k, v := range row {
		invalid[k] = v
	}
	invalid["id"], invalid["end_longitude"] = 4321, -120.12

	var assetLoads, queries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/explore":
			fmt.Fprint(w, `<script src="/assets/main-test.js"></script>`)
		case "/assets/main-test.js":
			assetLoads.Add(1)
			fmt.Fprintf(w, "%q %q", token("service_role"), anon)
		case "/rest/v1/rpc/get_segments_in_bbox":
			if r.Method != http.MethodPost || r.Header.Get("apikey") != anon || r.URL.Query().Get("select") != definitionFields {
				t.Error("expected anonymous RPC requesting only definition fields")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 6 ||
				body["west"] != -121.0 || body["south"] != 38.0 || body["east"] != -120.0 || body["north"] != 39.0 ||
				body["max_results"] != 1000.0 || body["include_intersecting"] != true {
				t.Error("expected only bounds and geographic query parameters")
			}
			switch queries.Add(1) {
			case 1:
				rows := make([]map[string]any, resultLimit)
				for i := range rows {
					rows[i] = row
				}
				rows[0] = invalid
				_ = json.NewEncoder(w).Encode(rows)
			case 2:
				w.WriteHeader(http.StatusTooManyRequests)
			case 3:
				_ = json.NewEncoder(w).Encode([]map[string]any{invalid})
			case 4:
				fmt.Fprint(w, "[]")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := New()
	p.mapURL, p.apiURL = srv.URL, srv.URL
	box := port.BoundingBox{MinLat: 38, MaxLat: 39, MinLon: -121, MaxLon: -120}

	page, err := p.Discover(t.Context(), box)
	if err != nil || !page.Saturated || len(page.Segments) != 1 {
		t.Fatalf("expected one valid unique segment from a saturated result, got %+v, %v", page, err)
	}
	s := page.Segments[0]
	if *s.ExternalID != "1234" || s.Name != "Test segment" || s.Source != domain.SegmentSourceStravaCatalog ||
		*s.CatalogProvider != p.Name() || *s.ElevationGainM != 60 || *s.AvgGrade != 3.3 {
		t.Fatalf("unexpected segment metadata: %+v", s)
	}
	for range 2 {
		if _, err := p.Discover(t.Context(), box); err == nil {
			t.Fatal("expected rate-limited and invalid responses to fail")
		}
	}
	if page, err := p.Discover(t.Context(), box); err != nil || len(page.Segments) != 0 || page.Saturated || assetLoads.Load() != 1 {
		t.Fatalf("valid empty result should use the cached public key: %+v %v", page, err)
	}
}
