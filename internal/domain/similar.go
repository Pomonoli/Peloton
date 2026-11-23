package domain

import (
	"sort"
	"time"
)

// SimilarRow is one activity of a same-route group.
type SimilarRow struct {
	SimilarActivity
	IsCurrent       bool
	PaceSPerKm      *float64 // moving seconds per km when distance and time are known
	OverlapPct      float64  // geographical overlap, 0..1
	AverageSpeedKmh *float64
}

// SimilarGroup is the reference activity with its same-route matches,
// chronological, ranked by average moving speed or equivalent pace.
type SimilarGroup struct {
	TargetSegments                int
	Rows                          []SimilarRow
	BestMovingS                   int64
	CurrentMovingS                int64
	CurrentRank                   int // 1-based
	IsBest                        bool
	BestSpeedKmh, CurrentSpeedKmh *float64
}

// BuildSimilarGroup assembles the group of act and its matches.
func BuildSimilarGroup(act Activity, matches []SimilarActivity) SimilarGroup {
	target := 0
	if len(matches) > 0 {
		target = matches[0].TargetSegments
	}
	g := SimilarGroup{TargetSegments: target}
	g.Rows = append(g.Rows, similarRow(ReferenceAsSimilar(act, target), true, target))
	for _, m := range matches {
		g.Rows = append(g.Rows, similarRow(m, false, target))
	}
	sort.SliceStable(g.Rows, func(i, j int) bool { return g.Rows[i].StartTime.Before(g.Rows[j].StartTime) })
	for _, r := range g.Rows {
		if r.AverageSpeedKmh != nil && (g.BestSpeedKmh == nil || *r.AverageSpeedKmh > *g.BestSpeedKmh) {
			g.BestSpeedKmh, g.BestMovingS = r.AverageSpeedKmh, r.MovingS
		}
		if r.IsCurrent {
			g.CurrentSpeedKmh, g.CurrentMovingS = r.AverageSpeedKmh, r.MovingS
		}
	}
	if g.CurrentSpeedKmh != nil {
		g.CurrentRank = 1
		for _, r := range g.Rows {
			if r.AverageSpeedKmh != nil && *r.AverageSpeedKmh > *g.CurrentSpeedKmh {
				g.CurrentRank++
			}
		}
		g.IsBest = g.CurrentRank == 1
	}
	return g
}

// ReferenceAsSimilar projects the reference activity into the SimilarActivity
// shape so it sits in the same group as its matches.
func ReferenceAsSimilar(a Activity, targetSegments int) SimilarActivity {
	sa := SimilarActivity{
		ActivityID:     a.ID,
		Title:          a.Title,
		Type:           string(a.Type),
		StartTime:      a.StartTime,
		DistanceM:      a.Summary.DistanceM,
		MovingS:        int64(a.MovingDuration.Seconds()),
		ElapsedS:       int64(a.ElapsedDuration.Seconds()),
		SharedSegments: targetSegments,
		TargetSegments: targetSegments,
	}
	if a.Summary.AvgHeartRateBpm != nil {
		v := *a.Summary.AvgHeartRateBpm
		sa.AvgHeartRateBpm = &v
	}
	if a.Summary.AvgPowerW != nil {
		v := *a.Summary.AvgPowerW
		sa.AvgPowerW = &v
	}
	return sa
}

func similarRow(sa SimilarActivity, current bool, target int) SimilarRow {
	r := SimilarRow{SimilarActivity: sa, IsCurrent: current}
	if speed := ActivityAverageSpeed(sa.DistanceM, time.Duration(sa.MovingS)*time.Second); speed != nil {
		r.PaceSPerKm = new(1000 / *speed)
		r.AverageSpeedKmh = new(3.6 * *speed)
	}
	if current {
		r.OverlapPct = 1
	} else if sa.RouteOverlap > 0 {
		r.OverlapPct = sa.RouteOverlap
	} else if target > 0 {
		r.OverlapPct = float64(sa.SharedSegments) / float64(target)
	}
	return r
}

// PickLaps returns the lap segmentation of the source with the most laps:
// the merge engine's lap winner is a single source and "most laps" is a
// robust proxy without depending on provenance internals.
func PickLaps(sources []ActivitySource) []ActivityLap {
	var laps []ActivityLap
	for _, s := range sources {
		if len(s.Parsed.Laps) > len(laps) {
			laps = s.Parsed.Laps
		}
	}
	return laps
}
