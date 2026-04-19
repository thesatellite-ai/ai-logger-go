package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	var tool string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install hooks for a tool (default: claude-code)",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := knownTools[tool]
			if !ok {
				return fmt.Errorf("unknown tool %q — try: %v", tool, knownToolNames())
			}
			switch spec.settings {
			case "claude":
				return installClaudeCodeHooks(cmd, spec)
			default:
				return printManualInstructions(cmd, spec)
			}
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "claude-code", "tool to install hooks for (claude-code, codex, opencode)")
	return cmd
}

func installClaudeCodeHooks(cmd *cobra.Command, spec toolHookSpec) error {
	path, err := config.ClaudeSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	bin, err := ailogCommand()
	if err != nil {
		return err
	}

	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &settings)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, sub := range spec.events {
		hooks[event] = []any{
			map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": fmt.Sprintf("%s hook %s", bin, sub),
					},
				},
			},
		}
	}
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "installed %s hooks → %s\n", spec.name, path)
	for event, sub := range spec.events {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-20s → ailog hook %s\n", event, sub)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nRestart your Claude Code session for hooks to take effect.")
	return nil
}

func printManualInstructions(cmd *cobra.Command, spec toolHookSpec) error {
	bin, err := ailogCommand()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s — no settings.json auto-installer yet.\n\n", spec.name)
	fmt.Fprintln(cmd.OutOrStdout(), "Wire the following commands into whatever hook mechanism the tool exposes:")
	for event, sub := range spec.events {
		fmt.Fprintf(cmd.OutOrStdout(), "  on %s:\n    %s hook %s\n", event, bin, sub)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nBoth commands read JSON from stdin. See `ailog hook", spec.name, "--help`.")
	return nil
}

func newHooksUninstallCmd() *cobra.Command {
	var tool string
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
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			settings := map[string]any{}
			if err := json.Unmarshal(b, &settings); err != nil {
				return err
			}
			if hooks, ok := settings["hooks"].(map[string]any); ok {
				for event := range spec.events {
					delete(hooks, event)
				}
				if len(hooks) == 0 {
					delete(settings, "hooks")
				} else {
					settings["hooks"] = hooks
				}
			}
			out, _ := json.MarshalIndent(settings, "", "  ")
			if err := os.WriteFile(path, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s hooks\n", spec.name)
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "claude-code", "tool to uninstall hooks for")
	return cmd
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
