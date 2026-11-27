package rest

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

type HeadToHeadHandlers struct {
	Auth  *Auth
	Views *view.HeadToHead
	Log   *slog.Logger
}

func (h *HeadToHeadHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/head-to-head", h.compare)
}

func (h *HeadToHeadHandlers) compare(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.Auth.Callers.Authenticate(r.Context(), credentialOf(r))
	if !ok {
		http.Error(w, "Unauthenticated", http.StatusUnauthorized)
		return
	}
	if caller.Scopes != nil && !slices.Contains(caller.Scopes, domain.ScopeActivitiesRead) {
		http.Error(w, "Activity read permission required", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	query := r.URL.Query()
	second := query.Get("second")
	if second == "" {
		second = query.Get("member")
	}
	result, err := h.Views.Compare(recordReadContext(r.Context(), r), caller.UserID, query.Get("first"), second)
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
