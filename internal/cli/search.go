package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/khanakia/ai-logger/ent"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var (
		project, tool, session, branch, since string
		limit                                 int
		asJSON                                bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across all logged entries",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cutoff, err := parseSince(since)
			if err != nil {
				return err
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			query := strings.Join(args, " ")
			f := store.SearchFilter{
				Project:   project,
				Tool:      tool,
				SessionID: session,
				Branch:    branch,
				Limit:     limit,
			}
			entries, err := s.Search(ctx, query, f)
			if err != nil {
				return err
			}
			if !cutoff.IsZero() {
				filtered := entries[:0]
				for _, e := range entries {
					if e.CreatedAt.After(cutoff) {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}
			if asJSON {
				return writeJSON(cmd, entries)
			}
			renderEntryList(cmd, entries)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project (host/owner/repo)")
	cmd.Flags().StringVar(&tool, "tool", "", "filter by tool")
	cmd.Flags().StringVar(&session, "session", "", "filter by session id")
	cmd.Flags().StringVar(&branch, "branch", "", "filter by git branch")
	cmd.Flags().StringVar(&since, "since", "", "only entries newer than duration (e.g. 24h, 7d, 2w)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max results")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of table")
	return cmd
}

func writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderEntryList(cmd *cobra.Command, entries []*ent.Entry) {
	out := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(out, "(no entries)")
		return
	}
	for _, e := range entries {
		fmt.Fprintf(out, "%s  %s  %s  %s  %s\n",
			shortID(e.ID),
			e.CreatedAt.Local().Format("2006-01-02 15:04"),
			pad(orDash(e.Tool), 12),
			pad(orDash(shortProject(e.Project)), 28),
			truncate(e.Prompt, 80),
		)
	}
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func shortProject(p string) string {
	if i := strings.Index(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
