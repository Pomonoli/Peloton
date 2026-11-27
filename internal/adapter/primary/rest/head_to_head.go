package rest

import (
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

type HeadToHeadHandlers struct {
	Auth  *Auth
	Views *view.HeadToHead
	Log   *slog.Logger
}

func (h *HeadToHeadHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/head-to-head", h.compare)
	mux.HandleFunc("GET /api/head-to-head/shared", h.shared)
}

func (h *HeadToHeadHandlers) activityReader(w http.ResponseWriter, r *http.Request) (domain.UserID, bool) {
	caller, ok := h.Auth.Callers.Authenticate(r.Context(), credentialOf(r))
	if !ok {
		http.Error(w, "Unauthenticated", http.StatusUnauthorized)
		return domain.UserID{}, false
	}
	if caller.Scopes != nil && !slices.Contains(caller.Scopes, domain.ScopeActivitiesRead) {
		http.Error(w, "Activity read permission required", http.StatusForbidden)
		return domain.UserID{}, false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	return caller.UserID, true
}

func comparisonRiders(query url.Values) (string, string) {
	second := query.Get("second")
	if second == "" {
		second = query.Get("member")
	}
	return query.Get("first"), second
}

func (h *HeadToHeadHandlers) compare(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.activityReader(w, r)
	if !ok {
		return
	}
	first, second := comparisonRiders(r.URL.Query())
	result, err := h.Views.Compare(recordReadContext(r.Context(), r), viewer, first, second)
	if err != nil {
		h.Log.Debug("load head-to-head comparison", "error", err)
		writeActionError(w, err, "Unable to load the comparison")
		return
	}

	segments := make([]map[string]any, 0, len(result.Segments))
	for _, s := range result.Segments {
		segments = append(segments, map[string]any{
			"id": s.ID.String(), "name": s.Name, "distance_m": s.Distance,
			"you": comparisonEffortJSON(&s.You), "opponent": comparisonEffortJSON(&s.Opponent),
			"you_speed_kmh": s.YouSpeedKmh, "opponent_speed_kmh": s.OpponentSpeedKmh,
			"time_difference_s": s.TimeDifferenceS, "speed_difference_kmh": s.SpeedDifferenceKmh,
			"winner": s.Winner,
		})
	}
	records := make([]map[string]any, 0, len(result.Records))
	for _, p := range result.Records {
		row := map[string]any{
			"activity_type": p.Bucket.ActivityType, "metric": p.Bucket.Metric,
			"window_kind": p.Bucket.WindowKind, "window_value": p.Bucket.WindowValue,
			"you": comparisonEffortJSON(p.You), "opponent": comparisonEffortJSON(p.Opponent),
			"winner": p.Winner, "difference": nil, "difference_percent": nil,
		}
		if p.Winner != "" {
			row["difference"], row["difference_percent"] = p.Difference, p.DifferencePercent
		}
		records = append(records, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"first": userCardJSON(result.First), "opponent": userCardJSON(result.Opponent), "segments": segments, "records": records,
		"segment_score": comparisonScoreJSON(result.SegmentScore),
		"record_score":  comparisonScoreJSON(result.RecordScore),
	})
}

func comparisonEffortJSON(e *domain.ComparisonEffort) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{"activity_id": e.ActivityID.String(), "value": e.Value}
}

func comparisonScoreJSON(s view.ComparisonScore) map[string]int {
	return map[string]int{"wins": s.Wins, "losses": s.Losses, "ties": s.Ties, "comparable": s.Wins + s.Losses + s.Ties}
}

func (h *HeadToHeadHandlers) shared(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.activityReader(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	first, second := comparisonRiders(query)
	from, to := dashParseDate(query.Get("from")), dashParseDate(query.Get("to"))
	if query.Get("from") != "" && from.IsZero() || query.Get("to") != "" && to.IsZero() {
		writeActionError(w, port.NewActionError(port.ActionErrorInvalid, "Invalid comparison date range"), "")
		return
	}
	if len(query.Get("to")) == len(time.DateOnly) {
		to = to.AddDate(0, 0, 1)
	}
	result, err := h.Views.Shared(r.Context(), viewer, first, second, from, to)
	if err != nil {
		h.Log.Debug("load shared activities", "error", err)
		writeActionError(w, err, "Unable to load shared activities")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
