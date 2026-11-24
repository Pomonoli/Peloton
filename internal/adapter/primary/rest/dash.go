package rest

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/dashboard"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// DashboardHandlers serve dashboards for the selected member.
//
//	GET /api/overview            landing dashboard
//	GET /api/stats               stats page
//	GET /api/analysis?days=N     training load (default 180 days)
//	GET /api/activities/feed     faceted activity list (dash_feed.go)
//	GET /api/activities/heatmap  every GPS track (dash_feed.go)
type DashboardHandlers struct {
	Auth            *Auth
	Overviews       *dashboard.Overviews
	Searches        *dashboard.Searches
	Stats           *dashboard.Stats
	PerformanceMaps *dashboard.PerformanceMaps
	Visibility      *view.Visibility
	Log             *slog.Logger
}

// Mount registers the routes.
func (h *DashboardHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/overview", h.overview)
	mux.HandleFunc("GET /api/analysis", h.analysis)
	mux.HandleFunc("GET /api/stats", h.stats)
	mux.HandleFunc("GET /api/activities/feed", h.feed)
	mux.HandleFunc("GET /api/activities/heatmap", h.heatmap)
	if h.PerformanceMaps != nil {
		mux.HandleFunc("GET /api/activities/heatmap/performance", h.performanceHeatmap)
	}
}

// overview → { totals, by_sport:[{value,count}], recent:[activity...], this_week, last_week }
func (h *DashboardHandlers) overview(w http.ResponseWriter, r *http.Request) {
	ctx, userID, ok := h.Auth.requireMember(w, r)
	if !ok {
		return
	}
	ov, err := h.Overviews.Get(ctx, userID)
	if err != nil {
		http.Error(w, "totals failed", http.StatusInternalServerError)
		return
	}
	rec := make([]dashFeedActivity, 0, len(ov.Recent))
	for _, a := range ov.Recent {
		rec = append(rec, dashFeedActivityOf(h.redactActivity(ctx, a), false))
	}
	weeks := make([]map[string]any, 0, len(ov.CyclingWeeks))
	for _, week := range ov.CyclingWeeks {
		weeks = append(weeks, map[string]any{"start": week.Start.Format("2006-01-02"), "distance_m": week.DistanceM})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cycling_weeks": weeks,
		"totals":        dashTotalsJSON(ov.Totals),
		"by_sport":      dashFacets(ov.BySport),
		"recent":        rec,
		"this_week":     dashTotalsJSON(ov.ThisWeek),
		"last_week":     dashTotalsJSON(ov.LastWeek),
	})
}

func dashTotalsJSON(t port.ActivityTotalsResult) map[string]any {
	return map[string]any{
		"count": t.Count, "distance_m": t.DistanceM,
		"moving_s": t.MovingS, "elevation_gain_m": t.ElevationGainM,
	}
}

// stats → { totals, by_sport, by_discipline, by_year, records:{longest_distance, longest_duration} }
func (h *DashboardHandlers) stats(w http.ResponseWriter, r *http.Request) {
	ctx, userID, ok := h.Auth.requireMember(w, r)
	if !ok {
		return
	}
	st, err := h.Stats.Get(ctx, userID)
	if err != nil {
		http.Error(w, "stats failed", http.StatusInternalServerError)
		return
	}
	yearOut := make([]map[string]any, 0, len(st.ByYear))
	for _, y := range st.ByYear {
		yearOut = append(yearOut, map[string]any{
			"year": y.Year, "count": y.Count, "distance_m": y.DistanceM,
			"moving_s": y.MovingS, "elevation_gain_m": y.ElevationGainM,
		})
	}
	sports := make([]map[string]any, 0, len(st.SportTotals))
	for _, totals := range st.SportTotals {
		row := dashTotalsJSON(totals.ActivityTotalsResult)
		row["value"], row["speed_kmh"] = totals.Type, totals.SpeedKmh
		sports = append(sports, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sport_totals":  sports,
		"totals":        dashTotalsJSON(st.Totals),
		"by_sport":      dashFacets(st.BySport),
		"by_discipline": dashFacets(st.ByDiscipline),
		"by_year":       yearOut,
		"records": map[string]any{
			"longest_distance": dashStatsRecordJSON(h.statsRecord(ctx, st.LongestDistance)),
			"longest_duration": dashStatsRecordJSON(h.statsRecord(ctx, st.LongestDuration)),
		},
	})
}

