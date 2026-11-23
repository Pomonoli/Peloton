package domain

// ContinuousRecordingLeg checks the shared sample boundaries for a channel.
func ContinuousRecordingLeg(a, b StreamSample, maxGapSeconds float64) bool {
	dt := b.Timestamp.Sub(a.Timestamp).Seconds()
	return !a.Timestamp.IsZero() && !b.Timestamp.IsZero() && dt > 0 && dt <= maxGapSeconds &&
		(a.RecordingIndex == 0 || b.RecordingIndex == a.RecordingIndex+1)
}

// ValidRecordingAltitude checks the range used for recorded elevation efforts.
func ValidRecordingAltitude(s StreamSample) bool {
	return s.AltitudeM != nil && *s.AltitudeM >= -450 && *s.AltitudeM <= 9000
}
