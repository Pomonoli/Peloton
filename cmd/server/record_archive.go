package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/fileimport"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/protoconv"
)

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
	meta, err := blobs.Stat(ctx, source.RawBlobID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check archive for source %s: %w", source.ID, err)
	}
	if meta.SizeBytes <= 0 || meta.SizeBytes > port.MaxUploadBytes {
		return false, nil
	}
	raw, _, err := blobs.Get(ctx, source.RawBlobID)
	if err != nil {
		return false, fmt.Errorf("read archive for source %s: %w", source.ID, err)
	}
	if len(raw) > port.MaxUploadBytes {
		return false, nil
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