func (h *DashboardHandlers) redactActivity(ctx context.Context, activity domain.Activity) domain.Activity {
	if !port.IsCommunityActivityRead(ctx) {
		return activity
	}
	id, _ := port.CommunityViewer(ctx)
	viewer, _ := h.Visibility.Resolve(ctx, activity, &id, false)
	return viewer.Redact(activity)
}

func (h *DashboardHandlers) statsRecord(ctx context.Context, activity *domain.Activity) *domain.Activity {
	if activity == nil {
		return nil
	}
	redacted := h.redactActivity(ctx, *activity)
	if !port.IsCommunityActivityRead(ctx) || redacted.Summary.AvgSpeedMps != nil {
		redacted.Summary.AvgSpeedMps = domain.ActivityAverageSpeed(redacted.Summary.DistanceM, redacted.MovingDuration)
	}
	return &redacted
}

// dashStatsRecordJSON renders a record-holding activity; nil when none.
func dashStatsRecordJSON(a *domain.Activity) map[string]any {
	if a == nil {
		return nil
	}
	var dist float64
	if a.Summary.DistanceM != nil {
		dist = *a.Summary.DistanceM
	}
	return map[string]any{
		"id": a.ID.String(), "title": a.Title,
		"type": string(a.Type), "discipline": string(a.Discipline),
		"distance_m":         dist,
		"elapsed_duration_s": int64(a.ElapsedDuration.Seconds()),
		"moving_duration_s":  int64(a.MovingDuration.Seconds()),
		"avg_speed_mps":      a.Summary.AvgSpeedMps,
	}
}

// analysis → { dates, ctl, atl, tsb, tss, current:{ctl,atl,tsb} }
func (h *DashboardHandlers) analysis(w http.ResponseWriter, r *http.Request) {
	ctx, userID, ok := h.Auth.requireMember(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	series, err := h.Stats.TrainingLoad(ctx, userID, days)
	if err != nil {
		writeActionError(w, err, "analysis failed")
		return
	}
	out := dashAnalysisJSON(series)
	coverage, err := h.Stats.TrainingStressCoverage(ctx, userID, days)
	if err != nil {
		h.Log.Warn("load training stress coverage", "error", err)
	} else {
		out["coverage"] = map[string]int{
			"imported": coverage.Imported, "sensor_estimate": coverage.SensorEstimate,
			"duration_estimate": coverage.DurationEstimate, "missing": coverage.Missing,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// dashAnalysisJSON renders the per-day series as parallel arrays.
func dashAnalysisJSON(series []domain.TrainingLoadDay) map[string]any {
	dates := make([]string, 0, len(series))
	ctl := make([]float64, 0, len(series))
	atl := make([]float64, 0, len(series))
	tsb := make([]float64, 0, len(series))
	tss := make([]float64, 0, len(series))
	observed := make([]*float64, 0, len(series))
	for _, d := range series {
		dates = append(dates, d.Date)
		ctl = append(ctl, d.CTL)
		atl = append(atl, d.ATL)
		tsb = append(tsb, d.TSB)
		tss = append(tss, d.TSS)
		var value *float64
		if d.HasTSS {
			value = &d.TSS
		}
		observed = append(observed, value)
	}
	current := map[string]any{"ctl": 0.0, "atl": 0.0, "tsb": 0.0}
	if n := len(series); n > 0 {
		current = map[string]any{"ctl": series[n-1].CTL, "atl": series[n-1].ATL, "tsb": series[n-1].TSB}
	}
	return map[string]any{
		"dates": dates, "ctl": ctl, "atl": atl, "tsb": tsb, "tss": tss,
		"current": current, "tss_observed": observed,
	}
}
