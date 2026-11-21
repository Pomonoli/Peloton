package domain

import "time"

// GarminActivity is a cycling activity returned by Garmin Connect. These
// measurements also let us import rides that have no original activity file.
type GarminActivity struct {
	ID, Title, Type, Description       string
	StartTime, LocalDay                time.Time
	Elapsed, Moving                    time.Duration
	DistanceM, ElevationM              *float64
	AvgHeartRate, AvgPower, AvgCadence *int32
}

type GarminSession struct {
	Key, Token string
	ExpiresAt  time.Time
}

// GarminSync holds the durable cursor and status for a user's Garmin account.
type GarminSync struct {
	UserID                       UserID
	Lease                        string
	LeaseUntil                   *time.Time
	Window, Watermark, CursorDay *time.Time
	NextRun, LastSync            *time.Time
	Offset                       int
	HistoryComplete              bool
	HistoryRides                 int
	SummaryOnly, Skipped         int
	Failures                     int
	Error                        string
	NeedsKey                     bool
}

// GarminSecretAAD binds encrypted session data to its owning user.
func GarminSecretAAD(user UserID) []byte {
	return []byte("garmin_connect:" + user.String())
}
