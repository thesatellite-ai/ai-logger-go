package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/khanakia/ai-logger/internal/config"
	"github.com/spf13/cobra"
)

// toolHookSpec describes which hook events a tool emits and the ailog
// subcommands that adapt each event. Adding a tool = adding a row here.
type toolHookSpec struct {
	name     string
	settings string            // path resolver key (for now we only ship claude-code)
	events   map[string]string // harness event name → `ailog hook <path>`
	notes    string
}

var knownTools = map[string]toolHookSpec{
	"claude-code": {
		name:     "claude-code",
		settings: "claude",
		events: map[string]string{
			"UserPromptSubmit": "claude-code prompt",
			"Stop":             "claude-code stop",
		},
		notes: "Writes into ~/.claude/settings.json",
	},
	"codex": {
		name:     "codex",
		settings: "codex",
		events: map[string]string{
			"prompt": "codex prompt",
			"stop":   "codex stop",
		},
		notes: "Skeleton only — wire manually until Codex CLI ships a hooks config schema.",
	},
	"opencode": {
		name:     "opencode",
		settings: "opencode",
		events: map[string]string{
			"prompt": "opencode prompt",
			"stop":   "opencode stop",
		},
		notes: "Skeleton only — wire manually until opencode ships a hooks config schema.",
	},
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install/remove tool hooks that call ailog on every prompt + response",
	}
	cmd.AddCommand(newHooksInstallCmd(), newHooksUninstallCmd(), newHooksShowCmd(), newHooksListCmd())
	return cmd
}

func newHooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every tool adapter ailog knows about",
		Run: func(cmd *cobra.Command, args []string) {
			for _, t := range []string{"claude-code", "codex", "opencode"} {
				spec := knownTools[t]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n  %s\n", spec.name, spec.notes)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\nFor any other tool: use `ailog add --tool X ...` directly,")
			fmt.Fprintln(cmd.OutOrStdout(), "or `ailog hook generic` if the tool can pipe structured JSON.")
		},
	}
}

func ailogCommand() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs, nil
		}
	}
	if p, err := exec.LookPath("ailog"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("cannot locate ailog binary")
}

func newHooksInstallCmd() *cobra.Command {
	var (
		tool   string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install hooks for a tool (requires --tool)",
		Long: `Install ailog's hooks for a tool.

For claude-code this merges into ~/.claude/settings.json: ailog adds its
own UserPromptSubmit and Stop entries and leaves every other hook, key
and formatting byte as it was. Running it again changes nothing. The
previous file is saved as settings.json.bak-<timestamp> before any write.
Use --dry-run to print the diff without writing.`,
		// Previously --tool defaulted to "claude-code", so `ailog hooks
		// install` quietly edited ~/.claude/settings.json even if the
		// user was actually trying to install hooks for a different
		// tool. Make --tool required so cobra prints the usage block
		// (which lists every known tool) when it's missing.
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := knownTools[tool]
			if !ok {
				return fmt.Errorf("unknown tool %q — available: %v", tool, knownToolNames())
			}
			switch spec.settings {
			case "claude":
				return installClaudeCodeHooks(cmd, spec, dryRun)
			default:
				return printManualInstructions(cmd, spec)
			}
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "", "tool to install hooks for (claude-code, codex, opencode)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the settings diff without writing")
	_ = cmd.MarkFlagRequired("tool")
	return cmd
}

