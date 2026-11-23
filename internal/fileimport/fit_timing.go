package fileimport

import (
	"math"
	"time"

	"github.com/tormoder/fit"
	"google.golang.org/protobuf/types/known/durationpb"

	cairnv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/protoconv"
)

func finishFIT(b *activityBuilder, file *fit.ActivityFile) (*cairnv1.ActivitySourcePayload, *cairnv1.ActivityStream, bool, error) {
	fileStart, fileEnd, sessionTimer := fitSessionTime(file)
	windows, timer := fitTimerEvents(file.Events)
	if fileStart.IsZero() && len(windows) > 0 {
		fileStart, fileEnd = windows[0].Start, windows[len(windows)-1].End
	}
	p, st, err := b.finishWithin(fileStart, fileEnd)
	if err != nil {
		return nil, nil, false, err
	}
	start, end, elapsed := p.GetStartTime().AsTime(), p.GetEndTime().AsTime(), p.GetElapsedDuration().AsDuration()
	if p.Summary.DistanceM == nil && len(file.Sessions) == 1 {
		distance := file.Sessions[0].GetTotalDistanceScaled()
		if distance >= 0 {
			p.Summary.DistanceM = &distance
		}
	}
	if len(windows) > 0 && (windows[0].Start.Before(start) || windows[len(windows)-1].End.After(end)) {
		windows, timer = nil, 0
	}
	if timer <= 0 {
		timer = sessionTimer
		if timer <= 0 && file.Activity != nil {
			timer = fitDuration(file.Activity.GetTotalTimerTimeScaled())
		}
	}
	if timer <= 0 || timer > elapsed {
		return p, st, false, nil
	}

	moving := timer
	if p.GetType() == cairnv1.ActivityType_ACTIVITY_TYPE_RIDE || p.GetType() == cairnv1.ActivityType_ACTIVITY_TYPE_WORKOUT {
		coverage := elapsed
		if len(windows) > 0 {
			coverage = timer
		}
		samples := protoconv.ActivityStreamSamplesFromProto(st, start)
		if estimate, ok := domain.EstimateGPSMovingTime(samples, coverage, windows); ok {
			moving = min(estimate, timer)
		}
	}
	p.MovingDuration = durationpb.New(moving)
	return p, st, true, nil
}

func fitSessionTime(file *fit.ActivityFile) (start, end time.Time, timer time.Duration) {
	validTimer := len(file.Sessions) > 0
	validBounds := validTimer
	for _, session := range file.Sessions {
		begin := session.StartTime
		d := fitDuration(session.GetTotalElapsedTimeScaled())
		active := fitDuration(session.GetTotalTimerTimeScaled())
		validTimer = validTimer && active > 0 && (d == 0 || active <= d)
		timer += active
		if !validFITTime(begin) || d <= 0 {
			validBounds = false
			continue
		}
		finish := begin.Add(d)
		// Some devices report whole-second session end timestamps.
		if validFITTime(session.Timestamp) && session.Timestamp.After(finish) && session.Timestamp.Sub(finish) <= time.Second {
			finish = session.Timestamp
		}
		if start.IsZero() || begin.Before(start) {
			start = begin
		}
		if finish.After(end) {
			end = finish
		}
	}
	if !validTimer {
		timer = 0
	}
	if !validBounds {
		start, end = time.Time{}, time.Time{}
	}
	return start, end, timer
}

func fitDuration(seconds float64) time.Duration {
	if math.IsNaN(seconds) || seconds <= 0 {
		return 0
	}
	return time.Duration(math.Round(seconds * float64(time.Second)))
}

func validFITTime(t time.Time) bool {
	return !t.IsZero() && !fit.IsBaseTime(t)
}

// fitTimerEvents uses complete start/stop pairs. An incomplete or inconsistent
// event log falls back to the device's recorded timer total.
func fitTimerEvents(events []*fit.EventMsg) ([]domain.RecordingInterval, time.Duration) {
	var windows []domain.RecordingInterval
	var previous, running time.Time
	var total time.Duration
	for _, event := range events {
		if event.Event != fit.EventTimer || !validFITTime(event.Timestamp) {
			continue
		}
		t := event.Timestamp
		if t.Before(previous) {
			return nil, 0
		}
		switch event.EventType {
		case fit.EventTypeStart:
			if !running.IsZero() && !running.Equal(t) {
				return nil, 0
			}
			running = t
		case fit.EventTypeStop, fit.EventTypeStopAll, fit.EventTypeStopDisable, fit.EventTypeStopDisableAll:
			if running.IsZero() {
				// Some files emit both Stop and StopAll at the same instant.
				if t.Equal(previous) {
					continue
				}
				return nil, 0
			}
			if !t.After(running) {
				return nil, 0
			}
			windows = append(windows, domain.RecordingInterval{Start: running, End: t})
			total += t.Sub(running)
			running = time.Time{}
		default:
			continue
		}
		previous = t
	}
	if !running.IsZero() {
		return nil, 0
	}
	return windows, total
}
