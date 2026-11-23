package fileimport

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/tormoder/fit"
)

func TestParseFITTimer(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		gps, events, incomplete bool
		want                    time.Duration
	}{
		{name: "timer events without GPS", events: true, want: 80 * time.Second},
		{name: "session timer without events", want: 90 * time.Second},
		{name: "incomplete events use session timer", events: true, incomplete: true, want: 90 * time.Second},
		{name: "GPS removes stops while the timer runs", events: true, gps: true, want: 60 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
			f, err := fit.NewFile(fit.FileTypeActivity, fit.NewHeader(fit.V20, true))
			if err != nil {
				t.Fatal(err)
			}
			f.FileId.Type, f.FileId.TimeCreated = fit.FileTypeActivity, start
			file, err := f.Activity()
			if err != nil {
				t.Fatal(err)
			}
			session := fit.NewSessionMsg()
			session.Sport, session.StartTime, session.Timestamp = fit.SportCycling, start, start.Add(100*time.Second)
			session.TotalElapsedTime, session.TotalTimerTime = 100000, 90000
			file.Sessions = append(file.Sessions, session)
			distance := 0.0
			for i := 0; i <= 100; i++ {
				if i > 0 && (i <= 20 || i > 40) && (i <= 60 || i > 80) {
					distance += 7
				}
				record := fit.NewRecordMsg()
				record.Timestamp = start.Add(time.Duration(i) * time.Second)
				if tc.gps {
					record.PositionLat = fit.NewLatitudeDegrees(50.8 + distance/111195)
					record.PositionLong = fit.NewLongitudeDegrees(4.4)
				}
				file.Records = append(file.Records, record)
			}
			if tc.events {
				for i, seconds := range []int{0, 20, 40, 100} {
					if tc.incomplete && i == 3 {
						break
					}
					event := fit.NewEventMsg()
					event.Event = fit.EventTimer
					event.EventType = []fit.EventType{fit.EventTypeStart, fit.EventTypeStop, fit.EventTypeStart, fit.EventTypeStopAll}[i]
					event.Timestamp = start.Add(time.Duration(seconds) * time.Second)
					file.Events = append(file.Events, event)
				}
			}
			var raw bytes.Buffer
			if err := fit.Encode(&raw, f, binary.LittleEndian); err != nil {
				t.Fatal(err)
			}
			p, st, timer, err := ParseWithTimer("ride.fit", raw.Bytes())
			if err != nil || !timer {
				t.Fatalf("FIT timer present = %t, error %v", timer, err)
			}
			if elapsed := p.GetElapsedDuration().AsDuration(); elapsed != 100*time.Second || st.GetSampleCount() != 101 {
				t.Fatalf("elapsed %s, samples %d", elapsed, st.GetSampleCount())
			}
			margin := time.Duration(0)
			if tc.gps {
				margin = 3 * time.Second
			}
			if got := p.GetMovingDuration().AsDuration(); got < tc.want-margin || got > tc.want+margin {
				t.Fatalf("moving time %s, want %s ± %s", got, tc.want, margin)
			}
		})
	}
}
