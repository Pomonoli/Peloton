package domain

// ComparisonEffort is a rider's best eligible result. Value uses the metric's
// storage unit, or elapsed seconds for a segment.
type ComparisonEffort struct {
	ActivityID ActivityID
	Value      float64
}

type SharedSegment struct {
	ID       SegmentID
	Name     string
	Distance float64
	You      ComparisonEffort
	Opponent ComparisonEffort
}

type MemberRecord struct {
	UserID UserID
	Bucket RecordBucket
	Effort ComparisonEffort
}
