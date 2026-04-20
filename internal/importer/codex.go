package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// codex parses OpenAI Codex CLI session rollouts found at
// ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl.
//
// Each line is a one-record envelope with `type` ∈ {session_meta,
// turn_context, event_msg, response_item} and a tool-specific
// `payload`. We only need a slice of these:
//
//   - session_meta  → header (one per file): session id, cwd, cli_version.
//   - turn_context  → per-turn metadata: current model.
//   - event_msg.user_message    → the user's prompt text.
//   - event_msg.agent_message   → the assistant's final-answer text.
//   - event_msg.token_count     → per-turn usage (input / cached / output);
//                                 attached to the most recent agent_message.
//
// Everything else (response_item, agent_reasoning, exec_command_*) is
// internal-pipeline state we don't need for backfill — they'd inflate
// the entry count without adding new searchable content.
type codex struct{}

func init() { Register(codex{}) }

func (codex) Name() string        { return "codex" }
func (codex) DefaultRoot() string { return expandHome("~/.codex/sessions") }

// LastKnownVersion is the highest Codex CLI version this parser was
// validated against. Codex versions look like "0.118.0-alpha.2" — the
// versionCmp helper walks numeric segments and treats prerelease tags
// per semver convention.
func (codex) LastKnownVersion() string { return "0.118.0" }

// Anchor: assistant turns must report token usage. Codex emits a
// token_count event_msg right after every agent_message; the driver's
// per-turn pairing copies token_count.info.last_token_usage onto the
// pending assistant Record. Zero across an entire session means the
// token_count event shape moved.
func (codex) Anchor(r Record) bool {
	return r.Role == RoleAssistant && r.TokensIn > 0
}

