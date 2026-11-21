package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	cairnv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	workerv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/worker/v1"
	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/garmin"
	"github.com/opencairn-org/cairn-core/internal/config"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/fileimport"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/protoconv"
	"github.com/opencairn-org/cairn-core/internal/usecase/garminsync"
)

const garminProvider = "garmin"

func wireGarmin(app *App, repo port.GarminSyncRepo, cfg config.SchedulerConfig, logger *slog.Logger) {
	var cadence time.Duration
	if cfg.Enabled {
		cadence = cfg.PollingInterval
	}
	app.Garmin = garminsync.New(repo, garmin.New(), &garminRideIngester{app: app, repo: repo, log: logger}, cadence, logger)
}

type garminRideIngester struct {
	app  *App
	repo port.GarminSyncRepo
	log  *slog.Logger
}

func (g *garminRideIngester) Existing(ctx context.Context, user domain.UserID, id string) (bool, bool, error) {
	source, err := g.app.Activities.FindOwnedSourceByExternalID(ctx, user, garminProvider, nil, id)
	if errors.Is(err, domain.ErrNotFound) {
		return false, false, g.checkRemoved(ctx, user, id)
	}
	return err == nil, source.Parsed.HasStream, err
}

func (g *garminRideIngester) IngestRide(ctx context.Context, sync domain.GarminSync, ride domain.GarminActivity, file port.UploadInput) (bool, error) {
	if err := actImports(g.app, g.log).Gate(ctx, sync.UserID); err != nil {
		return false, err
	}
	payload, stream, err := garminPayload(ride, file)
	if err != nil {
		return false, err
	}
	event := &workerv1.ImportedActivity{
		Ref:     &workerv1.ExternalRef{UserId: sync.UserID.String(), Provider: garminProvider, ExternalId: ride.ID},
		Payload: payload, Stream: stream,
	}
	if len(file.Raw) > 0 && g.app.BlobStore != nil {
		sum := sha256.Sum256(file.Raw)
		event.RawBlobId = fmt.Sprintf("users/%s/garmin/uploads/personal-%s-%x%s", sync.UserID, ride.ID, sum[:16], fileimport.Ext(file.Filename))
		event.RawContentType = fileimport.ContentType(fileimport.Ext(file.Filename))
		event.RawSizeBytes = int64(len(file.Raw))
		if err := g.app.BlobStore.Put(ctx, event.RawBlobId, file.Raw, event.RawContentType); err != nil {
			return false, fmt.Errorf("archive Garmin activity: %w", err)
		}
	}
	jr := &workerv1.JobResult{WorkerName: "garmin-connect", WorkerVersion: "1.0"}
	_, err = ingestActivityEventChecked(ctx, g.app, g.log, jr, event, func(ctx context.Context) error {
		if err := g.repo.Guard(ctx, sync.UserID, sync.Lease); err != nil {
			return err
		}
		return g.checkRemoved(ctx, sync.UserID, ride.ID)
	})
	return stream.GetSampleCount() > 0 && stream.GetSampleCount() <= protoconv.MaxStreamSamples, err
}

func (g *garminRideIngester) checkRemoved(ctx context.Context, user domain.UserID, id string) error {
	if g.app.SourceDenylist == nil {
		return nil
	}
	removed, err := g.app.SourceDenylist.IsDenied(ctx, user, garminProvider, nil, id)
	if removed && err == nil {
		return port.ErrGarminIgnored
	}
	return err
}

func garminPayload(ride domain.GarminActivity, file port.UploadInput) (*cairnv1.ActivitySourcePayload, *cairnv1.ActivityStream, error) {
	var payload *cairnv1.ActivitySourcePayload
	var stream *cairnv1.ActivityStream
	if len(file.Raw) != 0 {
		p, st, err := fileimport.Parse(file.Filename, file.Raw)
		if err == nil && p.GetElapsedDuration().AsDuration() > 0 {
			payload, stream = p, st
		}
	}
	if payload == nil {
		if ride.StartTime.IsZero() || !timestamppb.New(ride.StartTime).IsValid() || ride.Elapsed <= 0 {
			return nil, nil, port.ErrGarminInvalidRide
		}
		payload = &cairnv1.ActivitySourcePayload{
			StartTime: timestamppb.New(ride.StartTime), EndTime: timestamppb.New(ride.StartTime.Add(ride.Elapsed)),
			ElapsedDuration: durationpb.New(ride.Elapsed), MovingDuration: durationpb.New(min(ride.Moving, ride.Elapsed)), Timezone: "UTC",
			Summary: &cairnv1.ActivitySummary{
				DistanceM: ride.DistanceM, ElevationGainM: ride.ElevationM,
				AvgHeartRateBpm: ride.AvgHeartRate, AvgPowerW: ride.AvgPower, AvgCadence: ride.AvgCadence,
			},
		}
	} else if ride.Moving > 0 {
		payload.MovingDuration = durationpb.New(min(ride.Moving, payload.GetElapsedDuration().AsDuration()))
	}
	if payload.Summary.AvgCadence == nil {
		payload.Summary.AvgCadence = ride.AvgCadence
	}
	payload.Type = cairnv1.ActivityType_ACTIVITY_TYPE_RIDE
	payload.Discipline = garminDiscipline(ride.Type)
	payload.ProviderNativeType = ride.Type
	payload.IsVirtual = strings.Contains(ride.Type, "virtual") || strings.Contains(ride.Type, "indoor")
	payload.IsEbike = strings.Contains(ride.Type, "e_bike") || strings.Contains(ride.Type, "ebike")
	payload.Title = strings.TrimSpace(ride.Title)
	if payload.Title == "" {
		payload.Title = "Garmin ride"
	}
	payload.Description = ride.Description
	return payload, stream, nil
}

func garminDiscipline(native string) cairnv1.Discipline {
	switch {
	case strings.Contains(native, "mountain"):
		return cairnv1.Discipline_DISCIPLINE_RIDE_MTB
	case strings.Contains(native, "gravel"):
		return cairnv1.Discipline_DISCIPLINE_RIDE_GRAVEL
	case strings.Contains(native, "cyclocross"):
		return cairnv1.Discipline_DISCIPLINE_RIDE_CYCLOCROSS
	case strings.Contains(native, "track"):
		return cairnv1.Discipline_DISCIPLINE_RIDE_TRACK
	case strings.Contains(native, "bmx"):
		return cairnv1.Discipline_DISCIPLINE_RIDE_BMX
	default:
		return cairnv1.Discipline_DISCIPLINE_RIDE_ROAD
	}
}
