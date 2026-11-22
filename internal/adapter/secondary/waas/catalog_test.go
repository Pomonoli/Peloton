package waas

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicCatalogFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("member credentials reached public source")
		}
		if r.URL.Path != "/terrain-data.js" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, "window.WAAS_DATA = {};")
	}))
	defer server.Close()
	asset, err := NewCatalog(server.URL, server.Client()).Open(context.Background(), "terrain-data.js")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = asset.Body.Close() }()
	body, _ := io.ReadAll(asset.Body)
	if string(body) != "window.WAAS_DATA = {};" || asset.ETag != `"v1"` {
		t.Fatal("asset changed")
	}
}

func TestCatalogRejectsRedirectsAndLargeResponses(t *testing.T) {
	for _, mode := range []string{"cross-origin", "loop", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "cross-origin":
					http.Redirect(w, r, "https://example.invalid/private", http.StatusFound)
				case "loop":
					http.Redirect(w, r, "/again", http.StatusFound)
				case "oversized":
					w.Header().Set("Content-Length", "33554433")
				}
			}))
			defer server.Close()
			_, err := NewCatalog(server.URL, server.Client()).Open(context.Background(), "terrain-data.js")
			if err == nil {
				t.Fatal("unsafe response accepted")
			}
			if calls > 5 {
				t.Fatalf("redirect limit exceeded: %d", calls)
			}
		})
	}
}
