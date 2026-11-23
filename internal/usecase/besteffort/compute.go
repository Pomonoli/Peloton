// Package besteffort holds the sliding-window best-effort computation
// use case. The output rows feed the per-activity power/pace curves on
// the activity-detail page and the per-user PR queries.
package besteffort

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ComputeBestEffortsForSource takes one ActivitySource, reads its raw
// stream, computes peak averages over the standard windows for each
// supported metric, and persists the result.
//
// Idempotent: a re-run (after a reimport, or after the user changed the
// activity's discipline) deletes the source's prior rows and writes the
// fresh set in a single transaction.
//
// Assumptions (v1):
//
// Cycling speed and pace use validated intervals. Duration records use
// elapsed time even when sampling intervals are irregular.
type ComputeBestEffortsForSource struct {
	activities  port.ActivityAggregateReader
	streams     port.StreamRepo
	bestEfforts port.BestEffortRepo
	tx          port.TxManager
	// Customs supplies the user's own windows (optional; nil → standard set only).
	Customs port.CustomBestEffortRepo

	now   func() time.Time
	newID func() uuid.UUID
}

func NewComputeBestEffortsForSource(
	activities port.ActivityAggregateReader,
	streams port.StreamRepo,
	bestEfforts port.BestEffortRepo,
	tx port.TxManager,
	now func() time.Time,
	newID func() uuid.UUID,
) *ComputeBestEffortsForSource {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if newID == nil {
		newID = func() uuid.UUID {
			id, err := uuid.NewV7()
			if err != nil {
				return uuid.New()
			}
			return id
		}
	}
	return &ComputeBestEffortsForSource{
		activities:  activities,
		streams:     streams,
		bestEfforts: bestEfforts,
		tx:          tx,
		now:         now,
		newID:       newID,
	}
}

// Input identifies the source to compute for.
type Input struct {
	ActivitySourceID domain.SourceID
}

// Result reports what the compute produced. It is non-fatal to return
// EffortsCount == 0 — short or stream-less activities legitimately have
// no efforts.
type Result struct {
	EffortsCount int
	NoStream     bool // source has no stored stream samples
	// NewRecords lists the all-time personal records this source now holds
	// that it did not hold before the compute (live "new PR" events).
	NewRecords []domain.PersonalRecord
}

// ---------------------------------------------------------------------------
// Standard windows
// ---------------------------------------------------------------------------

// windowSpec is one (metric, window-kind, window-value) combination the
// compute scans. The value is interpreted by Kind: seconds for duration
// windows, metres for distance windows.
type windowSpec struct {
	Metric domain.BestEffortMetric
	Kind   domain.BestEffortWindowKind
	Value  int
}

// StandardWindows is the v1 set. Duration windows are sample-count based
// (assumes ~1Hz); distance windows are cumulative-distance based via a
// two-pointer scan.
var StandardWindows = []windowSpec{
	// --- Duration windows ---
	// Cycling power — the canonical 5s/30s/1m/5m/20m/60m curve.
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 5},
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 30},
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 60},
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 300},
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 1200},
	{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 3600},
	// Heart rate — fewer windows since HR averages don't change as quickly.
	{domain.BestEffortMetricHeartRate, domain.BestEffortWindowDuration, 60},
	{domain.BestEffortMetricHeartRate, domain.BestEffortWindowDuration, 300},
	{domain.BestEffortMetricHeartRate, domain.BestEffortWindowDuration, 1200},
	{domain.BestEffortMetricHeartRate, domain.BestEffortWindowDuration, 3600},
	// Speed — relevant for cycling activities without a power meter.
	{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 60},
	{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 300},
	{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 1200},

	// VAM — cumulative ascent over the best window, stored in metres per hour.
	{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 300},
	{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 1200},
	{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 3600},

	// --- Distance windows (pace) ---
	// Shared PR distances for activities with cumulative distance.
	{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 1000},
	{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 5000},
	{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 10000},
}

var longCyclingWindows = []windowSpec{
	{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 20000},
	{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 50000},
}

