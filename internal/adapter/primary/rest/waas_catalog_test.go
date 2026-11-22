package rest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/waas"
)

type routeSource struct {
	err    error
	closed bool
}
type routeBody struct {
	io.Reader
	source *routeSource
}

func (b routeBody) Close() error { b.source.closed = true; return nil }
func (s *routeSource) Open(context.Context, string) (port.RouteAsset, error) {
	if s.err != nil {
		return port.RouteAsset{}, s.err
	}
	return port.RouteAsset{Body: routeBody{strings.NewReader("public-data"), s}, Size: 11, ETag: `"v1"`}, nil
}
func TestWaasPublicRouteData(t *testing.T) {
	for _, tc := range []struct {
		method, etag string
		err          error
		status       int
		body         string
	}{
		{"GET", "", nil, 200, "public-data"},
		{"HEAD", "", nil, 200, ""},
		{"GET", `"v1"`, nil, 304, ""},
		{"GET", "", domain.ErrNotFound, 404, ""},
		{"GET", "", errors.New("upstream"), 502, ""},
	} {
		t.Run(tc.method+tc.etag+http.StatusText(tc.status), func(t *testing.T) {
			source := &routeSource{err: tc.err}
			mux := http.NewServeMux()
			(&WaasCatalog{Catalog: &waas.Catalog{Source: source}}).Mount(mux)
			req := httptest.NewRequest(tc.method, "/api/route-planner/waasland/data/terrain-data.js", nil)
			req.Header.Set("If-None-Match", tc.etag)
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status %d", res.Code)
			}
			if tc.err == nil {
				if res.Body.String() != tc.body {
					t.Fatalf("body %q", res.Body.String())
				}
				if !source.closed {
					t.Error("upstream body leaked")
				}
				if res.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error("missing nosniff")
				}
				if tc.status == 200 && res.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Error("script is executable")
				}
			}
		})
	}
}
