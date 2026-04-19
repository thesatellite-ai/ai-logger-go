package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// newMigrateCmd registers `ailog migrate` — explicit schema migration.
//
// Note: migrations ALSO run silently on every `ailog` invocation via
// store.Open (additive-only — new columns get added, nothing gets
// dropped). This command is the user-facing wrapper for when you want
// to see what's happening, preview the DDL, or inspect the current
// schema on demand.
func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending schema migrations to the local DB",
		Long: `ailog auto-migrates on every invocation (additive only — new columns
with defaults are added, nothing is dropped). This command surfaces
that explicitly so you can see it happen, dry-run it, or inspect the
current schema.

Subcommands:
  ailog migrate           apply pending migrations now
  ailog migrate --dry-run print the DDL that would run, don't apply
  ailog migrate status    list current columns of the entries table`,
		RunE: runMigrateApply,
	}
	cmd.Flags().BoolP("dry-run", "n", false, "preview the DDL without applying it")
	cmd.AddCommand(newMigrateStatusCmd())
	return cmd
}

// runMigrateApply is the default action for `ailog migrate` — either
// dry-run (print DDL) or apply + report elapsed time.
func runMigrateApply(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		ddl, err := s.MigrateDiff(ctx)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), ddl)
		return nil
	}

	start := time.Now()
	if err := s.MigrateApply(ctx); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "migrations applied in %s\n", time.Since(start).Round(time.Microsecond))
	return nil
}

// newMigrateStatusCmd lists the columns the DB actually has right now
// — useful to verify a Tier 1-era column really lives there after the
// auto-migration has run.
func newMigrateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the current column list of the entries table",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			return s.SchemaInspect(ctx, cmd.OutOrStdout())
		},
	}
}