func (codex) Discover(_ context.Context, root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(p) == ".jsonl" && filepath.Base(p) != "session_index.jsonl" {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// codexEnvelope is the outer wrapper every line in a Codex rollout uses.
type codexEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// codexSessionMeta is the payload of the one-per-file session_meta line.
//
// Beyond id/cwd/version we also capture provenance fields that Claude
// transcripts simply don't have:
//   - originator: "Codex Desktop" vs "Codex CLI" — the host application
//   - source:     "vscode" / "terminal" / etc. — embedding context
//   - model_provider: "openai" / "anthropic" / "google" — Codex routes to
//                     multiple providers, useful for grouping cost
type codexSessionMeta struct {
	ID            string `json:"id"`
	Cwd           string `json:"cwd"`
	CLIVersion    string `json:"cli_version"`
	Originator    string `json:"originator"`
	Source        string `json:"source"`
	ModelProvider string `json:"model_provider"`
}

// codexTurnContext is the per-turn config including the active model
// plus Codex-specific governance knobs (sandbox / approval / mode).
// We capture all of it so the per-turn raw blob can faithfully describe
// "what mode was Codex running in when this answer happened".
type codexTurnContext struct {
	Cwd               string             `json:"cwd"`
	Model             string             `json:"model"`
	ApprovalPolicy    string             `json:"approval_policy"`
	Personality       string             `json:"personality"`
	Timezone          string             `json:"timezone"`
	SandboxPolicy     codexSandboxPolicy `json:"sandbox_policy"`
	CollaborationMode codexCollabMode    `json:"collaboration_mode"`
}

// codexSandboxPolicy is Codex's filesystem/network sandbox setup. We
// only keep the discriminator + network bit; writable_roots is
// per-cwd noise we don't need.
type codexSandboxPolicy struct {
	Type          string `json:"type"`
	NetworkAccess bool   `json:"network_access"`
}

// codexCollabMode carries the active "default | plan" mode plus the
// reasoning effort the assistant is running at.
type codexCollabMode struct {
	Mode     string                  `json:"mode"`
	Settings codexCollabModeSettings `json:"settings"`
}

type codexCollabModeSettings struct {
	ReasoningEffort string `json:"reasoning_effort"`
}

// codexExtras packs Codex-specific provenance into a small map the
// driver will JSON-encode into Record.Raw. Each key is omitted when
// its value is empty/zero so the resulting blob stays compact.
//
// Keys are namespaced with a "codex." prefix so a future cross-tool
// reader can route them without ambiguity.
func codexExtras(provider, sandbox string, network bool, networkSet bool,
	mode, effort, personality, timezone string) map[string]any {
	m := map[string]any{}
	addNonEmpty := func(k, v string) {
		if v != "" {
			m["codex."+k] = v
		}
	}
	addNonEmpty("model_provider", provider)
	addNonEmpty("sandbox_type", sandbox)
	addNonEmpty("collab_mode", mode)
	addNonEmpty("reasoning_effort", effort)
	addNonEmpty("personality", personality)
	addNonEmpty("timezone", timezone)
	if networkSet {
		m["codex.network_access"] = network
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// codexEventMsg discriminates the various event_msg subtypes via
// payload.type. We further-decode based on that discriminator.
type codexEventMsg struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Phase   string          `json:"phase"`
	Info    *codexUsageInfo `json:"info,omitempty"`
}

// codexUsageInfo wraps the token_count.info subobject. nil when the
// CLI hasn't aggregated usage yet (early-cancelled turns).
type codexUsageInfo struct {
	LastTokenUsage *codexUsage `json:"last_token_usage,omitempty"`
}

// codexUsage is OpenAI's per-turn token breakdown.
//
// `cached_input_tokens` is a subset of `input_tokens` (it's the cached
// portion served at a discount) — same accounting model as Anthropic's
// cache_read_input_tokens, so we map it onto our cache_read column for
// consistency in stats / grouping.
type codexUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

func (codex) Parse(_ context.Context, path string, emit func(Record) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)

	// Per-file state derived from the session_meta header / turn_context
	// updates. Each emitted Record is stamped with whatever values are
	// current at its line. The "extras" set is Codex-specific metadata
	// that gets folded into Record.Raw as JSON so it isn't lost — claude
	// transcripts have no equivalent so the importer's neutral Record
	// type doesn't carry dedicated fields for it.
	var (
		sessionID  string
		cwd        string
		cliVersion string

		originator    string // session_meta.originator   (Codex Desktop / CLI)
		source        string // session_meta.source       (vscode / terminal)
		modelProvider string // session_meta.model_provider

		currentModel      string
		approvalPolicy    string // turn_context.approval_policy
		personality       string
		timezone          string
		sandboxType       string
		networkAccess     bool
		networkAccessSet  bool // disambiguate "false because absent" from "false because set"
		collabMode        string
		reasoningEffort   string
	)

	// Buffered assistant record. Codex emits agent_message THEN a
	// matching token_count one envelope later — we hold the assistant
	// Record so the usage numbers can be attached before emit. Flushed
	// when the next user_message lands or the file ends.
	var pending *Record
	flush := func() error {
		if pending == nil {
			return nil
		}
		r := *pending
		pending = nil
		return emit(r)
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		// Snapshot line bytes — bufio reuses its buffer between reads,
		// and we hash + persist these bytes downstream.
		line := make([]byte, len(raw))
		copy(line, raw)

		var env codexEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, env.Timestamp)

		switch env.Type {
		case "session_meta":
			var m codexSessionMeta
			_ = json.Unmarshal(env.Payload, &m)
			if m.ID != "" {
				sessionID = m.ID
			}
			if m.Cwd != "" {
				cwd = m.Cwd
			}
			if m.CLIVersion != "" {
				cliVersion = m.CLIVersion
			}
			if m.Originator != "" {
				originator = m.Originator
			}
			if m.Source != "" {
				source = m.Source
			}
			if m.ModelProvider != "" {
				modelProvider = m.ModelProvider
			}

		case "turn_context":
			var tc codexTurnContext
			_ = json.Unmarshal(env.Payload, &tc)
			if tc.Cwd != "" {
				cwd = tc.Cwd
			}
			if tc.Model != "" {
				currentModel = tc.Model
			}
			if tc.ApprovalPolicy != "" {
				approvalPolicy = tc.ApprovalPolicy
			}
			if tc.Personality != "" {
				personality = tc.Personality
			}
			if tc.Timezone != "" {
				timezone = tc.Timezone
			}
			if tc.SandboxPolicy.Type != "" {
				sandboxType = tc.SandboxPolicy.Type
				networkAccess = tc.SandboxPolicy.NetworkAccess
				networkAccessSet = true
			}
			if tc.CollaborationMode.Mode != "" {
				collabMode = tc.CollaborationMode.Mode
			}
			if tc.CollaborationMode.Settings.ReasoningEffort != "" {
				reasoningEffort = tc.CollaborationMode.Settings.ReasoningEffort
			}

		case "event_msg":
			var ev codexEventMsg
			_ = json.Unmarshal(env.Payload, &ev)
			switch ev.Type {
			case "user_message":
				if err := flush(); err != nil {
					return err
				}
				if ev.Message == "" {
					continue
				}
				rec := Record{
					Tool:           "codex",
					ToolVersion:    cliVersion,
					SessionID:      sessionID,
					CWD:            cwd,
					Role:           RoleUser,
					Text:           ev.Message,
					Timestamp:      ts,
					LineHash:       hashLine(line),
					PermissionMode: approvalPolicy,
					Terminal:       originator,
					TerminalTitle:  source,
					Extras: codexExtras(modelProvider, sandboxType, networkAccess, networkAccessSet,
						collabMode, reasoningEffort, personality, timezone),
				}
				if err := emit(rec); err != nil {
					return err
				}

			case "agent_message":
				// Only treat the final-answer phase as a captured turn —
				// intermediate "thinking" agent_messages would otherwise
				// double-count and pollute the search index.
				if ev.Phase != "" && ev.Phase != "final_answer" {
					continue
				}
				if err := flush(); err != nil {
					return err
				}
				if ev.Message == "" {
					continue
				}
				pending = &Record{
					Tool:           "codex",
					ToolVersion:    cliVersion,
					SessionID:      sessionID,
					CWD:            cwd,
					Role:           RoleAssistant,
					Text:           ev.Message,
					Model:          currentModel,
					Timestamp:      ts,
					LineHash:       hashLine(line),
					PermissionMode: approvalPolicy,
					Terminal:       originator,
					TerminalTitle:  source,
					Extras: codexExtras(modelProvider, sandboxType, networkAccess, networkAccessSet,
						collabMode, reasoningEffort, personality, timezone),
				}

			case "token_count":
				// Attach to the buffered assistant record. No-op if no
				// pending record (token_count for a tool-only turn) or
				// if the CLI hasn't aggregated usage yet (Info nil).
				if pending == nil || ev.Info == nil || ev.Info.LastTokenUsage == nil {
					continue
				}
				u := ev.Info.LastTokenUsage
				pending.TokensIn = u.InputTokens
				// reasoning tokens are billed as output but tracked
				// separately — fold them in so totals match the OpenAI
				// invoice.
				pending.TokensOut = u.OutputTokens + u.ReasoningOutputTokens
				pending.TokensCacheRead = u.CachedInputTokens
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}
