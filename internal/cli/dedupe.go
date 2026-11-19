package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/khanakia/ai-logger/internal/importer"
)

// dedupeBackupLayout stamps the backup ailog dedupe writes first.
const dedupeBackupLayout = "20060102-150405"

func newDedupeCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Merge imported duplicates into the live-hook rows of the same turns",
		Long: `Before ailog matched imported prompts to the rows its live hooks had
already written, an import stored the same turn twice: once from the
hook, once from the transcript. dedupe finds those pairs (same tool and
session, same prompt text, captured within 10 minutes of the transcript
time, the hook row not yet linked) and merges each imported row into its
hook row: the hook row keeps every value it captured and gains the
import's keys, answer and usage where it has none; the imported row is
deleted.

--dry-run prints the counts and changes nothing. A real run first writes
a full copy of the database next to it (log.db.bak-<timestamp>).

  ailog dedupe --dry-run
  ailog dedupe`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			out := cmd.OutOrStdout()
			if !dryRun {
				backup := st.Path() + ".bak-" + time.Now().Format(dedupeBackupLayout)
				if err := st.BackupTo(ctx, backup); err != nil {
					return err
				}
				fmt.Fprintf(out, "backup: %s\n", backup)
			}
			stats, err := importer.Dedupe(ctx, st, dryRun)
			verb := "merged"
			if dryRun {
				verb = "would merge"
			}
			tools := make([]string, 0, len(stats.Merged))
			for t := range stats.Merged {
				tools = append(tools, t)
			}
			sort.Strings(tools)
			for _, t := range tools {
				fmt.Fprintf(out, "%s: %s %d imported duplicate(s) into hook rows\n", t, verb, stats.Merged[t])
			}
			fmt.Fprintf(out, "%d session(s) with a repeated prompt checked; %s %d row(s)\n", stats.Sessions, verb, stats.Total())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be merged; change nothing")
	return cmd
}
