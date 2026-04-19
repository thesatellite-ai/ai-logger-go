package cli

import (
	"github.com/spf13/cobra"
)

// Populated by cmd/ailog/main.go from its ldflags-injected vars.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ailog",
		Short:         "Local log of AI prompts & responses",
		Long:          "ailog captures every prompt and response you exchange with AI tools so nothing gets lost between sessions.",
		Version:       Version + " (" + Commit + ", " + BuildDate + ")",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newAddCmd(),
		newSearchCmd(),
		newShowCmd(),
		newLastCmd(),
		newSessionCmd(),
		newTagCmd(),
		newStarCmd(),
		newUnstarCmd(),
		newTemplatesCmd(),
		newStatsCmd(),
		newExportCmd(),
		newImportCmd(),
		newRedactCmd(),
		newPurgeCmd(),
		newSkillCmd(),
		newHooksCmd(),
		newHookCmd(),
		newDebugCmd(),
	)

	return cmd
}
