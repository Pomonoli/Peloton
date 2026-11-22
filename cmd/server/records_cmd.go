package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/postgres"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
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
	}, &cobra.Command{
		Use:   "recompute-cycling",
		Short: "Rebuild cycling best efforts and speed statistics for all users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withPool(cmd.Context(), func(ctx context.Context, pool *pgxpool.Pool) error {
				count, err := recomputeCyclingRecords(ctx, pool)
				cmd.Printf("Recomputed %d cycling activities\n", count)
				return err
			})
		},
	})
	return cmd
}

func recomputeCyclingRecords(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	activities := postgres.NewActivityRepo(pool)
	streams := postgres.NewStreamRepo(pool)
	store := postgres.NewMaintenanceRepo(pool)
	gear := postgres.NewGearRepo(pool)
	tx := postgres.NewTxManager(pool)
	records := postgres.NewBestEffortRepo(pool)
	compute := besteffort.NewComputeBestEffortsForSource(activities, streams, records, tx, nil, nil)
	compute.Customs = postgres.NewCustomBestEffortRepo(pool)

	const pageSize = 100
	var after domain.ActivityID
	var failures []error
	count := 0
	for {
		ids, err := store.ListCyclingActivityIDs(ctx, after, pageSize)
		if err != nil {
			return count, errors.Join(append(failures, err)...)
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return count, errors.Join(append(failures, ctx.Err())...)
			}
			err := tx.InTx(ctx, func(ctx context.Context) error {
				act, err := activities.GetActivity(ctx, id)
				if errors.Is(err, domain.ErrNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				// Use the same lock order as ingest and reclustering.
				if err := tx.LockUser(ctx, act.UserID); err != nil {
					return err
				}
				sources, err := activities.ListSourcesForActivity(ctx, id)
				if err != nil {
					return err
				}
				for _, src := range sources {
					if err := compute.Recalculate(ctx, src.ID); err != nil {
						return err
					}
				}
				return activity.RefreshCyclingSummary(ctx, activities, streams, activities, id)
			})
			if err != nil {
				failures = append(failures, fmt.Errorf("recompute cycling activity %s: %w", id, err))
			} else {
				count++
			}
		}
		if len(ids) < pageSize {
			if err := records.DeleteDetachedCyclingEfforts(ctx); err != nil {
				failures = append(failures, err)
			}
			owners, err := gear.ListUserIDsWithGear(ctx)
			if err != nil {
				return count, errors.Join(append(failures, err)...)
			}
			for _, owner := range owners {
				if err := gear.RecomputeTotalsForUser(ctx, owner); err != nil {
					failures = append(failures, fmt.Errorf("rebuild gear totals for %s: %w", owner, err))
				}
			}
			return count, errors.Join(failures...)
		}
		after = ids[len(ids)-1]
	}
}
