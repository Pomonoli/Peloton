package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/fileimport"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/protoconv"
)

const fileTimingParserVersion = "1.1"

type activityTimingArchive struct {
	blobs port.BlobStore
	log   *slog.Logger
}

func (a activityTimingArchive) FITMovingTime(ctx context.Context, src domain.ActivitySource) (time.Duration, bool, error) {
	if (src.SourceWorkerName == "file-upload" || src.SourceWorkerName == "garmin-connect") &&
		src.SourceWorkerVersion == fileTimingParserVersion {
		return 0, false, nil
	}
	raw, err := a.readTimingArchive(ctx, src, ".fit")
	if len(raw) == 0 || err != nil {
		return 0, false, err
	}
	payload, _, timer, err := fileimport.ParseWithTimer("activity.fit", raw)
	if err != nil {
		// The stream is still usable when the original file cannot be decoded.
		return 0, false, nil
	}
	return payload.GetMovingDuration().AsDuration(), timer, nil
}

func (a activityTimingArchive) GPXMovingTime(ctx context.Context, src domain.ActivitySource) (time.Duration, bool, error) {
	raw, err := a.readTimingArchive(ctx, src, ".gpx")
	if len(raw) == 0 || err != nil {
		return 0, false, err
	}
	payload, stream, err := fileimport.Parse("activity.gpx", raw)
	if err != nil {
		return 0, false, nil
	}
	samples := protoconv.ActivityStreamSamplesFromProto(stream, payload.GetStartTime().AsTime())
	moving, ok := domain.EstimateGPSMovingTime(samples, src.Parsed.ElapsedDuration, nil)
	return moving, ok, nil
}

func (a activityTimingArchive) readTimingArchive(ctx context.Context, src domain.ActivitySource, ext string) ([]byte, error) {
	if src.RawContentType != fileimport.ContentType(ext) && fileimport.Ext(src.RawBlobID) != ext {
		return nil, nil
	}
	raw, err := readActivityArchive(ctx, a.blobs, src)
	if err != nil && a.log != nil {
		a.log.Warn("could not recover archived activity timing", "source_id", src.ID, "error", err)
		return nil, nil
	}
	return raw, err
}

// Restore a missing stream from a file the normal import pipeline archived.
// The caller holds the source lock alongside the raw-stream existence check.
func restoreRecordStream(ctx context.Context, blobs port.BlobStore, streams port.StreamRepo, source domain.ActivitySource) (bool, error) {
	if blobs == nil || source.RawBlobID == "" || source.RawSizeBytes > port.MaxUploadBytes {
		return false, nil
	}
	ext := strings.ToLower(filepath.Ext(source.RawBlobID))
	if ext != ".fit" && ext != ".gpx" && ext != ".tcx" {
		switch source.RawContentType {
		case "application/vnd.ant.fit", "application/gpx+xml", "application/vnd.garmin.tcx+xml":
		default:
			return false, nil // provider archives can also be JSON and ZIP
		}
	}
	existing, err := streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: source.ID})
	if err != nil || len(existing.Samples) > 0 {
		return false, err
	}
	raw, err := readActivityArchive(ctx, blobs, source)
	if len(raw) == 0 || err != nil {
		return false, err
	}
	payload, stream, err := fileimport.Parse(source.RawBlobID, raw)
	if err != nil {
		return false, fmt.Errorf("parse archive for source %s: %w", source.ID, err)
	}
	samples := protoconv.ActivityStreamSamplesFromProto(stream, payload.GetStartTime().AsTime())
	if len(samples) == 0 {
		return false, nil
	}
	if err := streams.WriteStream(ctx, source.ID, samples); err != nil {
		return false, err
	}
	return true, nil
}

func readActivityArchive(ctx context.Context, blobs port.BlobStore, source domain.ActivitySource) ([]byte, error) {
	if blobs == nil || source.RawBlobID == "" || source.RawSizeBytes > port.MaxUploadBytes {
		return nil, nil
	}
	meta, err := blobs.Stat(ctx, source.RawBlobID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check archive for source %s: %w", source.ID, err)
	}
	if meta.SizeBytes <= 0 || meta.SizeBytes > port.MaxUploadBytes {
		return nil, nil
	}
	raw, _, err := blobs.Get(ctx, source.RawBlobID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read archive for source %s: %w", source.ID, err)
	}
	if len(raw) > port.MaxUploadBytes {
		return nil, nil
	}
	return raw, nil
}