func standardWindowsFor(activityType domain.ActivityType) []windowSpec {
	if activityType == domain.ActivityTypeRide {
		return slices.Concat(StandardWindows, longCyclingWindows)
	}
	return StandardWindows
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func (uc *ComputeBestEffortsForSource) Execute(ctx context.Context, in Input) (Result, error) {
	return uc.replaceEfforts(ctx, in.ActivitySourceID, true)
}

// Recalculate replaces historical efforts without emitting new-record events.
func (uc *ComputeBestEffortsForSource) Recalculate(ctx context.Context, id domain.SourceID) error {
	_, err := uc.replaceEfforts(ctx, id, false)
	return err
}

func (uc *ComputeBestEffortsForSource) replaceEfforts(ctx context.Context, id domain.SourceID, notify bool) (Result, error) {
	var res Result
	var act domain.Activity
	var before []domain.PersonalRecord
	err := uc.withSourceLock(ctx, id, func(ctx context.Context) error {
		src, err := uc.activities.GetSource(ctx, id)
		if errors.Is(err, domain.ErrNotFound) && !notify {
			return nil // a maintenance scan can race a source deletion
		}
		if err != nil {
			return fmt.Errorf("load source: %w", err)
		}
		act, err = uc.activities.GetActivity(ctx, src.ActivityID)
		if err != nil {
			return fmt.Errorf("load activity: %w", err)
		}
		if act.IsDeleted() || src.Status == domain.SourceStatusDetached {
			return nil
		}
		stream, err := uc.streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: id, Resolution: domain.StreamResolutionRaw})
		if err != nil {
			return fmt.Errorf("read stream: %w", err)
		}
		res.NoStream = len(stream.Samples) == 0
		var efforts []domain.BestEffort
		if !res.NoStream {
			windows, err := uc.windowsFor(ctx, act)
			if err != nil {
				return err
			}
			efforts = uc.computeAll(act, src, stream.Samples, windows)
			res.EffortsCount = len(efforts)
			if notify {
				// Only feeds live events. A lookup error inside this transaction
				// would also prevent the write, so propagate it for retry.
				before, err = uc.bestEfforts.ListPersonalRecords(ctx, act.UserID)
				if err != nil {
					return err
				}
			}
		}
		if err := uc.bestEfforts.DeleteForSource(ctx, id); err != nil {
			return err
		}
		return uc.bestEfforts.SaveEfforts(ctx, efforts)
	})
	if err != nil {
		return Result{}, fmt.Errorf("replace efforts for %s: %w", id, err)
	}
	if notify && res.EffortsCount > 0 {
		after, _ := uc.bestEfforts.ListPersonalRecords(ctx, act.UserID)
		res.NewRecords = newRecords(before, after, act.ID)
	}
	return res, nil
}

func (uc *ComputeBestEffortsForSource) withSourceLock(ctx context.Context, id domain.SourceID, run func(context.Context) error) error {
	return uc.tx.InTx(ctx, func(ctx context.Context) error {
		if locker, ok := uc.bestEfforts.(port.BestEffortSourceLocker); ok {
			if err := locker.LockBestEffortSource(ctx, id); err != nil {
				return fmt.Errorf("lock source: %w", err)
			}
		}
		return run(ctx)
	})
}

