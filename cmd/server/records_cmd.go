package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/postgres"
	"github.com/opencairn-org/cairn-core/internal/config"
	"github.com/opencairn-org/cairn-core/internal/db"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
)

func newRecordsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "records", Short: "Maintain personal records"}
	cmd.AddCommand(&cobra.Command{
		Use:   "backfill-cycling-distances",
		Short: "Compute missing 20 km and 50 km records from historical cycling recordings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			pool, err := db.Open(ctx, db.Config{
				URL:                      cfg.Database.URL,
				MaxConns:                 cfg.Database.MaxConns,
				MinConns:                 cfg.Database.MinConns,
				StatementTimeout:         cfg.Database.StatementTimeout,
				LockTimeout:              cfg.Database.LockTimeout,
				IdleInTransactionTimeout: cfg.Database.IdleInTransactionTimeout,
				ApplicationName:          "cairn-record-backfill",
			})
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer pool.Close()
			records := postgres.NewBestEffortRepo(pool)
			compute := besteffort.NewComputeBestEffortsForSource(
				postgres.NewActivityRepo(pool), postgres.NewStreamRepo(pool), records, postgres.NewTxManager(pool), nil, nil,
			)
			count, err := compute.BackfillCyclingDistances(ctx, records)
			cmd.Printf("Processed %d cycling recordings\n", count)
			return err
		},
	})
	return cmd
}
