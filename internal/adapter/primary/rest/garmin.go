package rest

import (
	"errors"
	"net/http"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/garminsync"
)

type GarminHandlers struct {
	Auth        *Auth
	Connections *garminsync.Connections
}

type garminStatus struct {
	Connected       bool       `json:"connected"`
	NeedsKey        bool       `json:"needs_key"`
	Busy            bool       `json:"busy"`
	HistoryComplete bool       `json:"history_complete"`
	HistoryRides    int        `json:"history_rides"`
	SummaryOnly     int        `json:"summary_only"`
	Skipped         int        `json:"skipped"`
	LastSync        *time.Time `json:"last_sync"`
	NextRun         *time.Time `json:"next_run"`
	Error           string     `json:"error"`
}

func (h *GarminHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connections/garmin", h.authorize(h.status))
	mux.HandleFunc("PUT /api/connections/garmin", h.authorize(h.connect))
	mux.HandleFunc("DELETE /api/connections/garmin", h.authorize(h.disconnect))
	mux.HandleFunc("POST /api/connections/garmin/sync", h.authorize(h.syncNow))
}

func (h *GarminHandlers) authorize(fn func(http.ResponseWriter, *http.Request, domain.UserID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		user, ok := h.Auth.UserNoOAuth(r)
		if !ok {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && !requireJSON(w, r) {
			return
		}
		fn(w, r, user)
	}
}

func (h *GarminHandlers) status(w http.ResponseWriter, r *http.Request, user domain.UserID) {
	state, err := h.Connections.Status(r.Context(), user)
	if errors.Is(err, domain.ErrNotFound) {
		writeJSON(w, http.StatusOK, garminStatus{})
		return
	}
	if err != nil {
		writeActionError(w, err, "status_failed")
		return
	}
	writeJSON(w, http.StatusOK, garminStatus{
		Connected: true, NeedsKey: state.NeedsKey, Busy: state.LeaseUntil != nil && state.LeaseUntil.After(time.Now()),
		HistoryComplete: state.HistoryComplete, HistoryRides: state.HistoryRides, SummaryOnly: state.SummaryOnly,
		Skipped: state.Skipped, LastSync: state.LastSync, NextRun: state.NextRun, Error: state.Error,
	})
}

func (h *GarminHandlers) connect(w http.ResponseWriter, r *http.Request, user domain.UserID) {
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid_key", http.StatusBadRequest)
		return
	}
	if err := h.Connections.Connect(r.Context(), user, body.Key); err != nil {
		writeActionError(w, err, "connect_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *GarminHandlers) disconnect(w http.ResponseWriter, r *http.Request, user domain.UserID) {
	if err := h.Connections.Disconnect(r.Context(), user); err != nil {
		writeActionError(w, err, "disconnect_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *GarminHandlers) syncNow(w http.ResponseWriter, r *http.Request, user domain.UserID) {
	if err := h.Connections.SyncNow(r.Context(), user); err != nil {
		writeActionError(w, err, "queue_failed")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
