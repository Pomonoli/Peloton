package domain

import "time"

// CommunityMember is the directory's public member card and visible activity totals.
type CommunityMember struct {
	UserID           UserID
	Username         string
	DisplayName      string
	AvatarURL        string
	ActivityCount    int
	CyclingDistanceM float64
	LatestRide       *CommunityRide
}

type CommunityRide struct {
	ID        ActivityID
	Title     string
	StartTime time.Time
}
