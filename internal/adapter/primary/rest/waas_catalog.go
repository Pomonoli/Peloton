package rest

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/waas"
)

// WaasCatalog serves public route data. Member activities are never involved.
type WaasCatalog struct{ Catalog *waas.Catalog }

func (h *WaasCatalog) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/route-planner/waasland/data/{file}", h.serve)
}

func (h *WaasCatalog) serve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	asset, err := h.Catalog.Open(r.Context(), name)
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Regional route data is temporarily unavailable.", http.StatusBadGateway)
		return
	}
	defer func() { _ = asset.Body.Close() }()
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", asset.ETag)
	if asset.ETag != "" && r.Header.Get("If-None-Match") == asset.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	switch {
	case strings.HasSuffix(name, ".bin"):
		w.Header().Set("Content-Type", "application/octet-stream")
	case strings.HasSuffix(name, ".png"):
		w.Header().Set("Content-Type", "image/png")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	if asset.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(asset.Size, 10))
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, io.LimitReader(asset.Body, 32<<20))
}
