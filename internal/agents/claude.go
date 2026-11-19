package agents

import (
	"context"
	"os"

	"github.com/ubgo/agentio"
	"github.com/ubgo/agentio/claude"
)

// claudeAgent is Claude Code. Its transcripts are the conversations of a
// home plus each conversation's subagent runs (projects/<key>/<session>/
// subagents/agent-*.jsonl), which ailog has always imported: agentio's
// neutral listing leaves subagents out (gap A9), so they come from the
// vendor SubagentRuns (runs with and without a .meta.json). home, when
// set, is the only home used.
func claudeAgent(c *claude.Agent, home string) Agent {
	n := c.Neutral()
	a := Agent{
		Tool:      ToolClaude,
		Neutral:   n,
		TestedMax: claude.TestedMax,
	}
	if home != "" {
		a.homes = func() ([]agentio.Home, error) { return []agentio.Home{c.HomeAt(home).Neutral()}, nil }
	}
	read := neutralReader(n)
	a.transcripts = func(ctx context.Context, h agentio.Home) ([]Transcript, error) {
		convs, out, err := conversationTranscripts(ctx, n, h, read)
		if err != nil {
			return nil, err
		}
		ch, err := c.HomeOf(h)
		if err != nil {
			return nil, err
		}
		for _, conv := range convs {
			runs, err := c.SubagentRuns(ch, conv.Dir, conv.ID)
			if err != nil {
				continue // an unreadable subagents folder must not hide the main transcript
			}
			for _, r := range runs {
				// A run's meta file can outlive its transcript; list
				// only transcripts that exist.
				if _, err := os.Stat(r.Transcript); err == nil {
					out = append(out, fileTranscript(r.Transcript, conv, read))
				}
			}
		}
		return out, nil
	}
	return a
}
