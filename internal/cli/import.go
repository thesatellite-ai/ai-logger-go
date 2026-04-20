// Package cli — `ailog import` is a tool-agnostic backfill driver.
// Each upstream tool is a subcommand backed by an importer.Source
// registered in internal/importer; this file is the cobra wiring +
// flag plumbing.
//
//	ailog import claude-code [--from PATH]
//	ailog import codex      [--from PATH]
//	ailog import opencode   [--from PATH]
//	ailog import all        [--force] [--since RFC3339] [--limit N]
//
// Idempotency comes from the store's import_lines table (per-line SHA
// dedup) and import_state table (per-file mtime watermark). Re-running
// the same command is cheap and safe; pass --force to bypass the
// per-file fast path when re-parsing is intentional.
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/importer"
	"github.com/spf13/cobra"
)

// importFlags collects every flag the per-tool subcommands share.
// Bound once and reused across each registered source's command.
type importFlags struct {
	From   string
	Since  string
	Limit  int
	Force  bool
	Strict bool
}

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Backfill historical entries from a tool's transcript files",
		Long: "Each subcommand pulls entries from a different tool's native " +
			"transcript format. Re-runs are idempotent (per-line dedup) and fast " +
			"(per-file mtime watermarks). Use `import all` to run every source.",
	}

	for _, src := range importer.All() {
		cmd.AddCommand(newImportSourceCmd(src))
	}
	cmd.AddCommand(newImportAllCmd())
	return cmd
}

// newImportSourceCmd builds one `ailog import <name>` subcommand bound
// to a specific Source. The flag set is identical across sources so
// muscle memory transfers.
func newImportSourceCmd(src importer.Source) *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   src.Name(),
		Short: fmt.Sprintf("Import %s transcripts (default root: %s)", src.Name(), src.DefaultRoot()),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			opts, err := buildImportOptions(f, cmd)
			if err != nil {
				return err
			}

			stats, err := importer.Run(ctx, st, src, opts)
			printImportStats(cmd, src.Name(), stats)
			return err
		},
	}
	bindImportFlags(cmd, &f, src)
	return cmd
}

// newImportAllCmd runs every registered source in turn, sharing one
// store handle. Stops on the first error so partial-state failures
// surface loudly.
func newImportAllCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Run every registered import source against its default root",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			for _, src := range importer.All() {
				opts, err := buildImportOptions(f, cmd)
				if err != nil {
					return err
				}
				// `all` ignores --from on purpose — each source's
				// DefaultRoot() is correct, and one path can't be right
				// for every tool.
				opts.Root = ""
				fmt.Fprintf(cmd.OutOrStdout(), "→ %s\n", src.Name())
				stats, err := importer.Run(ctx, st, src, opts)
				printImportStats(cmd, src.Name(), stats)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
	bindImportFlags(cmd, &f, nil)
	return cmd
}

// bindImportFlags attaches the shared flag set to a subcommand. When
// src is non-nil, --from defaults to the source's DefaultRoot in the
// help text so users see what they'd get.
func bindImportFlags(cmd *cobra.Command, f *importFlags, src importer.Source) {
	defaultFrom := ""
	if src != nil {
		defaultFrom = src.DefaultRoot()
	}
	cmd.Flags().StringVar(&f.From, "from", "", fmt.Sprintf("transcript root to scan (default %s)", defaultFrom))
	cmd.Flags().StringVar(&f.Since, "since", "", "skip records older than this RFC3339 timestamp")
	cmd.Flags().IntVar(&f.Limit, "limit", 0, "stop after importing N records (0 = no cap)")
	cmd.Flags().BoolVar(&f.Force, "force", false, "ignore per-file mtime watermark and re-parse everything")
	cmd.Flags().BoolVar(&f.Strict, "strict", false, "treat upstream schema drift on a newer-than-known tool version as a hard reject")
}

// buildImportOptions converts CLI flags into importer.Options, wiring
// the cobra writer for verbose output and validating --since.
func buildImportOptions(f importFlags, cmd *cobra.Command) (importer.Options, error) {
	opts := importer.Options{
		Root:    f.From,
		Limit:   f.Limit,
		Force:   f.Force,
		Strict:  f.Strict,
		Verbose: false,
		Out:     cmd.OutOrStdout(),
	}
	if s := strings.TrimSpace(f.Since); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return opts, fmt.Errorf("--since: %w", err)
		}
		opts.Since = t
	}
	return opts, nil
}

// printImportStats writes the per-source summary line. Suspect-file
// count is appended only when non-zero so healthy runs stay quiet.
func printImportStats(cmd *cobra.Command, name string, st importer.Stats) {
	tail := ""
	if st.FilesSuspect > 0 {
		tail = fmt.Sprintf(" — drift watch: %d suspect file(s)", st.FilesSuspect)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"%s: %d files (%d skipped via watermark) — %d records (%d skipped, %d new prompts, %d responses attached, %d standalone)%s\n",
		name,
		st.Files, st.FilesSkipped,
		st.Records, st.RecordsSkipped, st.Inserted, st.Attached, st.Standalone,
		tail,
	)
}