// installClaudeCodeHooks merges ailog's hooks into Claude Code's
// settings.json (see planClaudeInstall). It never replaces another
// tool's hook, writes nothing when ailog's entries are already there,
// and backs the file up before writing.
func installClaudeCodeHooks(cmd *cobra.Command, spec toolHookSpec, dryRun bool) error {
	path, err := config.ClaudeSettingsPath()
	if err != nil {
		return err
	}
	bin, err := ailogCommand()
	if err != nil {
		return err
	}
	before, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	after, added, err := planClaudeInstall(before, bin, spec.events)
	if err != nil {
		return fmt.Errorf("%s: %w (file left unchanged)", path, err)
	}
	out := cmd.OutOrStdout()
	if len(added) == 0 {
		fmt.Fprintf(out, "%s hooks already installed in %s — nothing to change\n", spec.name, path)
		return nil
	}
	if dryRun {
		writeLineDiff(out, path, before, after)
		fmt.Fprintf(out, "\ndry run: would add %s (nothing written)\n", strings.Join(added, ", "))
		return nil
	}
	backup, err := writeSettings(path, before, after, existed)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "installed %s hooks → %s\n", spec.name, path)
	for _, event := range added {
		fmt.Fprintf(out, "  %-20s → ailog hook %s\n", event, spec.events[event])
	}
	if backup != "" {
		fmt.Fprintf(out, "backup: %s\n", backup)
	}
	fmt.Fprintln(out, "\nRestart your Claude Code session for hooks to take effect.")
	return nil
}

func printManualInstructions(cmd *cobra.Command, spec toolHookSpec) error {
	bin, err := ailogCommand()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s — no settings.json auto-installer yet.\n\n", spec.name)
	fmt.Fprintln(cmd.OutOrStdout(), "Wire the following commands into whatever hook mechanism the tool exposes:")
	for _, event := range sortedEvents(spec.events) {
		fmt.Fprintf(cmd.OutOrStdout(), "  on %s:\n    %s\n", event, hookCommandLine(bin, spec.events[event]))
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nBoth commands read JSON from stdin. See `ailog hook", spec.name, "--help`.")
	return nil
}

func newHooksUninstallCmd() *cobra.Command {
	var (
		tool   string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove hooks installed by `ailog hooks install`",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := knownTools[tool]
			if !ok {
				return fmt.Errorf("unknown tool %q", tool)
			}
			if spec.settings != "claude" {
				fmt.Fprintln(cmd.OutOrStdout(), "only claude-code has an auto-installer; remove entries manually for other tools")
				return nil
			}
			path, err := config.ClaudeSettingsPath()
			if err != nil {
				return err
			}
			bin, err := ailogCommand()
			if err != nil {
				return err
			}
			before, existed, err := readSettings(path)
			if err != nil {
				return err
			}
			// Surgical removal: only commands that are ailog's own
			// (`<path>/ailog hook <tool> <event>`) go; other hooks on the
			// same events, other events and other keys keep their bytes.
			after, removed, err := planClaudeUninstall(before, bin, spec.events)
			if err != nil {
				return fmt.Errorf("%s: %w (file left unchanged)", path, err)
			}
			out := cmd.OutOrStdout()
			if removed == 0 {
				fmt.Fprintf(out, "no %s hooks to remove (nothing in %s matched `ailog hook`)\n", spec.name, path)
				return nil
			}
			if dryRun {
				writeLineDiff(out, path, before, after)
				fmt.Fprintf(out, "\ndry run: would remove %d entr%s (nothing written)\n", removed, pluralEntries(removed))
				return nil
			}
			backup, err := writeSettings(path, before, after, existed)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "removed %d %s hook entr%s from %s (non-ailog hooks preserved)\nbackup: %s\n",
				removed, spec.name, pluralEntries(removed), path, backup)
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "claude-code", "tool to uninstall hooks for")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the settings diff without writing")
	return cmd
}

// pluralEntries returns "y" for 1 entry and "ies" otherwise so the
// output message reads naturally.
func pluralEntries(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func newHooksShowCmd() *cobra.Command {
	var tool string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the commands a given tool's hooks will invoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := knownTools[tool]
			if !ok {
				return fmt.Errorf("unknown tool %q — try: %v", tool, knownToolNames())
			}
			bin, err := ailogCommand()
			if err != nil {
				return err
			}
			for event, sub := range spec.events {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s  %s hook %s\n", event, bin, sub)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "claude-code", "tool to inspect")
	return cmd
}

func knownToolNames() []string {
	names := make([]string, 0, len(knownTools))
	for k := range knownTools {
		names = append(names, k)
	}
	return names
}
