package view

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

var vNow = time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC) // a Wednesday

type fakeUsers struct {
	port.UserReader
	users map[domain.UserID]domain.User
}

func (f *fakeUsers) GetUser(_ context.Context, id domain.UserID) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func addActivity(r *inmem.ActivityRepo, user domain.UserID, t domain.ActivityType, start time.Time, privacy domain.ActivityPrivacy) domain.ActivityID {
	id := domain.ActivityID(uuid.New())
	d := 1000.0
	r.Activities[id] = domain.Activity{ID: id, UserID: user, Type: t, Discipline: domain.DisciplineRideRoad, Title: "a", StartTime: start,
		MovingDuration: time.Hour, ElapsedDuration: time.Hour, Summary: domain.ActivitySummary{DistanceM: &d}, Privacy: privacy}
	return id
}
