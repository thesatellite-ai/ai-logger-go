package cli

import (
	"fmt"

	"github.com/khanakia/ai-logger/internal/config"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the ailog home directory and initialize the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := config.EnsureHome()
			if err != nil {
				return err
			}
			dbPath, err := config.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			fmt.Fprintf(cmd.OutOrStdout(), "ailog home: %s\n", home)
			fmt.Fprintf(cmd.OutOrStdout(), "database:   %s\n\n", dbPath)
			fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
			fmt.Fprintln(cmd.OutOrStdout(), "  ailog skill install        # Claude Code integration (auto-logs on each turn)")
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "For other tools (Cursor, Codex, manual use):")
			fmt.Fprintln(cmd.OutOrStdout(), "  ailog add --tool <name> --session <id> --prompt \"...\"")
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "No shell rc changes required. `--tool` defaults to \"manual\" when unset.")
			return nil
		},
	}
}
