package cli

import (
	"fmt"
	"os"
	"strings"

	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/khanakia/ai-logger/internal/redact"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		promptFlag, responseFlag, entryFlag string
		tool, model, sessionID, sessionName string
		rawFile                             string
		tokensIn, tokensOut                 int
		tagCSV                              string
		noRedact                            bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Log a prompt or attach a response",
		Long: `Log a prompt (creates a new entry) or attach a response to an existing
entry via --entry <id>. Body can come from a flag or stdin.

Examples:
  ailog add --prompt "fix the race in worker.go"
  echo "big prompt" | ailog add
  ailog add --response "my reply" --entry 01h3a4b5
  ailog add --prompt "one-shot" --response "done" --model claude-opus-4-7`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			stdin, err := readStdinIfPiped()
			if err != nil {
				return err
			}
			prompt := strings.TrimSpace(firstNonEmpty(promptFlag, stdin))
			response := strings.TrimSpace(responseFlag)

			// If --entry given without --response, treat as "attach empty response" error.
			if entryFlag != "" && response == "" && prompt == "" {
				return fmt.Errorf("--entry given but no --response to attach")
			}

			var raw string
			if rawFile != "" {
				b, err := os.ReadFile(rawFile)
				if err != nil {
					return fmt.Errorf("read --raw file: %w", err)
				}
				raw = string(b)
			}

			// Secret scrubbing.
			if !noRedact {
				prompt = redact.Scrub(prompt)
				response = redact.Scrub(response)
			}

			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			// --entry path: attach response to existing entry.
			if entryFlag != "" && prompt == "" {
				id, err := s.ResolveIDPrefix(ctx, entryFlag)
				if err != nil {
					return err
				}
				if err := s.AttachResponse(ctx, id, response, model, tokensOut); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), id)
				return nil
			}

			// Insert path: new entry, optionally with response already attached.
			if prompt == "" {
				return fmt.Errorf("no --prompt given (and stdin empty)")
			}

			env := aictx.Collect(ctx)

			in := store.InsertEntryInput{
				Tool:          pickTool(tool, env.Session.Tool),
				CWD:           env.CWD,
				Project:       env.Project,
				RepoOwner:     env.Git.Owner,
				RepoName:      env.Git.Name,
				RepoRemote:    env.Git.Remote,
				GitBranch:     env.Git.Branch,
				GitCommit:     env.Git.Commit,
				SessionID:     pickFirst(sessionID, env.Session.ID),
				SessionName:   pickFirst(sessionName, env.Session.Name),
				Hostname:      env.Env.Hostname,
				User:          env.Env.User,
				Shell:         env.Env.Shell,
				Terminal:      env.Env.Terminal,
				TerminalTitle: env.Env.TerminalTitle,
				TTY:           env.Env.TTY,
				PID:           env.Env.PID,
				Prompt:        prompt,
				Response:      response,
				Model:         model,
				Raw:           raw,
				TokensIn:      tokensIn,
				TokensOut:     tokensOut,
				Tags:          tagCSV,
			}
			id, err := s.InsertEntry(ctx, in)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}

	cmd.Flags().StringVar(&promptFlag, "prompt", "", "prompt text (or pipe via stdin)")
	cmd.Flags().StringVar(&responseFlag, "response", "", "assistant response text")
	cmd.Flags().StringVar(&entryFlag, "entry", "", "existing entry id prefix to attach response to")
	cmd.Flags().StringVar(&tool, "tool", "", "tool name (claude-code, cursor, codex, chatgpt, ...)")
	cmd.Flags().StringVar(&model, "model", "", "model id, e.g. claude-opus-4-7")
	cmd.Flags().StringVar(&sessionID, "session", "", "session id — groups related turns")
	cmd.Flags().StringVar(&sessionName, "session-name", "", "human-readable session label")
	cmd.Flags().StringVar(&rawFile, "raw", "", "path to raw JSON payload to store verbatim")
	cmd.Flags().IntVar(&tokensIn, "tokens-in", 0, "input tokens if known")
	cmd.Flags().IntVar(&tokensOut, "tokens-out", 0, "output tokens if known")
	cmd.Flags().StringVar(&tagCSV, "tag", "", "comma-separated tags")
	cmd.Flags().BoolVar(&noRedact, "no-redact", false, "disable automatic secret scrubbing")
	return cmd
}

func pickTool(explicit, fromEnv string) string {
	if explicit != "" {
		return explicit
	}
	if fromEnv != "" {
		return fromEnv
	}
	return "manual"
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func pickFirst(vs ...string) string { return firstNonEmpty(vs...) }
