package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	s3adapter "github.com/opencairn-org/cairn-core/internal/adapter/secondary/s3"
	"github.com/opencairn-org/cairn-core/internal/config"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func newActivitiesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "activities", Short: "Maintain imported activities"}
	cmd.AddCommand(&cobra.Command{
		Use:   "backfill-moving-time",
		Short: "Recalculate historical moving time and average speed",
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
				count, err := backfillMovingTime(ctx, pool, activityTimingArchive{blobs: blobs})
				cmd.Printf("Updated timing for %d activities\n", count)
				return err
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "repair-cycling-distance <activity-id>",
		Short: "Refresh one ride's distance and speed summary from stored data",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepairCyclingDistance,
	})
	return cmd
}

func runRepairCyclingDistance(cmd *cobra.Command, args []string) error {
	id, err := domain.ParseUUID[domain.ActivityID](args[0])
	if err != nil {
		return fmt.Errorf("invalid activity ID: %w", err)
	}
	return withPool(cmd.Context(), func(ctx context.Context, pool *pgxpool.Pool) error {
		changed, err := repairCyclingDistance(ctx, pool, id)
		if err != nil {
			return err
		}
		if changed {
			cmd.Printf("Updated cycling summary for %s\n", id)
		} else {
			cmd.Printf("Cycling summary already current for %s\n", id)
		}
		return nil
	})
}
