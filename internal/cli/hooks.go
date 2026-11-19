package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
)

// ailogBinaryName is the binary name an ailog hook command runs.
const ailogBinaryName = "ailog"

// shellSafePath matches a binary path that needs no shell quoting.
var shellSafePath = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// hookCommandLine renders the shell command an agent runs for one hook:
// "<bin> hook <tool> <sub>", the bin single-quoted when it holds a
// character a shell would split or expand. The exact string every ailog
// release writes, so an installed entry stays recognisable.
func hookCommandLine(bin, tool, sub string) string {
	if !shellSafePath.MatchString(bin) {
		bin = "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	}
	return bin + " hook " + tool + " " + sub
}

// isAilogCommandFor reports whether a hook command runs some ailog binary
// (any path, quoted or not) with "hook <tool> <sub>": the entries an
// older install or another ailog binary wrote. A compound shell command
// that merely mentions ailog is not one.
func isAilogCommandFor(cmd, tool, sub string) bool {
	cmd = strings.TrimSpace(cmd)
	suffix := " hook " + tool + " " + sub
	if !strings.HasSuffix(cmd, suffix) {
		return false
	}
	bin := strings.TrimSpace(strings.TrimSuffix(cmd, suffix))
	quoted := len(bin) >= 2 && (bin[0] == '\'' || bin[0] == '"') && bin[len(bin)-1] == bin[0]
	switch {
	case quoted:
		bin = bin[1 : len(bin)-1]
	case strings.ContainsAny(bin, " \t;&|<>$`"):
		return false // a compound shell command, not a bare binary path
	}
	return strings.TrimSuffix(filepath.Base(bin), ".exe") == ailogBinaryName
}

// isAilogHook reports whether cmd is any ailog hook command of tool.
func isAilogHook(cmd, tool string) bool {
	for _, ks := range hookKindSubs() {
		if isAilogCommandFor(cmd, tool, ks.Sub) {
			return true
		}
	}
	return false
}

// ailogCommand returns the absolute path of the running ailog binary, the
// command the installed hooks run.
func ailogCommand() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs, nil
		}
	}
	if p, err := exec.LookPath(ailogBinaryName); err == nil {
		return p, nil
	}
	return "", errors.New("cannot locate ailog binary")
}

// wantedHooks are the entries ailog installs for tool, run by bin.
func wantedHooks(bin, tool string) []agentio.HookEntry {
	var out []agentio.HookEntry
	for _, ks := range hookKindSubs() {
		out = append(out, agentio.HookEntry{Kind: ks.Kind, Command: hookCommandLine(bin, tool, ks.Sub)})
	}
	return out
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install/remove agent hooks that call ailog on every prompt + response",
	}
	cmd.AddCommand(newHooksInstallCmd(), newHooksUninstallCmd(), newHooksShowCmd(), newHooksListCmd())
	return cmd
}

// hookAgent resolves --tool to an agent with command hooks.
func hookAgent(tool string) (agents.Agent, agentio.HookInstaller, error) {
	reg, err := agents.FromOS()
	if err != nil {
		return agents.Agent{}, nil, err
	}
	ag, ok := reg.ByTool(tool)
	if !ok {
		return agents.Agent{}, nil, fmt.Errorf("unknown tool %q — available: %s", tool, strings.Join(hookTools(), ", "))
	}
	h, ok := ag.Hooks()
	if !ok {
		return agents.Agent{}, nil, fmt.Errorf("%s has no command hooks; capture it with `ailog import %s`", tool, tool)
	}
	return ag, h, nil
}

// hookHomes resolves the homes a hooks command acts on: --home, else
// every home agentio finds (AILOG_CLAUDE_HOME narrows Claude to one),
// else the agent's default home when none exists yet.
func hookHomes(ag agents.Agent, dir string) ([]agentio.Home, error) {
	if dir != "" {
		return []agentio.Home{ag.HomeAt(expandUser(dir))}, nil
	}
	hs, err := ag.Homes()
	if err != nil {
		return nil, err
	}
	if len(hs) == 0 {
		hs = []agentio.Home{ag.Neutral.DefaultHome()}
	}
	return hs, nil
}

