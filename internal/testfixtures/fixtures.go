// Package testfixtures writes made-up agent homes (Claude Code, Codex,
// Gemini CLI, pi, opencode) into a temp user home for tests. Every value
// is invented (users alice and bob, /work/... folders); the shapes follow
// the vendor formats agentio reads. Nothing here is copied from a real
// transcript. Imported only by tests.
package testfixtures

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// SQLite driver for the opencode.db fixture.
	_ "modernc.org/sqlite"
)

// Fixed ids the tests assert on.
const (
	ClaudeSession    = "aaaaaaaa-1111-4111-8111-000000000001"
	ClaudeBobSession = "bbbbbbbb-2222-4222-8222-000000000002"
	CodexSession     = "11111111-2222-4333-8444-555555555555"
	GeminiSession    = "cccccccc-0000-4000-8000-0000000000a1"
	PiSession        = "dddddddd-0000-7000-8000-0000000000b1"
	OpencodeLegacy   = "ses_0f0000000001aliceAAAAAAA"
	OpencodeDB       = "ses_0f0000000002bobBBBBBBBBB"
)

// Isolate points every agent lookup at a fresh user home: HOME, and the
// agent home variables cleared so the real ~/.claude, ~/.codex, ~/.ailog
// are never read or written. Returns the user home.
func Isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GEMINI_CLI_HOME", "PI_CODING_AGENT_DIR",
		"XDG_DATA_HOME", "XDG_CONFIG_HOME", "OPENCODE_DB", "AILOG_CLAUDE_HOME"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	t.Setenv("AILOG_HOME", filepath.Join(home, ".ailog"))
	return home
}

