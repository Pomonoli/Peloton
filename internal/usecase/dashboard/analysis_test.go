package dashboard

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func TestAnalysisRangeAndCoverage(t *testing.T) {
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	repo := inmem.NewActivityRepo()
	stats := NewStats(repo, nil, nil, nil, func() time.Time { return now })
	rider, other := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	add := func(user domain.UserID, daysAgo int, summary domain.ActivitySummary, hidden bool) domain.ActivityID {
		t.Helper()
		id := domain.ActivityID(uuid.New())
		if err := repo.SaveActivity(t.Context(), domain.Activity{
			ID: id, UserID: user, StartTime: now.AddDate(0, 0, -daysAgo), Summary: summary, HiddenByAdmin: hidden,
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	add(rider, 4000, domain.ActivitySummary{TSS: new(50.0)}, false)
	add(rider, 91, domain.ActivitySummary{TSS: new(250.0)}, false)
	zero := add(rider, 30, domain.ActivitySummary{TSS: new(0.0)}, false)
	sourceID := domain.SourceID(uuid.New())
	if err := repo.SaveSource(t.Context(), domain.ActivitySource{
		ID: sourceID, ActivityID: zero, Provider: "manual",
		Parsed: domain.ActivitySourcePayload{Summary: domain.ActivitySummary{TSS: new(0.0)}},
	}); err != nil {
		t.Fatal(err)
	}
	activity := repo.Activities[zero]
	activity.MergeProvenance = domain.MergeProvenance{domain.FieldGroupTrainingLoad: sourceID}
	if err := repo.SaveActivity(t.Context(), activity); err != nil {
		t.Fatal(err)
	}
	add(rider, 8, domain.ActivitySummary{TSS: new(70.2), AvgHeartRateBpm: new(145)}, false)
	add(rider, 8, domain.ActivitySummary{TSS: new(34.125)}, false)
	add(rider, 8, domain.ActivitySummary{TSS: new(math.NaN())}, false)
	add(rider, 7, domain.ActivitySummary{}, false)
	add(rider, 6, domain.ActivitySummary{TSS: new(-12.0)}, false)
	add(rider, 5, domain.ActivitySummary{TSS: new(math.Inf(1))}, false)
	add(rider, 3, domain.ActivitySummary{TSS: new(400.0)}, true)

	view := port.WithActivityReadViewer(t.Context(), false)
	all, err := stats.Analysis(view, rider, TrainingLoadMaxDays)
	if err != nil {
		t.Fatal(err)
	}
	if all.Start != now.AddDate(0, 0, -4000).Format(time.DateOnly) || all.End != now.Format(time.DateOnly) {
		t.Fatalf("unexpected All range: %s to %s", all.Start, all.End)
	}
	for _, days := range []int{90, 180, 365} {
		narrow, err := stats.Analysis(view, rider, days)
		if err != nil {
			t.Fatal(err)
		}
		if narrow.Start != now.AddDate(0, 0, 1-days).Format(time.DateOnly) || len(narrow.Load) != days ||
			!slices.Equal(narrow.Load, all.Load[len(all.Load)-days:]) {
			t.Fatalf("changing to %d days changed overlapping load", days)
		}
	}
	narrow, err := stats.Analysis(view, rider, 90)
	if err != nil {
		t.Fatal(err)
	}
	if narrow.Coverage != (port.TrainingStressCoverage{Imported: 1, SensorEstimate: 1, DurationEstimate: 1, Missing: 4}) ||
		narrow.RecentScored != 3 || narrow.RecentTotal != 7 {
		t.Fatalf("unexpected coverage: %+v, recent %d/%d", narrow.Coverage, narrow.RecentScored, narrow.RecentTotal)
	}
	if len(narrow.Stress) != 5 || narrow.Stress[0].Scored() != 1 || narrow.Stress[0].TSS != 0 ||
		narrow.Stress[1].Scored() != 2 || narrow.Stress[1].Missing != 1 || math.Abs(narrow.Stress[1].TSS-104.325) > 1e-9 ||
		narrow.Stress[2].Scored() != 0 || narrow.Stress[2].Missing != 1 {
		t.Fatalf("unexpected daily stress: %+v", narrow.Stress)
	}
	last := narrow.Load[len(narrow.Load)-1]
	if last.Date != narrow.End || last.CTL <= 0 || last.ATL >= narrow.Load[len(narrow.Load)-9].ATL {
		t.Fatalf("load did not carry forward and decay through the current UTC date: %+v", last)
	}

	owner, err := stats.Analysis(t.Context(), rider, 90)
	if err != nil || owner.Coverage.Scored() != 4 || owner.Load[len(owner.Load)-1].ATL <= last.ATL {
		t.Fatalf("owner's load should include their hidden ride: %v %+v", err, owner.Coverage)
	}
	empty, err := stats.Analysis(t.Context(), other, 90)
	if err != nil || len(empty.Load) != 0 || len(empty.Stress) != 0 {
		t.Fatalf("empty rider should have no scored history: %v %+v", err, empty)
	}
	add(other, 0, domain.ActivitySummary{}, false)
	missing, err := stats.Analysis(t.Context(), other, 90)
	if err != nil || len(missing.Load) != 0 || len(missing.Stress) != 1 || missing.Coverage.Missing != 1 {
		t.Fatalf("unscored history should retain its activity count: %v %+v", err, missing)
	}
}