// expandUser turns a leading "~" into the user's home directory.
func expandUser(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(u, strings.TrimPrefix(p, "~"))
}

func newHooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every agent ailog knows and how it is captured",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := agents.FromOS()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, ag := range reg.All() {
				if h, ok := ag.Hooks(); ok {
					var kinds []string
					for _, k := range h.HookKinds() {
						kinds = append(kinds, k.String())
					}
					fmt.Fprintf(out, "%s\n  command hooks (%s): `ailog hooks install --tool %s`; backfill: `ailog import %s`\n",
						ag.Tool, strings.Join(kinds, ", "), ag.Tool, ag.Tool)
					continue
				}
				fmt.Fprintf(out, "%s\n  no command hooks; captured by `ailog import %s`\n", ag.Tool, ag.Tool)
			}
			fmt.Fprintln(out, "\nFor any other tool: use `ailog add --tool X ...` directly,")
			fmt.Fprintln(out, "or `ailog hook generic` if the tool can pipe structured JSON.")
			return nil
		},
	}
}

func newHooksInstallCmd() *cobra.Command {
	var (
		tool, home string
		dryRun     bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install hooks for an agent in every home it has (requires --tool)",
		Long: `Install ailog's prompt and stop hooks for an agent (claude-code, codex,
gemini) in every home agentio finds (for Claude Code ~/.claude and every
~/.claude-*; AILOG_CLAUDE_HOME or --home narrows it to one).

The agent's settings file is merged, never replaced: ailog appends its own
entries and leaves every other hook, key and byte as it was, after saving
a backup next to the file. Running it again changes nothing. An older
ailog entry that runs a different ailog binary is replaced by one running
this binary. Use --dry-run to print the plan without writing.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ag, h, err := hookAgent(tool)
			if err != nil {
				return err
			}
			bin, err := ailogCommand()
			if err != nil {
				return err
			}
			homes, err := hookHomes(ag, home)
			if err != nil {
				return err
			}
			for _, hm := range homes {
				if err := installHooksIn(cmd.OutOrStdout(), ag, h, hm, bin, dryRun); err != nil {
					return err
				}
			}
			if !dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "\nRestart running %s sessions for hooks to take effect.\n", tool)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "", "agent to install hooks for ("+strings.Join(hookTools(), ", ")+")")
	cmd.Flags().StringVar(&home, "home", "", "only this agent home (default: every home found)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be added without writing")
	_ = cmd.MarkFlagRequired("tool")
	return cmd
}

// installHooksIn installs ailog's hooks into one home: older ailog entries
// running another binary are removed first (their own backup), then the
// missing entries are appended (another backup). A dry run only plans and
// prints both.
func installHooksIn(out io.Writer, ag agents.Agent, h agentio.HookInstaller, home agentio.Home, bin string, dryRun bool) error {
	want := wantedHooks(bin, ag.Tool)
	current := map[string]bool{}
	for _, w := range want {
		current[w.Command] = true
	}
	plan, err := h.PlanHooks(home, want)
	if err != nil {
		return fmt.Errorf("%s: %w (file left unchanged)", home.Dir(), err)
	}
	// Older ailog entries of this tool that run another binary: replaced,
	// so the agent never runs two.
	isStale := func(cmd string) bool {
		return isAilogHook(cmd, ag.Tool) && !current[strings.TrimSpace(cmd)]
	}
	stale := plan.Matching(isStale)
	if dryRun {
		printHookPlan(out, ag.Tool, plan, stale)
		fmt.Fprintln(out, "dry run: nothing written")
		return nil
	}
	if len(stale) > 0 {
		n, b, err := h.RemoveHooks(home, isStale)
		if err != nil {
			return fmt.Errorf("%s: %w", plan.File, err)
		}
		fmt.Fprintf(out, "replaced %d older ailog hook entr%s in %s\n", n, pluralEntries(n), plan.File)
		printBackup(out, b)
		if plan, err = h.PlanHooks(home, want); err != nil {
			return fmt.Errorf("%s: %w", home.Dir(), err)
		}
	}
	if plan.IsNoop() {
		fmt.Fprintf(out, "%s hooks already installed in %s — nothing to change\n", ag.Tool, plan.File)
		printManual(out, plan)
		return nil
	}
	b, err := h.ApplyHooks(plan)
	if err != nil {
		return fmt.Errorf("%s: %w", plan.File, err)
	}
	fmt.Fprintf(out, "installed %s hooks → %s\n", ag.Tool, plan.File)
	for _, e := range plan.Add {
		fmt.Fprintf(out, "  %-20s → %s\n", e.Event, e.Command)
	}
	printBackup(out, b)
	printManual(out, plan)
	return nil
}

// printHookPlan prints what a plan would add, what it would replace and
// what is already there.
func printHookPlan(out io.Writer, tool string, p agentio.HookPlan, replace []agentio.HookEntry) {
	fmt.Fprintf(out, "%s: %s\n", tool, p.File)
	for _, e := range replace {
		fmt.Fprintf(out, "  replace %-20s %s\n", e.Event, e.Command)
	}
	for _, e := range p.Present {
		fmt.Fprintf(out, "  present %-20s %s\n", e.Event, e.Command)
	}
	for _, e := range p.Add {
		fmt.Fprintf(out, "  add     %-20s %s\n", e.Event, e.Command)
	}
	printManual(out, p)
}

// printManual prints the steps the agent needs that ailog does not take.
func printManual(out io.Writer, p agentio.HookPlan) {
	for _, m := range p.Manual {
		fmt.Fprintf(out, "  manual step: %s\n", m)
	}
}

// printBackup names the backup copies a write made.
func printBackup(out io.Writer, b agentio.Backup) {
	for _, e := range b.Entries {
		fmt.Fprintf(out, "backup: %s\n", e.Copy)
	}
}

func newHooksUninstallCmd() *cobra.Command {
	var tool, home string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the hooks `ailog hooks install` added, in every home",
		Long: `Remove every ailog hook entry of an agent (any ailog binary path) from
each home's settings file. Other hooks are kept as they are; the file is
backed up before it changes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ag, h, err := hookAgent(tool)
			if err != nil {
				return err
			}
			homes, err := hookHomes(ag, home)
			if err != nil {
				return err
			}
			// This binary's own entries count even when it is not named
			// ailog (a renamed or test build).
			own := map[string]bool{}
			if bin, err := ailogCommand(); err == nil {
				for _, w := range wantedHooks(bin, tool) {
					own[w.Command] = true
				}
			}
			out := cmd.OutOrStdout()
			for _, hm := range homes {
				n, b, err := h.RemoveHooks(hm, func(c string) bool {
					return own[strings.TrimSpace(c)] || isAilogHook(c, tool)
				})
				if err != nil {
					return fmt.Errorf("%s: %w (file left unchanged)", hm.Dir(), err)
				}
				if n == 0 {
					fmt.Fprintf(out, "no %s hooks to remove in %s\n", tool, hm.Dir())
					continue
				}
				fmt.Fprintf(out, "removed %d %s hook entr%s from %s (non-ailog hooks preserved)\n",
					n, tool, pluralEntries(n), hm.Dir())
				printBackup(out, b)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", agents.ToolClaude, "agent to uninstall hooks for")
	cmd.Flags().StringVar(&home, "home", "", "only this agent home (default: every home found)")
	return cmd
}

// pluralEntries returns the suffix for "entr…": "y" for one, "ies" else.
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
		Short: "Print the commands an agent's hooks will invoke",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, _, err := hookAgent(tool); err != nil {
				return err
			}
			bin, err := ailogCommand()
			if err != nil {
				return err
			}
			for _, w := range wantedHooks(bin, tool) {
				fmt.Fprintf(cmd.OutOrStdout(), "%-14s  %s\n", w.Kind, w.Command)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", agents.ToolClaude, "agent to inspect")
	return cmd
}
