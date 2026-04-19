package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Print a single entry by id (prefix ok)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			id, err := s.ResolveIDPrefix(ctx, args[0])
			if err != nil {
				return err
			}
			e, err := s.GetByID(ctx, id)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd, e)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "id:          %s\n", e.ID)
			fmt.Fprintf(out, "created:     %s\n", e.CreatedAt.Local().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(out, "tool:        %s\n", orDash(e.Tool))
			fmt.Fprintf(out, "project:     %s\n", orDash(e.Project))
			fmt.Fprintf(out, "cwd:         %s\n", orDash(e.Cwd))
			fmt.Fprintf(out, "branch:      %s @ %s\n", orDash(e.GitBranch), orDash(e.GitCommit))
			fmt.Fprintf(out, "session:     %s  %q\n", shortID(e.SessionID), e.SessionName)
			fmt.Fprintf(out, "turn:        %d\n", e.TurnIndex)
			fmt.Fprintf(out, "model:       %s\n", orDash(e.Model))
			fmt.Fprintf(out, "tokens:      in=%d out=%d\n", e.TokenCountIn, e.TokenCountOut)
			fmt.Fprintf(out, "tags:        %s\n", orDash(e.Tags))
			fmt.Fprintf(out, "starred:     %v\n", e.Starred)
			fmt.Fprintf(out, "hostname:    %s  user=%s  shell=%s  term=%s\n",
				orDash(e.Hostname), orDash(e.User), orDash(e.Shell), orDash(e.Terminal))
			fmt.Fprintln(out, "\n--- PROMPT ---")
			fmt.Fprintln(out, e.Prompt)
			if e.Response != "" {
				fmt.Fprintln(out, "\n--- RESPONSE ---")
				fmt.Fprintln(out, e.Response)
			}
			if e.Notes != "" {
				fmt.Fprintln(out, "\n--- NOTES ---")
				fmt.Fprintln(out, e.Notes)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of formatted text")
	return cmd
}
