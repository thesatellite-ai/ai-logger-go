package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

// genericPayload is the neutral JSON shape ailog accepts from any tool
// that can pipe a small JSON object on stdin. A single message can
// carry both prompt and response — the adapter inserts a new entry
// when prompt is set, attaches a response when response is set, or
// both in sequence when both are present.
type genericPayload struct {
	Tool      string `json:"tool"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Prompt    string `json:"prompt"`
	Response  string `json:"response"`
	Model     string `json:"model"`
}

// newGenericHookCmd is the universal escape hatch. Anything that can
// shell out and pipe a JSON object into `ailog hook generic` is
// supported without writing a tool-specific adapter.
func newGenericHookCmd() *cobra.Command {
	var toolOverride string
	cmd := &cobra.Command{
		Use:   "generic",
		Short: "Neutral JSON adapter — pipe {tool, session_id, cwd, prompt?, response?, model?} on stdin",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := io.ReadAll(os.Stdin)
			var p genericPayload
			if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
				return err
			}
			// Tool resolution priority: flag > payload field > "generic".
			tool := p.Tool
			if toolOverride != "" {
				tool = toolOverride
			}
			if tool == "" {
				tool = "generic"
			}
			hookDebug("generic", raw, fmt.Sprintf("tool=%s session=%s promptLen=%d respLen=%d",
				tool, p.SessionID, len(p.Prompt), len(p.Response)))

			if p.Prompt != "" {
				if err := capturePrompt(cmd.Context(), promptCapture{
					Tool: tool, SessionID: p.SessionID, CWD: p.CWD, Prompt: p.Prompt,
				}); err != nil {
					return err
				}
			}
			if p.Response != "" && p.SessionID != "" {
				return attachResponse(cmd.Context(), p.SessionID, p.Response, p.Model, store.AttachResponseInput{})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&toolOverride, "tool", "", "override the tool field in the payload")
	return cmd
}
