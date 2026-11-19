// Package cli — `ailog import` is a tool-agnostic backfill driver.
// Each agent ailog knows (internal/agents, backed by agentio) is a
// subcommand; this file is the cobra wiring + flag plumbing.
//
//	ailog import claude-code [--from HOME]
//	ailog import codex       [--from HOME]
//	ailog import gemini      [--from HOME]
//	ailog import pi          [--from HOME]
//	ailog import opencode    [--from HOME]
//	ailog import all         [--force] [--since RFC3339] [--limit N]
//
// Without --from every home of the agent is read (for Claude Code
// ~/.claude and every ~/.claude-*; AILOG_CLAUDE_HOME narrows it to one).
// Idempotency comes from the store's import_lines table (per-message
// dedup keys) and import_state table (per-transcript watermark and read
// cursor). Re-running the same command is cheap and safe; pass --force to
// read every transcript from the start when re-parsing is intentional.
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/agents"
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
		Long: "Each subcommand reads one agent's transcripts in every home it has " +
			"(through agentio). Re-runs are idempotent (per-message dedup) and fast " +
			"(per-transcript watermarks and read cursors). Use `import all` to run every agent.",
	}

	for _, tool := range agents.ToolNames() {
		cmd.AddCommand(newImportSourceCmd(tool))
	}
	cmd.AddCommand(newImportAllCmd())
	return cmd
}

// newImportSourceCmd builds one `ailog import <tool>` subcommand. The
// flag set is identical across agents so muscle memory transfers.
func newImportSourceCmd(tool string) *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   tool,
		Short: fmt.Sprintf("Import %s transcripts from every home (or --from HOME)", tool),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			reg, err := agents.FromOS()
			if err != nil {
				return err
			}
			ag, ok := reg.ByTool(tool)
			if !ok {
				return fmt.Errorf("unknown agent %q", tool)
			}
			st, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			opts, err := buildImportOptions(f, cmd)
			if err != nil {
				return err
			}

			stats, err := importer.Run(ctx, st, ag, opts)
			printImportStats(cmd, tool, stats)
			return err
		},
	}
	bindImportFlags(cmd, &f, true)
	return cmd
}

// newImportAllCmd runs every agent in turn, sharing one store handle.
// Stops on the first error so partial-state failures surface loudly.
func newImportAllCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Import every agent's transcripts from every home",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			reg, err := agents.FromOS()
			if err != nil {
				return err
			}
			st, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			for _, ag := range reg.All() {
				opts, err := buildImportOptions(f, cmd)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "→ %s\n", ag.Tool)
				stats, err := importer.Run(ctx, st, ag, opts)
				printImportStats(cmd, ag.Tool, stats)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
	bindImportFlags(cmd, &f, false)
	return cmd
}

// bindImportFlags attaches the shared flag set to a subcommand. --from
// exists only on a per-agent subcommand: one folder can't be a home of
// every agent, so `all` always reads every home.
func bindImportFlags(cmd *cobra.Command, f *importFlags, withFrom bool) {
	if withFrom {
		cmd.Flags().StringVar(&f.From, "from", "", "read only the agent home at this folder (e.g. ~/.claude-work; default every home)")
	}
	cmd.Flags().StringVar(&f.Since, "since", "", "skip records older than this RFC3339 timestamp")
	cmd.Flags().IntVar(&f.Limit, "limit", 0, "stop after importing N records (0 = no cap)")
	cmd.Flags().BoolVar(&f.Force, "force", false, "ignore watermarks and read cursors; read every transcript from the start")
	cmd.Flags().BoolVar(&f.Strict, "strict", false, "treat upstream schema drift on a newer-than-known tool version as a hard reject")
}

// buildImportOptions converts CLI flags into importer.Options, wiring
// the cobra writer for verbose output and validating --since.
func buildImportOptions(f importFlags, cmd *cobra.Command) (importer.Options, error) {
	opts := importer.Options{
		Home:    f.From,
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
	if st.Linked > 0 {
		tail += fmt.Sprintf(" — %d prompt(s) linked to live-hook rows", st.Linked)
	}
	if st.BadLines > 0 {
		tail += fmt.Sprintf(" — %d undecodable line(s) skipped", st.BadLines)
	}
	if st.FilesSuspect > 0 {
		tail += fmt.Sprintf(" — drift watch: %d suspect file(s)", st.FilesSuspect)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"%s: %d home(s), %d files (%d skipped via watermark) — %d records (%d skipped, %d new prompts, %d responses attached, %d standalone)%s\n",
		name,
		st.Homes, st.Files, st.FilesSkipped,
		st.Records, st.RecordsSkipped, st.Inserted, st.Attached, st.Standalone,
		tail,
	)
}
