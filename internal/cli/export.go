package cli

import (
	"fmt"
	"strings"

	"github.com/khanakia/ai-logger/ent"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var format, project, tool, since string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Dump entries as JSON or Markdown",
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
			defer s.Close()
			all, err := s.All(ctx)
			if err != nil {
				return err
			}
			filtered := all[:0]
			for _, e := range all {
				if project != "" && e.Project != project {
					continue
				}
				if tool != "" && e.Tool != tool {
					continue
				}
				if !cutoff.IsZero() && e.CreatedAt.Before(cutoff) {
					continue
				}
				filtered = append(filtered, e)
			}
			switch format {
			case "json":
				return writeJSON(cmd, filtered)
			case "md", "markdown":
				writeMarkdown(cmd, filtered)
				return nil
			default:
				return fmt.Errorf("unknown format %q (want json or md)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "json | md")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	cmd.Flags().StringVar(&tool, "tool", "", "filter by tool")
	cmd.Flags().StringVar(&since, "since", "", "only since duration (e.g. 7d)")
	return cmd
}

func writeMarkdown(cmd *cobra.Command, entries []*ent.Entry) {
	out := cmd.OutOrStdout()
	for _, e := range entries {
		fmt.Fprintf(out, "## %s — %s\n\n", e.CreatedAt.Local().Format("2006-01-02 15:04"), shortID(e.ID))
		if e.Project != "" {
			fmt.Fprintf(out, "- project: `%s`\n", e.Project)
		}
		if e.Tool != "" {
			fmt.Fprintf(out, "- tool: `%s`\n", e.Tool)
		}
		if e.Model != "" {
			fmt.Fprintf(out, "- model: `%s`\n", e.Model)
		}
		if e.GitBranch != "" {
			fmt.Fprintf(out, "- branch: `%s @ %s`\n", e.GitBranch, e.GitCommit)
		}
		if e.Tags != "" {
			fmt.Fprintf(out, "- tags: %s\n", e.Tags)
		}
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "### Prompt")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, indent(e.Prompt, "> "))
		if e.Response != "" {
			fmt.Fprintln(out, "\n### Response")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, e.Response)
		}
		fmt.Fprintln(out, "\n---")
	}
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