// Write writes content at home/rel, creating parents.
func Write(t *testing.T, home, rel, content string) string {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Append appends content to the file at path.
func Append(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// lines joins JSONL records, each ending in a newline.
func lines(rec ...string) string { return strings.Join(rec, "\n") + "\n" }

// ClaudeLines is alice's Claude Code transcript: a prompt, a thinking-only
// assistant line with usage (no text), the answer, a tool_result user line
// (no text), and a second prompt.
func ClaudeLines() []string {
	return []string{
		`{"type":"user","uuid":"u-1","sessionId":"` + ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:00:00.000Z","version":"2.1.300","message":{"role":"user","content":"Explain goroutines briefly."}}`,
		`{"type":"assistant","uuid":"a-0","sessionId":"` + ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:00:01.000Z","version":"2.1.300","message":{"role":"assistant","model":"claude-test","content":[{"type":"thinking","thinking":"hmm"}],"usage":{"input_tokens":5,"output_tokens":1}}}`,
		`{"type":"assistant","uuid":"a-1","sessionId":"` + ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:00:02.000Z","version":"2.1.300","message":{"role":"assistant","model":"claude-test","stop_reason":"end_turn","content":[{"type":"text","text":"Lightweight threads."},{"type":"text","text":"Scheduled by the runtime."}],"usage":{"input_tokens":12,"output_tokens":6,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`,
		`{"type":"user","uuid":"u-tr","sessionId":"` + ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:00:03.000Z","version":"2.1.300","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"user","uuid":"u-2","sessionId":"` + ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:01:00.000Z","version":"2.1.300","message":{"role":"user","content":[{"type":"text","text":"And channels?"}]}}`,
	}
}

// ClaudeHome writes ~/.claude with alice's transcript and one subagent
// run, plus ~/.claude-bob with bob's one-turn transcript. Returns the
// path of alice's main transcript.
func ClaudeHome(t *testing.T, home string) string {
	t.Helper()
	Write(t, home, ".claude/settings.json", "{}\n")
	main := Write(t, home, ".claude/projects/-work-alice/"+ClaudeSession+".jsonl", lines(ClaudeLines()...))
	sub := ".claude/projects/-work-alice/" + ClaudeSession + "/subagents/agent-s1"
	Write(t, home, sub+".meta.json", `{"agentType":"general-purpose","description":"look around"}`+"\n")
	Write(t, home, sub+".jsonl", lines(
		`{"type":"user","uuid":"s-u1","isSidechain":true,"sessionId":"`+ClaudeSession+`","cwd":"/work/alice","timestamp":"2026-03-01T10:00:01.500Z","version":"2.1.300","message":{"role":"user","content":"Find the config file."}}`,
		`{"type":"assistant","uuid":"s-a1","isSidechain":true,"sessionId":"`+ClaudeSession+`","cwd":"/work/alice","timestamp":"2026-03-01T10:00:01.700Z","version":"2.1.300","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"It is config.yaml."}],"usage":{"input_tokens":4,"output_tokens":2}}}`,
	))
	Write(t, home, ".claude-bob/settings.json", "{}\n")
	Write(t, home, ".claude-bob/projects/-work-bob/"+ClaudeBobSession+".jsonl", lines(
		`{"type":"user","uuid":"b-u1","sessionId":"`+ClaudeBobSession+`","cwd":"/work/bob","timestamp":"2026-03-02T09:00:00.000Z","version":"2.1.300","message":{"role":"user","content":"Hello from bob."}}`,
		`{"type":"assistant","uuid":"b-a1","sessionId":"`+ClaudeBobSession+`","cwd":"/work/bob","timestamp":"2026-03-02T09:00:01.000Z","version":"2.1.300","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"Hi bob."}],"usage":{"input_tokens":7,"output_tokens":2}}}`,
	))
	return main
}

// CodexRollout is the rel path of a Codex rollout named for id.
func CodexRollout(id string) string {
	return ".codex/sessions/2026/02/01/rollout-2026-02-01T09-00-00-" + id + ".jsonl"
}

// CodexHome writes ~/.codex with the given rollouts (rollout id → JSONL
// content). Returns the rollout paths in argument order.
func CodexHome(t *testing.T, home string, rollouts map[string]string, order ...string) []string {
	t.Helper()
	Write(t, home, ".codex/config.toml", "")
	var out []string
	for _, id := range order {
		out = append(out, Write(t, home, CodexRollout(id), rollouts[id]))
	}
	return out
}

// CodexCurrent is a Codex 0.16x rollout in the item_completed format: a
// prompt, a commentary message with its own token_count, the final
// answer and its token_count.
func CodexCurrent() string {
	return lines(
		`{"timestamp":"2026-02-01T09:00:00.000Z","type":"session_meta","payload":{"id":"`+CodexSession+`","timestamp":"2026-02-01T09:00:00.000Z","cwd":"/work/alice","originator":"Codex Desktop","cli_version":"0.160.0","source":"vscode","model_provider":"openai"}}`,
		`{"timestamp":"2026-02-01T09:00:00.100Z","type":"turn_context","payload":{"cwd":"/work/alice","approval_policy":"on-request","sandbox_policy":{"type":"workspace-write","network_access":false},"model":"gpt-test-1","timezone":"UTC","collaboration_mode":{"mode":"default","settings":{"reasoning_effort":"medium"}}}}`,
		`{"timestamp":"2026-02-01T09:00:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"item-u1","content":[{"type":"text","text":"How do I list files?"}]}}}`,
		`{"timestamp":"2026-02-01T09:00:02.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"item-c1","phase":"commentary","content":[{"type":"Text","text":"Looking at the folder."}]}}}`,
		`{"timestamp":"2026-02-01T09:00:02.100Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":90,"cached_input_tokens":10,"output_tokens":5,"reasoning_output_tokens":0,"total_tokens":95}}}}`,
		`{"timestamp":"2026-02-01T09:00:03.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"item-a1","phase":"final_answer","content":[{"type":"Text","text":"Use ls."}]}}}`,
		`{"timestamp":"2026-02-01T09:00:03.100Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":7,"total_tokens":227}}}}`,
	)
}

// CodexLegacyID is the id of the CodexLegacy rollout.
const CodexLegacyID = "22222222-3333-4444-8555-666666666666"

// CodexLegacy is a Codex 0.118 rollout: user_message / agent_message
// events, each answer followed by its token_count.
func CodexLegacy() string {
	return lines(
		`{"timestamp":"2025-12-01T08:00:00.000Z","type":"session_meta","payload":{"id":"`+CodexLegacyID+`","timestamp":"2025-12-01T08:00:00.000Z","cwd":"/work/bob","cli_version":"0.118.0"}}`,
		`{"timestamp":"2025-12-01T08:00:00.100Z","type":"turn_context","payload":{"cwd":"/work/bob","approval_policy":"never","sandbox_policy":{"type":"read-only"},"model":"gpt-test-0"}}`,
		`{"timestamp":"2025-12-01T08:00:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"What is two plus two?"}}`,
		`{"timestamp":"2025-12-01T08:00:02.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Four."}}`,
		`{"timestamp":"2025-12-01T08:00:02.100Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":50,"cached_input_tokens":0,"output_tokens":3,"reasoning_output_tokens":0,"total_tokens":53}}}}`,
		`{"timestamp":"2025-12-01T08:00:03.000Z","type":"event_msg","payload":{"type":"user_message","message":"Thanks"}}`,
		`{"timestamp":"2025-12-01T08:00:04.000Z","type":"event_msg","payload":{"type":"agent_message","message":"You are welcome."}}`,
		`{"timestamp":"2025-12-01T08:00:04.100Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":60,"cached_input_tokens":0,"output_tokens":4,"reasoning_output_tokens":0,"total_tokens":64}}}}`,
	)
}

// GeminiHome writes ~/.gemini with one chat of alice's: a prompt, an
// answer first written without tokens and re-appended with them.
func GeminiHome(t *testing.T, home string) string {
	t.Helper()
	Write(t, home, ".gemini/settings.json", "{}\n")
	Write(t, home, ".gemini/tmp/alice-proj/.project_root", "/work/alice\n")
	return Write(t, home, ".gemini/tmp/alice-proj/chats/session-2026-03-03T08-00-cccccccc.jsonl", lines(
		`{"sessionId":"`+GeminiSession+`","projectHash":"0000","startTime":"2026-03-03T08:00:00.000Z","lastUpdated":"2026-03-03T08:00:00.000Z","kind":"main"}`,
		`{"id":"g-u1","timestamp":"2026-03-03T08:00:01.000Z","type":"user","content":[{"text":"Summarise the README."}]}`,
		`{"id":"g-m1","timestamp":"2026-03-03T08:00:03.000Z","type":"gemini","content":"It explains setup.","model":"gemini-test"}`,
		`{"id":"g-m1","timestamp":"2026-03-03T08:00:03.000Z","type":"gemini","content":"It explains setup.","tokens":{"input":300,"output":8,"cached":100,"thoughts":2,"tool":0,"total":310},"model":"gemini-test"}`,
		`{"":{"lastUpdated":"2026-03-03T08:00:04.000Z"}}`,
	))
}

// PiHome writes ~/.pi/agent with one session of bob's.
func PiHome(t *testing.T, home string) string {
	t.Helper()
	Write(t, home, ".pi/agent/settings.json", "{}\n")
	return Write(t, home, ".pi/agent/sessions/--work-bob--/2026-03-04T10-00-00-000Z_"+PiSession+".jsonl", lines(
		`{"type":"session","version":3,"id":"`+PiSession+`","timestamp":"2026-03-04T10:00:00.000Z","cwd":"/work/bob"}`,
		`{"type":"model_change","id":"e1","parentId":null,"timestamp":"2026-03-04T10:00:00.100Z","provider":"prov","modelId":"pi-model"}`,
		`{"type":"message","id":"e2","parentId":"e1","timestamp":"2026-03-04T10:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"List the tests."}],"timestamp":1772618401000}}`,
		`{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-03-04T10:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"There are three."}],"api":"openai-completions","provider":"prov","model":"pi-model","usage":{"input":40,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":45},"stopReason":"stop","timestamp":1772618403000}}`,
		`{"type":"session_info","id":"e4","parentId":"e3","timestamp":"2026-03-04T10:00:04.000Z","name":"bob tests"}`,
	))
}

// opencodeData is the opencode data dir under a user home.
const opencodeData = ".local/share/opencode"

// OpencodeHome writes the opencode data dir: alice's legacy storage
// session (1.1.x files) and bob's session in opencode.db.
func OpencodeHome(t *testing.T, home string) {
	t.Helper()
	st := opencodeData + "/storage"
	Write(t, home, st+"/session/global/"+OpencodeLegacy+".json",
		`{"id":"`+OpencodeLegacy+`","version":"1.1.14","projectID":"global","directory":"/work/alice","title":"Alice legacy","time":{"created":1772700000000,"updated":1772700005000}}`)
	Write(t, home, st+"/message/"+OpencodeLegacy+"/msg_0f0000000001aliceAAAAAA01.json",
		`{"id":"msg_0f0000000001aliceAAAAAA01","sessionID":"`+OpencodeLegacy+`","role":"user","time":{"created":1772700001000},"agent":"build","model":{"providerID":"prov","modelID":"oc-model"}}`)
	Write(t, home, st+"/part/msg_0f0000000001aliceAAAAAA01/prt_0f0000000001aliceAAAAAA01.json",
		`{"id":"prt_0f0000000001aliceAAAAAA01","messageID":"msg_0f0000000001aliceAAAAAA01","sessionID":"`+OpencodeLegacy+`","type":"text","text":"Rename the module."}`)
	Write(t, home, st+"/message/"+OpencodeLegacy+"/msg_0f0000000002aliceAAAAAA02.json",
		`{"id":"msg_0f0000000002aliceAAAAAA02","sessionID":"`+OpencodeLegacy+`","role":"assistant","time":{"created":1772700002000,"completed":1772700004000},"parentID":"msg_0f0000000001aliceAAAAAA01","modelID":"oc-model","providerID":"prov","mode":"build","agent":"build","path":{"cwd":"/work/alice","root":"/work/alice"},"cost":0.25,"tokens":{"input":30,"output":6,"reasoning":2,"cache":{"read":4,"write":1}}}`)
	Write(t, home, st+"/part/msg_0f0000000002aliceAAAAAA02/prt_0f0000000002aliceAAAAAA01.json",
		`{"id":"prt_0f0000000002aliceAAAAAA01","messageID":"msg_0f0000000002aliceAAAAAA02","sessionID":"`+OpencodeLegacy+`","type":"text","text":"Renamed it."}`)

	dbPath := filepath.Join(home, opencodeData, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, stmt := range []string{
		`CREATE TABLE project (id text PRIMARY KEY, worktree text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, sandboxes text NOT NULL)`,
		`CREATE TABLE session (id text PRIMARY KEY, project_id text NOT NULL, parent_id text, slug text NOT NULL, directory text NOT NULL, title text NOT NULL, version text NOT NULL, share_url text, time_created integer NOT NULL, time_updated integer NOT NULL, time_archived integer)`,
		`CREATE TABLE message (id text PRIMARY KEY, session_id text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`,
		`CREATE TABLE part (id text PRIMARY KEY, message_id text NOT NULL, session_id text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`,
		`INSERT INTO project VALUES ('proj-bob', '/work/bob', 1, 2, '[]')`,
		fmt.Sprintf(`INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated) VALUES ('%s', 'proj-bob', 'bob', '/work/bob', 'Bob db', '1.18.30', 1772800000000, 1772800009000)`, OpencodeDB),
		fmt.Sprintf(`INSERT INTO message VALUES ('msg_b1', '%s', 1772800001000, 1772800001000, '{"role":"user","time":{"created":1772800001000},"agent":"build","model":{"providerID":"prov","modelID":"oc-model"}}')`, OpencodeDB),
		fmt.Sprintf(`INSERT INTO message VALUES ('msg_b2', '%s', 1772800002000, 1772800003000, '{"role":"assistant","time":{"created":1772800002000,"completed":1772800003000},"parentID":"msg_b1","modelID":"oc-model","providerID":"prov","mode":"build","agent":"build","cost":0,"tokens":{"input":11,"output":3,"reasoning":0,"cache":{"read":0,"write":0}},"finish":"stop"}')`, OpencodeDB),
		fmt.Sprintf(`INSERT INTO part VALUES ('prt_b1', 'msg_b1', '%s', 1772800001000, 1772800001000, '{"type":"text","text":"Add a test."}')`, OpencodeDB),
		fmt.Sprintf(`INSERT INTO part VALUES ('prt_b2', 'msg_b2', '%s', 1772800003000, 1772800003000, '{"type":"text","text":"Added one."}')`, OpencodeDB),
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("opencode fixture: %v\n%s", err, stmt)
		}
	}
}
