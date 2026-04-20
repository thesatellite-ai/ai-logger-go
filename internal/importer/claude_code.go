package importer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// claudeCode parses Claude Code's per-session JSONL transcripts found
// under ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl.
//
// Each line is one envelope with at least:
//   {"type":"user"|"assistant", "sessionId":"…", "cwd":"…",
//    "timestamp":"2025-…Z", "version":"2.1.114",
//    "message":{...}}
//
// `message.content` is either a string or a typed-block array. For
// assistant lines we extract `message.usage` (Anthropic-only token
// breakdown), `message.model`, and `message.stop_reason`. Lines we
// don't recognize are skipped silently — unknown types are normal as
// the schema evolves.
type claudeCode struct{}

func init() { Register(claudeCode{}) }

func (claudeCode) Name() string        { return "claude-code" }
func (claudeCode) DefaultRoot() string { return expandHome("~/.claude/projects") }

// LastKnownVersion is the highest Claude Code release whose transcript
// shape matches what this parser reads. Bump when verifying a newer
// release; the driver uses it to gate the drift watchdog so warnings
// only escalate to errors when Claude has actually shipped a new
// version we haven't validated.
func (claudeCode) LastKnownVersion() string { return "2.1.114" }

// Anchor: assistant turns must carry input tokens. Anthropic has
// emitted message.usage.input_tokens on every assistant line for the
// life of Claude Code; if this stops being true, usage.* was renamed.
func (claudeCode) Anchor(r Record) bool {
	return r.Role == RoleAssistant && r.TokensIn > 0
}

func (claudeCode) Discover(_ context.Context, root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(p) == ".jsonl" {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// claudeLine mirrors the envelope fields we actually use. Anything not
// listed here is ignored on parse, so Claude can grow new fields
// without breaking import.
type claudeLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Cwd       string          `json:"cwd"`
	Timestamp string          `json:"timestamp"`
	Version   string          `json:"version"`
	Message   json.RawMessage `json:"message"`
}

// claudeUsage is the `message.usage` shape on assistant lines. All
// fields default to 0 when absent.
type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// claudeMessage is the inner `message` object. Content is a json.RawMessage
// because it can be a string or a typed-block array.
type claudeMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Usage      claudeUsage     `json:"usage"`
}

func (claudeCode) Parse(_ context.Context, path string, emit func(Record) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		// Snapshot the line bytes — bufio reuses its buffer between
		// Scan calls, so anything we keep across the loop must be copied.
		line := make([]byte, len(raw))
		copy(line, raw)

		var env claudeLine
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Type != "user" && env.Type != "assistant" {
			continue
		}

		var inner claudeMessage
		if len(env.Message) > 0 {
			_ = json.Unmarshal(env.Message, &inner)
		}
		text := flattenContent(inner.Content)
		if text == "" {
			continue
		}

		ts, _ := time.Parse(time.RFC3339Nano, env.Timestamp)

		rec := Record{
			Tool:        "claude-code",
			ToolVersion: env.Version,
			SessionID:   env.SessionID,
			CWD:         env.Cwd,
			Text:        text,
			Timestamp:   ts,
			LineHash:    hashLine(line),
		}
		switch env.Type {
		case "user":
			rec.Role = RoleUser
		case "assistant":
			rec.Role = RoleAssistant
			rec.Model = inner.Model
			rec.StopReason = inner.StopReason
			rec.TokensIn = inner.Usage.InputTokens
			rec.TokensOut = inner.Usage.OutputTokens
			rec.TokensCacheRead = inner.Usage.CacheReadInputTokens
			rec.TokensCacheWrite = inner.Usage.CacheCreationInputTokens
		}

		if err := emit(rec); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// flattenContent handles both shapes seen in Claude transcripts:
//
//   - Plain string: "what is foo"
//   - Block array: [{"type":"text","text":"…"}, {"type":"thinking",…}, …]
//
// Returns the concatenation of all "text" blocks, newline-joined; ""
// when no text content is present (e.g. a tool_use-only assistant turn).
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var out string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if out != "" {
					out += "\n"
				}
				out += b.Text
			}
		}
		return out
	}
	return ""
}

// hashLine is the SHA-256 hex of one transcript line — the per-line
// idempotency key stored in the import_lines table.
func hashLine(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
