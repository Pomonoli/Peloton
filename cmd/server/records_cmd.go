package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/postgres"
	s3adapter "github.com/opencairn-org/cairn-core/internal/adapter/secondary/s3"
	"github.com/opencairn-org/cairn-core/internal/config"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
	"github.com/opencairn-org/cairn-core/internal/usecase/segment"
	"github.com/opencairn-org/cairn-core/internal/usecase/segmentrank"
)

func newRecordsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "records", Short: "Maintain personal records"}
	cmd.AddCommand(&cobra.Command{
		Use:   "backfill-cycling-distances",
		Short: "Compute missing 20 km and 50 km records from historical cycling recordings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withPool(cmd.Context(), func(ctx context.Context, pool *pgxpool.Pool) error {
				records := postgres.NewBestEffortRepo(pool)
				compute := besteffort.NewComputeBestEffortsForSource(
					postgres.NewActivityRepo(pool), postgres.NewStreamRepo(pool), records, postgres.NewTxManager(pool), nil, nil,
				)
				count, err := compute.BackfillCyclingDistances(ctx, records)
				cmd.Printf("Processed %d cycling recordings\n", count)
				return err
			})
		},
	})
	for _, mode := range []struct {
		name, help  string
		cyclingOnly bool
	}{
		{"recompute", "Recover archived streams and rebuild historical records and cycling segments for all riders", false},
		{"recompute-cycling", "Rebuild cycling best efforts, speed statistics and segment efforts for all riders", true},
	} {
		cmd.AddCommand(&cobra.Command{
			Use:   mode.name,
			Short: mode.help,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				var blobs port.BlobStore
				if !cfg.Storage.Disabled() && cfg.Storage.Configured() {
					blobs, err = s3adapter.New(cfg.Storage)
					if err != nil {
						return err
					}
				}
				return withPool(cmd.Context(), func(ctx context.Context, pool *pgxpool.Pool) error {
					result, err := recomputeHistoricalRecords(ctx, pool, blobs, mode.cyclingOnly)
					cmd.Printf("Recomputed %d activities, recovered %d archived streams, and rematched %d cycling recordings\n",
						result.activities, result.restored, result.rematched)
					return err
				})
			},
		})
	}
	return cmd
}

type recordRebuildResult struct {
	activities, restored, rematched int
}

func recomputeHistoricalRecords(ctx context.Context, pool *pgxpool.Pool, blobs port.BlobStore, cyclingOnly bool) (recordRebuildResult, error) {
	activities := postgres.NewActivityRepo(pool)
	streams := postgres.NewStreamRepo(pool)
	store := postgres.NewMaintenanceRepo(pool)
	overrides := postgres.NewClassificationOverrideRepo(pool)
	gear := postgres.NewGearRepo(pool)
	tx := postgres.NewTxManager(pool)
	records := postgres.NewBestEffortRepo(pool)
	segments := postgres.NewSegmentRepo(pool)
	compute := besteffort.NewComputeBestEffortsForSource(activities, streams, records, tx, nil, nil)
	compute.Customs = postgres.NewCustomBestEffortRepo(pool)
	matcher := segment.NewMatchSegmentsForActivity(activities, streams, segments, tx,
		segmentrank.NewComputeSegmentRanks(segments, tx), nil, nil, domain.MatchTolerances{}, nil)

	const pageSize = 100
	list := store.ListCyclingActivityIDs
	if !cyclingOnly {
		list = func(ctx context.Context, after domain.ActivityID, limit int) ([]domain.ActivityID, error) {
			return store.ListActivityIDs(ctx, nil, after, limit)
		}
	}
	var after domain.ActivityID
	var failures []error
	var result recordRebuildResult
	for {
		ids, err := list(ctx, after, pageSize)
		if err != nil {
			return result, errors.Join(append(failures, err)...)
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return result, errors.Join(append(failures, ctx.Err())...)
			}
			var recovered, rematch []domain.SourceID
			err := tx.InTx(ctx, func(ctx context.Context) error {
				act, err := activities.GetActivity(ctx, id)
				if errors.Is(err, domain.ErrNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				if err := tx.LockUser(ctx, act.UserID); err != nil {
					return err
				}
				act, err = activities.GetActivity(ctx, id)
				if errors.Is(err, domain.ErrNotFound) || (err == nil && act.IsDeleted()) {
					return nil
				}
				if err != nil {
					return err
				}
				sources, err := activities.ListSourcesForActivity(ctx, id)
				if err != nil {
					return err
				}
				for _, src := range sources {
					if src.Status == domain.SourceStatusDetached {
						continue
					}
					if blobs != nil && src.RawBlobID != "" {
						if err := records.LockBestEffortSource(ctx, src.ID); err != nil {
							return err
						}
						restored, err := restoreRecordStream(ctx, blobs, streams, src)
						if err != nil {
							failures = append(failures, err)
							continue
						}
						if restored {
							recovered = append(recovered, src.ID)
							if !src.Parsed.HasStream {
								src.Parsed.HasStream = true
								if err := activities.SaveSource(ctx, src); err != nil {
									return err
								}
							}
							if act.PrimaryStreamSourceID == nil {
								act.PrimaryStreamSourceID = &src.ID
								if err := activities.SaveActivity(ctx, act); err != nil {
									return err
								}
							}
						}
					}
					if err := compute.Recalculate(ctx, src.ID); err != nil {
						return err
					}
					if act.Type == domain.ActivityTypeRide {
						_, _, gps, err := streams.FirstGeoPoint(ctx, src.ID)
						if err != nil {
							return err
						}
						if gps {
							rematch = append(rematch, src.ID)
						}
					}
				}
				return activity.RefreshCyclingSummary(ctx, activities, streams, activities, overrides, id)
			})
			if err != nil {
				failures = append(failures, fmt.Errorf("recompute activity %s: %w", id, err))
				continue
			}
			result.activities++
			for _, source := range recovered {
				result.restored++
				if err := streams.RefreshAggregates(ctx, source); err != nil {
					failures = append(failures, err)
				}
			}
			for _, source := range rematch {
				if _, err := matcher.Execute(ctx, segment.Input{ActivitySourceID: source}); err != nil {
					failures = append(failures, err)
					continue
				}
				result.rematched++
			}
		}
		if len(ids) < pageSize {
			if err := records.DeleteDetachedCyclingEfforts(ctx); err != nil {
				failures = append(failures, err)
			}
			owners, err := gear.ListUserIDsWithGear(ctx)
			if err != nil {
				return result, errors.Join(append(failures, err)...)
			}
			for _, owner := range owners {
				if err := gear.RecomputeTotalsForUser(ctx, owner); err != nil {
					failures = append(failures, fmt.Errorf("rebuild gear totals for %s: %w", owner, err))
				}
			}
			return result, errors.Join(failures...)
		}
		after = ids[len(ids)-1]
	}
}
