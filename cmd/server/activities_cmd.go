package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	s3adapter "github.com/opencairn-org/cairn-core/internal/adapter/secondary/s3"
	"github.com/opencairn-org/cairn-core/internal/config"
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
	return cmd
}
