package view

import (
	"context"
	"math"
	"slices"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
)

type HeadToHead struct {
	Members     *Members
	Segments    port.SegmentComparisonReader
	Records     port.RecordComparisonReader
	SharedRides port.SharedRideReader
	Visibility  *Visibility
}

type ComparisonWinner string

const (
	ComparisonYou      ComparisonWinner = "you"
	ComparisonOpponent ComparisonWinner = "opponent"
	ComparisonTie      ComparisonWinner = "tie"
)

type ComparisonScore struct {
	Wins, Losses, Ties int
}

func (s *ComparisonScore) add(w ComparisonWinner) {
	switch w {
	case ComparisonYou:
		s.Wins++
	case ComparisonOpponent:
		s.Losses++
	case ComparisonTie:
		s.Ties++
	}
}

type ComparedSegment struct {
	domain.SharedSegment
	Winner             ComparisonWinner
	YouSpeedKmh        float64
	OpponentSpeedKmh   float64
	TimeDifferenceS    float64
	SpeedDifferenceKmh float64
}

type ComparedRecord struct {
	Bucket            domain.RecordBucket
	You, Opponent     *domain.ComparisonEffort
	Winner            ComparisonWinner
	Difference        float64 // seconds for distance records, otherwise the metric's storage unit
	DifferencePercent float64 // relative to the better result
}

type HeadToHeadResult struct {
	First, Opponent           domain.User
	Segments                  []ComparedSegment
	Records                   []ComparedRecord
	SegmentScore, RecordScore ComparisonScore
}

func (h *HeadToHead) Compare(ctx context.Context, viewer domain.UserID, first, second string) (HeadToHeadResult, error) {
	var out HeadToHeadResult
	riders, err := h.riders(ctx, viewer, first, second)
	if err != nil {
		return out, err
	}
	user, opponent := riders[0], riders[1]
	segments, err := h.Segments.ListSharedSegments(ctx, viewer, user.ID, opponent.ID)
	if err != nil {
		return out, err
	}
	records, err := h.Records.ListComparisonRecords(ctx, viewer, user.ID, opponent.ID)
	if err != nil {
		return out, err
	}
	out.First, out.Opponent = user, opponent
	out.Segments = make([]ComparedSegment, 0, len(segments))
	for _, segment := range segments {
		row := compareSegment(segment)
		out.Segments = append(out.Segments, row)
		out.SegmentScore.add(row.Winner)
	}
	out.Records, out.RecordScore = compareRecords(user.ID, records)
	return out, nil
}

func (h *HeadToHead) riders(ctx context.Context, viewer domain.UserID, first, second string) ([2]domain.User, error) {
	var out [2]domain.User
	caller, err := h.Members.Get(ctx, viewer, viewer.String())
	if err != nil {
		return out, err
	}
	if !caller.IsActive() {
		return out, ErrForbidden
	}

	riders := [2]domain.User{caller}
	for i, key := range []string{first, second} {
		if i == 0 && key == "" {
			continue
		}
		rider, err := h.Members.Get(ctx, viewer, key)
		if err != nil {
			return out, err
		}
		if !rider.IsActive() {
			return out, domain.ErrNotFound
		}
		riders[i] = rider
	}
	if riders[0].ID == riders[1].ID {
		return out, port.NewActionError(port.ActionErrorInvalid, "Choose two distinct riders")
	}
	return riders, nil
}

func compareSegment(s domain.SharedSegment) ComparedSegment {
	youSpeed := s.Distance * 3.6 / s.You.Value
	opponentSpeed := s.Distance * 3.6 / s.Opponent.Value
	return ComparedSegment{
		SharedSegment:      s,
		Winner:             comparisonWinner(s.You.Value, s.Opponent.Value, true),
		YouSpeedKmh:        youSpeed,
		OpponentSpeedKmh:   opponentSpeed,
		TimeDifferenceS:    math.Abs(s.You.Value - s.Opponent.Value),
		SpeedDifferenceKmh: math.Abs(youSpeed - opponentSpeed),
	}
}

func compareRecords(viewer domain.UserID, records []domain.MemberRecord) ([]ComparedRecord, ComparisonScore) {
	buckets := make(map[domain.RecordBucket]ComparedRecord)
	standard := besteffort.StandardRecordBuckets(domain.ActivityTypeRide)
	for _, bucket := range standard {
		buckets[bucket] = ComparedRecord{Bucket: bucket}
	}
	for _, record := range records {
		if domain.RemovedRecordDistance(record.Bucket.WindowKind, record.Bucket.WindowValue) {
			continue
		}
		row := buckets[record.Bucket]
		row.Bucket = record.Bucket
		if record.UserID == viewer {
			row.You = &record.Effort
		} else {
			row.Opponent = &record.Effort
		}
		buckets[record.Bucket] = row
	}

	rows := make([]ComparedRecord, 0, len(buckets))
	var score ComparisonScore
	for bucket, row := range buckets {
		// Custom windows compare only if both riders have the same category.
		if !slices.Contains(standard, bucket) && (row.You == nil || row.Opponent == nil) {
			continue
		}
		if row.You != nil && row.Opponent != nil && bucket.Metric != domain.BestEffortMetricHeartRate {
			a, b := row.You.Value, row.Opponent.Value
			row.Winner = comparisonWinner(a, b, bucket.Metric.SmallerIsBetter())
			row.Difference = math.Abs(a - b)
			best := max(a, b)
			if bucket.Metric.SmallerIsBetter() {
				best = min(a, b)
				row.Difference *= float64(bucket.WindowValue) / 1000
			}
			row.DifferencePercent = math.Abs(a-b) / best * 100
			score.add(row.Winner)
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ComparedRecord) int {
		return besteffort.CompareRecordBuckets(a.Bucket, b.Bucket)
	})
	return rows, score
}

func comparisonWinner(you, opponent float64, smallerIsBetter bool) ComparisonWinner {
	if you == opponent {
		return ComparisonTie
	}
	if (you < opponent) == smallerIsBetter {
		return ComparisonYou
	}
	return ComparisonOpponent
}
