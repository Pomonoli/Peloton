package domain

import "time"

// ShareLink provides authenticated members with a shortcut to one activity via an
// unguessable token. A revoked link (RevokedAt != nil) no longer resolves.
type ShareLink struct {
	Token      string
	ActivityID ActivityID
	CreatedBy  UserID
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

// Active reports whether the link still grants access.
func (l ShareLink) Active() bool { return l.RevokedAt == nil }