// newRecords returns the records of after held by activityID that before
// did not attribute to it.
func newRecords(before, after []domain.PersonalRecord, activityID domain.ActivityID) []domain.PersonalRecord {
	type key struct {
		t domain.ActivityType
		m domain.BestEffortMetric
		k domain.BestEffortWindowKind
		v int
	}
	prev := map[key]domain.ActivityID{}
	for _, r := range before {
		prev[key{r.ActivityType, r.Metric, r.WindowKind, r.WindowValue}] = r.ActivityID
	}
	var out []domain.PersonalRecord
	for _, r := range after {
		if r.ActivityID != activityID {
			continue
		}
		if prev[key{r.ActivityType, r.Metric, r.WindowKind, r.WindowValue}] != activityID {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Compute pipeline
// ---------------------------------------------------------------------------

// windowsFor returns the standard windows plus the user's custom ones for
// the activity's type.
func (uc *ComputeBestEffortsForSource) windowsFor(ctx context.Context, act domain.Activity) ([]windowSpec, error) {
	standard := standardWindowsFor(act.Type)
	if uc.Customs == nil {
		return standard, nil
	}
	customs, err := uc.Customs.ListForUser(ctx, act.UserID, act.Type)
	if err != nil {
		return nil, fmt.Errorf("load custom effort windows: %w", err)
	}
	return mergeWindows(standard, customs), nil
}

// mergeWindows appends custom windows that are not already standard.
func mergeWindows(std []windowSpec, customs []domain.CustomBestEffort) []windowSpec {
	out := append([]windowSpec(nil), std...)
	for _, c := range customs {
		if domain.RemovedRecordDistance(c.WindowKind, c.WindowValue) {
			continue
		}
		w := windowSpec{Metric: c.Metric, Kind: c.WindowKind, Value: c.WindowValue}
		dup := false
		for _, s := range std {
			if s == w {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, w)
		}
	}
	return out
}

func (uc *ComputeBestEffortsForSource) computeAll(
	act domain.Activity,
	src domain.ActivitySource,
	samples []domain.StreamSample,
	windows []windowSpec,
) []domain.BestEffort {
	now := uc.now()
	if act.Type == domain.ActivityTypeRide {
		return uc.computeCycling(act, src, samples, windows, now)
	}
	var out []domain.BestEffort
	hasDistance := slices.ContainsFunc(samples, func(s domain.StreamSample) bool { return s.DistanceM != nil && *s.DistanceM > 0 })
	hasSpeed := slices.ContainsFunc(samples, func(s domain.StreamSample) bool {
		_, ok := readMetric(s, domain.BestEffortMetricSpeed)
		return ok
	})
	var motion *domain.CyclingStream
	for _, w := range windows {
		var effort domain.BestEffort
		var ok bool
		switch {
		case w.Metric == domain.BestEffortMetricPace && !hasDistance,
			w.Metric == domain.BestEffortMetricSpeed && !hasSpeed:
			if motion == nil {
				motion = new(domain.ValidateCyclingStream(samples))
			}
			effort, ok = uc.motionFallback(act, src, samples, *motion, w, now)
		case w.Kind == domain.BestEffortWindowDuration:
			effort, ok = uc.timedSensorEffort(act, src, samples, w, now)
		case w.Kind == domain.BestEffortWindowDistance:
			effort, ok = uc.computeDistanceEffort(act, src, samples, w, now)
		}
		if ok {
			out = append(out, effort)
		}
	}
	return out
}

// Older outdoor streams can contain positions and timestamps without the
// derived speed or distance channel. The same validated intervals used for
// cycling supply a fallback without bridging gaps or changing the recording.
func (uc *ComputeBestEffortsForSource) motionFallback(
	act domain.Activity, src domain.ActivitySource, samples []domain.StreamSample,
	motion domain.CyclingStream, window windowSpec, now time.Time,
) (domain.BestEffort, bool) {
	var best domain.BestEffort
	motion.Sections(func(start, end int) {
		points := motionPoints(motion.Legs[start:end])
		effort, ok := uc.cyclingMotionEffort(act, src, samples[start].Timestamp, points, window, now, nil)
		if ok && betterCyclingEffort(effort, best) {
			effort.StartOffset = int(effort.Timestamp.Sub(samples[0].Timestamp).Seconds())
			best = effort
		}
	})
	return best, best.WindowValue > 0
}

// computeDistanceEffort handles the cumulative-distance two-pointer case.
// Only pace is supported in v1 (the only distance metric in StandardWindows);
// other metrics would need their own per-distance scan.
func (uc *ComputeBestEffortsForSource) computeDistanceEffort(
	act domain.Activity,
	src domain.ActivitySource,
	samples []domain.StreamSample,
	w windowSpec,
	now time.Time,
) (domain.BestEffort, bool) {
	if w.Metric != domain.BestEffortMetricPace {
		return domain.BestEffort{}, false
	}
	pace, startIdx, endIdx, dist, ok := slidingWindowMinPace(samples, float64(w.Value))
	if !ok {
		return domain.BestEffort{}, false
	}
	startTS := samples[startIdx].Timestamp
	endTS := samples[endIdx].Timestamp
	distance := dist // captured before pointer-take
	return domain.BestEffort{
		ID:               domain.BestEffortID(uc.newID()),
		ActivityID:       act.ID,
		ActivitySourceID: src.ID,
		UserID:           act.UserID,
		ActivityType:     act.Type,
		Discipline:       act.Discipline,
		Metric:           w.Metric,
		WindowKind:       domain.BestEffortWindowDistance,
		WindowValue:      w.Value,
		AchievedValue:    pace,
		StartOffset:      int(startTS.Sub(samples[0].Timestamp).Seconds()),
		DurationS:        endTS.Sub(startTS).Seconds(),
		DistanceM:        &distance,
		Timestamp:        startTS,
		CreatedAt:        now,
	}, true
}

func readMetric(s domain.StreamSample, metric domain.BestEffortMetric) (float64, bool) {
	switch metric {
	case domain.BestEffortMetricPower:
		if s.PowerW == nil || *s.PowerW < 0 || *s.PowerW > 3000 {
			return 0, false
		}
		return float64(*s.PowerW), true
	case domain.BestEffortMetricHeartRate:
		if s.HeartRateBpm == nil || *s.HeartRateBpm < 25 || *s.HeartRateBpm > 250 {
			return 0, false
		}
		return float64(*s.HeartRateBpm), true
	case domain.BestEffortMetricSpeed:
		if s.SpeedMps == nil || !(*s.SpeedMps >= 0 && *s.SpeedMps <= 50) {
			return 0, false
		}
		return *s.SpeedMps, true
	default:
		// pace uses the distance two-pointer scan; vam its own altitude-rate
		// scan. Neither is an instantaneous channel here.
		return 0, false
	}
}

// paceDistancePoint is one sample that carries cumulative distance.
type paceDistancePoint struct {
	idx  int
	dist float64
	ts   time.Time
}

// paceDistancePoints is the compact view of samples that carry distance.
func paceDistancePoints(samples []domain.StreamSample) []paceDistancePoint {
	pts := make([]paceDistancePoint, 0, len(samples))
	for i, s := range samples {
		if s.DistanceM == nil {
			continue
		}
		pts = append(pts, paceDistancePoint{idx: i, dist: *s.DistanceM, ts: s.Timestamp})
	}
	return pts
}

// paceAdvanceEnd advances end until the window from start covers windowDistanceM.
// end is monotone across outer steps — never reset.
func paceAdvanceEnd(pts []paceDistancePoint, start, end int, windowDistanceM float64) int {
	if end < start {
		end = start
	}
	for end < len(pts) && pts[end].dist-pts[start].dist < windowDistanceM {
		end++
	}
	return end
}

// slidingWindowMinPace finds the fastest cumulative-distance window of
// at least windowDistanceM metres. Returns the pace in seconds per km
// over the window, the original-array start/end indices, the actual
// distance covered (always >= windowDistanceM), and ok.
//
// Two-pointer scan in O(N):
//
//   - Build a compact view of (idx, dist, ts) over samples that carry
//     cumulative DistanceM. Samples without it are skipped.
//   - For each `start`, advance `end` until the inter-sample distance
//     covers the window. Compute pace; track the minimum.
//   - When `end` runs off the array, no further windows fit — break.
//
// "Fastest" means LOWEST seconds-per-km. The use case persists the
// resulting BestEffort with Metric = pace; SmallerIsBetter == true on
// the metric tells leaderboard sorters to ORDER BY achieved_value ASC.
func slidingWindowMinPace(samples []domain.StreamSample, windowDistanceM float64) (paceSecPerKm float64, startIdx, endIdx int, distM float64, ok bool) {
	if len(samples) < 2 || windowDistanceM <= 0 {
		return 0, 0, 0, 0, false
	}

	pts := paceDistancePoints(samples)
	if len(pts) < 2 {
		return 0, 0, 0, 0, false
	}
	if pts[len(pts)-1].dist-pts[0].dist < windowDistanceM {
		return 0, 0, 0, 0, false
	}

	var (
		bestPace  float64
		bestStart = pts[0].idx
		bestEnd   = pts[0].idx
		bestDist  float64
		found     bool
	)
	end := 0
	for start := 0; start < len(pts); start++ {
		end = paceAdvanceEnd(pts, start, end, windowDistanceM)
		if end >= len(pts) {
			break // no further windows can cover the distance
		}
		elapsed := pts[end].ts.Sub(pts[start].ts).Seconds()
		dist := pts[end].dist - pts[start].dist
		if elapsed <= 0 || dist <= 0 {
			continue
		}
		pace := elapsed / (dist / 1000.0)
		if !found || pace < bestPace {
			bestPace = pace
			bestStart = pts[start].idx
			bestEnd = pts[end].idx
			bestDist = dist
			found = true
		}
	}
	return bestPace, bestStart, bestEnd, bestDist, found
}
