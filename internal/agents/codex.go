package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/ubgo/agentio"
	"github.com/ubgo/agentio/codex"
)

// codexExtraPrefix namespaces Codex keys in an entry's raw JSON.
const codexExtraPrefix = "codex."

// codexAgent is the OpenAI Codex CLI: rollouts listed and read through
// agentio, plus the per-turn governance values ailog has always stored
// (approval policy, sandbox, collaboration mode, effort, originator,
// source, provider). agentio's neutral Turn has no field for them yet
// (gap A8), so the annotator picks them from agentio's typed rollout
// records; ailog decodes no rollout format itself, except the two keys of
// turn_context.collaboration_mode that codex.TurnContext keeps raw.
func codexAgent(c *codex.Agent) Agent {
	n := c.Neutral()
	read := neutralReader(n)
	return Agent{
		Tool:      ToolCodex,
		Neutral:   n,
		TestedMax: codex.TestedMax,
		transcripts: func(ctx context.Context, h agentio.Home) ([]Transcript, error) {
			_, out, err := conversationTranscripts(ctx, n, h, read)
			for i := range out {
				path := out[i].Path
				out[i].annotate = func() Annotator { return codexAnnotator(c, path) }
			}
			return out, err
		},
	}
}

// codexState is what the session_meta and turn_context records in effect
// at a point of a rollout say.
type codexState struct {
	originator, source, provider string
	approval, sandbox, timezone  string
	mode, effort, persona        string
	network, networkSet          bool
}

// codexSnapshot is the state from time at on.
type codexSnapshot struct {
	at time.Time
	st codexState
}

// codexAnnotator reads the rollout's session_meta and turn_context records
// once and annotates each turn with the state in effect at its time. A
// rollout that cannot be read annotates nothing. Disk · Read (one pass).
func codexAnnotator(c *codex.Agent, path string) Annotator {
	snaps := codexSnapshots(c, path)
	return func(t agentio.Turn) Annotation {
		i := sort.Search(len(snaps), func(i int) bool { return snaps[i].at.After(t.Time) })
		if i == 0 {
			if len(snaps) == 0 {
				return Annotation{}
			}
			i = 1 // a turn stamped before the first record takes the header state
		}
		return snaps[i-1].st.annotation()
	}
}

// codexSnapshots collects the state after each session_meta and
// turn_context record, in file order. Disk · Read.
func codexSnapshots(c *codex.Agent, path string) []codexSnapshot {
	r, err := c.OpenRolloutAt(path, 0)
	if err != nil {
		return nil
	}
	// Read-only file: a close error cannot lose data.
	defer func() { _ = r.Close() }()
	var (
		st  codexState
		out []codexSnapshot
	)
	for {
		before := r.Offset()
		rec, err := r.Next()
		switch {
		case errors.Is(err, io.EOF):
			return out
		case err != nil && r.Offset() > before:
			continue // one undecodable line; the rest of the rollout still counts
		case err != nil:
			return out // a read error or an over-long line: stop, keep what was seen
		}
		switch {
		case rec.SessionMeta != nil:
			m := rec.SessionMeta
			st.originator = firstSet(m.Originator, st.originator)
			st.source = firstSet(string(m.Source), st.source)
			st.provider = firstSet(m.ModelProvider, st.provider)
		case rec.TurnContext != nil:
			st.applyTurnContext(rec.TurnContext)
		default:
			continue
		}
		out = append(out, codexSnapshot{at: rec.Time, st: st})
	}
}

// codexCollab is the part of turn_context.collaboration_mode ailog keeps;
// codex.TurnContext holds the object raw.
type codexCollab struct {
	Mode     string `json:"mode"`
	Settings struct {
		ReasoningEffort string `json:"reasoning_effort"`
	} `json:"settings"`
}

// applyTurnContext folds one turn_context into the state; absent values
// keep the earlier ones.
func (st *codexState) applyTurnContext(tc *codex.TurnContext) {
	st.approval = firstSet(string(tc.ApprovalPolicy), st.approval)
	st.timezone = firstSet(tc.Timezone, st.timezone)
	st.persona = firstSet(string(tc.Personality), st.persona)
	if tc.SandboxPolicy != nil && tc.SandboxPolicy.Type != "" {
		st.sandbox = string(tc.SandboxPolicy.Type)
		st.network, st.networkSet = tc.SandboxPolicy.NetworkAccess, true
	}
	var collab codexCollab
	if len(tc.CollaborationMode) > 0 && json.Unmarshal(tc.CollaborationMode, &collab) == nil {
		st.mode = firstSet(collab.Mode, st.mode)
		st.effort = firstSet(collab.Settings.ReasoningEffort, st.effort)
	}
	st.effort = firstSet(string(tc.Effort), st.effort)
}

// annotation renders the state as stored columns and raw keys. Pure.
func (st codexState) annotation() Annotation {
	m := map[string]any{}
	add := func(k, v string) {
		if v != "" {
			m[codexExtraPrefix+k] = v
		}
	}
	add("model_provider", st.provider)
	add("sandbox_type", st.sandbox)
	add("collab_mode", st.mode)
	add("reasoning_effort", st.effort)
	add("personality", st.persona)
	add("timezone", st.timezone)
	if st.networkSet {
		m[codexExtraPrefix+"network_access"] = st.network
	}
	a := Annotation{PermissionMode: st.approval, Terminal: st.originator, TerminalTitle: st.source}
	if len(m) > 0 {
		a.Extras = m
	}
	return a
}

// firstSet returns v when non-empty, else fallback. Pure.
func firstSet(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
