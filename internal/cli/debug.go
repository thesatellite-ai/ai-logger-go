package cli

import (
	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/spf13/cobra"
)

func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Diagnostic subcommands",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "context",
		Short: "Dump the resolved invocation Context as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, aictx.Collect(cmd.Context()))
		},
	})
	return cmd
}
