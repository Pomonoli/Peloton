//go:build integration

package postgres

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func TestStravaCatalogEffortsUseOneLocalSourcePerActivity(t *testing.T) {
	ctx, pool := requirePool(t)
	rider := seedUser(t)
	acts, segs := NewActivityRepo(pool), NewSegmentRepo(pool)
	activityID, primaryID := seedMaintenanceActivity(t, acts, rider, "")
	act, err := acts.GetActivity(ctx, activityID)
	if err != nil {
		t.Fatal(err)
	}
	act.PrimaryStreamSourceID = &primaryID
	if err := acts.SaveActivity(ctx, act); err != nil {
		t.Fatal(err)
	}
	secondary, err := acts.GetSource(ctx, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	secondary.ID = domain.SourceID(uuid.New())
	secondary.ExternalID = secondary.ID.String()
	if err := acts.SaveSource(ctx, secondary); err != nil {
		t.Fatal(err)
	}

	id, provider := strconv.FormatInt(time.Now().UnixNano(), 10), "test"
	seg := domain.Segment{
		ID: domain.SegmentID(uuid.New()), Source: domain.SegmentSourceStravaCatalog, ExternalID: &id,
		CatalogProvider: &provider, Scope: domain.SegmentScopeInstance, Name: "Two-source track",
		ActivityType: domain.ActivityTypeRide, PolylinePrecision: 5, DistanceM: 1000,
		Polyline: encodePolyline5([]domain.GeoPoint{{Lat: 48, Lon: 11}, {Lat: 48.01, Lon: 11}}),
	}
	if err := segs.SaveSegment(ctx, seg); err != nil {
		t.Fatal(err)
	}
	add := func(source domain.SourceID, start int, elapsed float64, providerRef *string) {
		t.Helper()
		if err := segs.SaveSegmentEffort(ctx, domain.SegmentEffort{
			ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg.ID, ActivityID: activityID,
			ActivitySourceID: source, UserID: rider, StartOffset: start, EndOffset: start + 1,
			StartTime: act.StartTime.Add(time.Duration(start) * time.Second),
			ElapsedS:  elapsed, MovingS: elapsed, ProviderEffortExternalID: providerRef,
		}); err != nil {
			t.Fatal(err)
		}
	}
	providerEffort := "external-effort"
	add(primaryID, 5, 260, nil)
	add(primaryID, 10, 250, nil)
	add(secondary.ID, 5, 240, nil)
	add(secondary.ID, 10, 100, &providerEffort)

	efforts, err := segs.ListEffortsForActivity(ctx, activityID)
	if err != nil || len(efforts) != 2 || efforts[0].ActivitySourceID != primaryID || efforts[1].ActivitySourceID != primaryID {
		t.Fatalf("activity should show both local laps from the primary source: %+v, %v", efforts, err)
	}
	rows, err := segs.ListSegmentEffortsFiltered(ctx, seg.ID, rider, domain.SegmentEffortQuery{FastestFirst: true})
	if err != nil || len(rows) != 2 || rows[0].Effort.ElapsedS != 250 {
		t.Fatalf("fastest rankable effort should use the primary source: %+v, %v", rows, err)
	}

	if err := segs.DeleteEffortsForSource(ctx, primaryID); err != nil {
		t.Fatal(err)
	}
	efforts, err = segs.ListEffortsForSegment(ctx, seg.ID, rider, port.LeaderboardScopePersonalOnly, 10, 0)
	if err != nil || len(efforts) != 1 || efforts[0].ActivitySourceID != secondary.ID || efforts[0].ElapsedS != 240 {
		t.Fatalf("fallback should exclude the reported provider time: %+v, %v", efforts, err)
	}
}
