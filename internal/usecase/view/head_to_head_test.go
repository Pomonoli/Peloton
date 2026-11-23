package view

import (
	"math"
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
)

func TestCompareSegmentElapsedSpeedAndScore(t *testing.T) {
	var score ComparisonScore
	for _, tc := range []struct {
		you, opponent float64
		winner        ComparisonWinner
	}{
		{100, 120, ComparisonYou},
		{120, 100, ComparisonOpponent},
		{100, 100, ComparisonTie},
	} {
		row := compareSegment(domain.SharedSegment{
			Distance: 1200,
			You:      domain.ComparisonEffort{Value: tc.you},
			Opponent: domain.ComparisonEffort{Value: tc.opponent},
		})
		expectedSpeedGap := 0.0
		if tc.you != tc.opponent {
			expectedSpeedGap = 7.2 // 43.2 km/h versus 36.0 km/h
		}
		if row.Winner != tc.winner || row.TimeDifferenceS != math.Abs(tc.you-tc.opponent) ||
			math.Abs(row.SpeedDifferenceKmh-expectedSpeedGap) > 1e-9 {
			t.Fatalf("unexpected comparison: %+v", row)
		}
		score.add(row.Winner)
	}
	if score != (ComparisonScore{Wins: 1, Losses: 1, Ties: 1}) {
		t.Fatalf("score = %+v", score)
	}
}

func TestCompareEveryStandardCyclingRecord(t *testing.T) {
	you, them := domain.UserID{1}, domain.UserID{2}
	expectedWindows := map[domain.BestEffortMetric]int{
		domain.BestEffortMetricPace:  5,
		domain.BestEffortMetricSpeed: 3, domain.BestEffortMetricPower: 6,
		domain.BestEffortMetricVAM: 3, domain.BestEffortMetricHeartRate: 4,
	}
	var records []domain.MemberRecord
	for _, bucket := range besteffort.StandardRecordBuckets(domain.ActivityTypeRide) {
		if !bucket.Valid() {
			t.Fatalf("invalid standard bucket: %+v", bucket)
		}
		expectedWindows[bucket.Metric]--
		records = append(records,
			domain.MemberRecord{UserID: you, Bucket: bucket, Effort: domain.ComparisonEffort{Value: 120}},
			domain.MemberRecord{UserID: them, Bucket: bucket, Effort: domain.ComparisonEffort{Value: 150}})
	}
	for metric, remainder := range expectedWindows {
		if remainder != 0 {
			t.Fatalf("%s window count differs by %d", metric, remainder)
		}
	}
	rows, score := compareRecords(you, records)
	if len(rows) != 21 || score != (ComparisonScore{Wins: 5, Losses: 12}) {
		t.Fatalf("rows %d, score %+v", len(rows), score)
	}
	for _, row := range rows {
		switch row.Bucket.Metric {
		case domain.BestEffortMetricHeartRate:
			if row.Winner != "" || row.You == nil || row.Opponent == nil {
				t.Fatalf("heart rate should be informational: %+v", row)
			}
		case domain.BestEffortMetricPace:
			want := 30 * float64(row.Bucket.WindowValue) / 1000
			if row.Winner != ComparisonYou || math.Abs(row.Difference-want) > 1e-9 || row.DifferencePercent != 25 {
				t.Fatalf("pace record mismatch: %+v", row)
			}
		default:
			if row.Winner != ComparisonOpponent || row.Difference != 30 || row.DifferencePercent != 20 {
				t.Fatalf("higher-is-better record mismatch: %+v", row)
			}
		}
	}
}

func TestCompareRecordsMissingAndExactMatching(t *testing.T) {
	you, them := domain.UserID{1}, domain.UserID{2}
	standard := domain.RecordBucket{ActivityType: domain.ActivityTypeRide, Metric: domain.BestEffortMetricPower,
		WindowKind: domain.BestEffortWindowDuration, WindowValue: 300}
	oneSided := standard
	oneSided.WindowValue = 301
	custom := standard
	custom.WindowValue = 400
	effort := domain.ComparisonEffort{Value: 280}
	rows, score := compareRecords(you, []domain.MemberRecord{
		{UserID: you, Bucket: standard, Effort: effort},
		{UserID: them, Bucket: oneSided, Effort: effort},
		{UserID: you, Bucket: custom, Effort: effort},
		{UserID: them, Bucket: custom, Effort: effort},
	})
	if score != (ComparisonScore{Ties: 1}) {
		t.Fatalf("score = %+v", score)
	}
	for _, row := range rows {
		switch row.Bucket {
		case oneSided:
			t.Fatal("unmatched custom category appeared")
		case standard:
			if row.You == nil || row.Opponent != nil || row.Winner != "" {
				t.Fatalf("missing opponent was scored: %+v", row)
			}
		case custom:
			if row.Winner != ComparisonTie || row.Difference != 0 {
				t.Fatalf("matched custom category was not tied: %+v", row)
			}
		}
	}
}
