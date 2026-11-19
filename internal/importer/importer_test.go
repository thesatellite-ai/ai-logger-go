package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
)

// All fixtures under testdata/ are made-up content in the real wire
// shapes; none is copied from a real transcript.

func parseAll(t *testing.T, src Source, path string) []Record {
	t.Helper()
	var out []Record
	if err := src.Parse(context.Background(), path, func(r Record) error {
		out = append(out, r)
		return nil
	}); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func texts(recs []Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = string(r.Role) + ":" + r.Text
	}
	return out
}

func TestCodex_CurrentFormat(t *testing.T) {
	recs := parseAll(t, codex{}, "testdata/codex/current.jsonl")
	want := []string{
		"user:How do I list files?",
		"assistant:Use ls.\nAdd -la for details.",
		"user:continue",
		"assistant:Done.",
		"user:continue", // a repeated prompt in the same encoding is a new turn
	}
	if got := texts(recs); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("records:\n got %q\nwant %q", got, want)
	}
	first, answer := recs[0], recs[1]
	if first.SessionID != "11111111-2222-4333-8444-555555555555" || first.CWD != "/work/demo" ||
		first.ToolVersion != "0.169.0-alpha.11.1" || first.PermissionMode != "on-request" ||
		first.Terminal != "Codex Desktop" || first.TerminalTitle != "vscode" {
		t.Fatalf("user record metadata: %+v", first)
	}
	if answer.Model != "gpt-test-1" {
		t.Fatalf("model = %q", answer.Model)
	}
	// The token_count after the final answer wins (not the one after
	// the commentary message), with reasoning folded into output.
	if answer.TokensIn != 200 || answer.TokensOut != 27 || answer.TokensCacheRead != 80 {
		t.Fatalf("tokens = in %d out %d cache %d", answer.TokensIn, answer.TokensOut, answer.TokensCacheRead)
	}
	if answer.Extras["codex.reasoning_effort"] != "medium" || answer.Extras["codex.sandbox_type"] != "workspace-write" {
		t.Fatalf("extras = %v", answer.Extras)
	}
	if !(codex{}).Anchor(answer) {
		t.Fatal("current-format answer fails the drift anchor")
	}
	if versionCmp(first.ToolVersion, (codex{}).LastKnownVersion()) > 0 {
		t.Fatalf("fixture version %s is newer than LastKnownVersion %s", first.ToolVersion, (codex{}).LastKnownVersion())
	}
}

func TestCodex_LegacyFormat(t *testing.T) {
	recs := parseAll(t, codex{}, "testdata/codex/legacy.jsonl")
	want := []string{
		"user:What is two plus two?",
		"assistant:Four.",
		"user:Thanks",
		"assistant:You are welcome.",
	}
	if got := texts(recs); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("records:\n got %q\nwant %q", got, want)
	}
	if recs[1].TokensIn != 50 || recs[1].TokensOut != 3 || recs[1].Model != "gpt-test-0" {
		t.Fatalf("legacy answer: %+v", recs[1])
	}
	if recs[0].PermissionMode != "never" || recs[0].Extras["codex.network_access"] != false {
		t.Fatalf("legacy turn context: %+v", recs[0])
	}
}

func TestCodex_MixedEncodingsKeptOnce(t *testing.T) {
	recs := parseAll(t, codex{}, "testdata/codex/mixed.jsonl")
	want := []string{"user:Rename the file.", "assistant:Renamed."}
	if got := texts(recs); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("records:\n got %q\nwant %q", got, want)
	}
	if recs[1].TokensIn != 70 {
		t.Fatalf("tokens lost on the deduplicated answer: %+v", recs[1])
	}
}

func TestCodex_DiscoverSkipsIndex(t *testing.T) {
	files, err := codex{}.Discover(context.Background(), "testdata/codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %v", files)
	}
	for _, f := range files {
		if filepath.Base(f) == "session_index.jsonl" {
			t.Fatal("session_index.jsonl was discovered")
		}
	}
}

func TestCodex_EmitErrorStopsParse(t *testing.T) {
	stop := errors.New("stop")
	n := 0
	err := codex{}.Parse(context.Background(), "testdata/codex/current.jsonl", func(Record) error {
		n++
		return stop
	})
	if !errors.Is(err, stop) || n != 1 {
		t.Fatalf("err %v after %d records", err, n)
	}
}

func TestClaudeCode_Parse(t *testing.T) {
	recs := parseAll(t, claudeCode{}, "testdata/claude/session.jsonl")
	want := []string{
		"user:Explain goroutines briefly.",
		"assistant:Lightweight threads.\nScheduled by the runtime.",
		"user:And channels?",
	}
	if got := texts(recs); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("records:\n got %q\nwant %q", got, want)
	}
	a := recs[1]
	if a.Model != "claude-test" || a.StopReason != "end_turn" || a.TokensIn != 12 || a.TokensOut != 6 ||
		a.TokensCacheRead != 3 || a.TokensCacheWrite != 2 || a.ToolVersion != "2.1.100" {
		t.Fatalf("assistant: %+v", a)
	}
	if recs[0].LineHash == "" || recs[0].LineHash == recs[2].LineHash {
		t.Fatal("line hashes missing or not unique")
	}
}

// writeJSON writes v as JSON at path, creating parents.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCode_Parse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	sess := filepath.Join(root, "session", "global", "ses_demo.json")
	writeJSON(t, sess, map[string]any{"id": "ses_demo", "version": "1.1.0", "directory": "/work/oc", "title": "Demo"})
	writeJSON(t, filepath.Join(root, "message", "ses_demo", "msg_b.json"), map[string]any{
		"id": "msg_b", "role": "assistant", "modelID": "m-1", "providerID": "p-1",
		"tokens": map[string]any{"input": 9, "output": 4, "reasoning": 2, "cache": map[string]int{"read": 1, "write": 0}},
		"time":   map[string]int64{"created": 2000},
	})
	writeJSON(t, filepath.Join(root, "message", "ses_demo", "msg_a.json"), map[string]any{
		"id": "msg_a", "role": "user", "time": map[string]int64{"created": 1000},
	})
	writeJSON(t, filepath.Join(root, "part", "msg_a", "prt_1.json"), map[string]string{"type": "text", "text": "Hello there"})
	writeJSON(t, filepath.Join(root, "part", "msg_b", "prt_1.json"), map[string]string{"type": "reasoning", "text": "hidden"})
	writeJSON(t, filepath.Join(root, "part", "msg_b", "prt_2.json"), map[string]string{"type": "text", "text": "Hi"})

	files, err := opencode{}.Discover(context.Background(), root)
	if err != nil || len(files) != 1 {
		t.Fatalf("discover = %v, %v", files, err)
	}
	recs := parseAll(t, opencode{}, files[0])
	if got := strings.Join(texts(recs), "|"); got != "user:Hello there|assistant:Hi" {
		t.Fatalf("records = %q", got)
	}
	if recs[1].TokensOut != 6 || recs[1].Model != "m-1" || recs[0].SessionName != "Demo" {
		t.Fatalf("assistant: %+v", recs[1])
	}
}

func TestVersionCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.169.0", "0.118.0", 1},
		{"0.118.0-alpha.2", "0.118.0", -1},
		{"0.118.0", "0.118.0", 0},
		{"2.1.9", "2.1.114", -1},
		{"1.0", "", 1},
	}
	for _, c := range cases {
		if got := versionCmp(c.a, c.b); sign(got) != c.want {
			t.Errorf("versionCmp(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func TestRegistry(t *testing.T) {
	names := strings.Join(Names(), ",")
	if names != "claude-code,codex,opencode" {
		t.Fatalf("names = %s", names)
	}
	if _, ok := Lookup("codex"); !ok {
		t.Fatal("codex not registered")
	}
	if len(All()) != 3 {
		t.Fatal("All() size")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	Register(codex{})
}

// TestRun_CodexEndToEnd imports the codex fixtures into a real store,
// then re-runs to prove the import is idempotent.
func TestRun_CodexEndToEnd(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	root := t.TempDir()
	for _, name := range []string{"current.jsonl", "legacy.jsonl"} {
		b, err := os.ReadFile(filepath.Join("testdata", "codex", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	stats, err := Run(ctx, st, codex{}, Options{Root: root, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	// current: 3 prompts, 2 answers; legacy: 2 prompts, 2 answers.
	if stats.Inserted != 5 || stats.Attached != 4 || stats.Standalone != 0 || stats.FilesSuspect != 0 {
		t.Fatalf("stats = %+v\n%s", stats, out.String())
	}
	entries, err := st.SessionEntries(ctx, "11111111-2222-4333-8444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Prompt != "How do I list files?" || !strings.HasPrefix(entries[0].Response, "Use ls.") {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Project != "demo" {
		t.Fatalf("project fallback = %q", entries[0].Project)
	}

	again, err := Run(ctx, st, codex{}, Options{Root: root, Out: &out, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if again.Inserted != 0 || again.Attached != 0 || again.RecordsSkipped != 9 {
		t.Fatalf("second run not idempotent: %+v", again)
	}
	skipped, err := Run(ctx, st, codex{}, Options{Root: root, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.FilesSkipped != 2 {
		t.Fatalf("watermark did not skip unchanged files: %+v", skipped)
	}
}

func TestRun_SinceAndLimit(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := Run(ctx, st, codex{}, Options{Root: "testdata/codex", Since: since, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	// legacy.jsonl (2025) is filtered out by --since.
	if stats.Inserted != 4 { // current 3 + mixed 1
		t.Fatalf("since: %+v", stats)
	}

	st2, err := store.Open(ctx, filepath.Join(t.TempDir(), "log2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	limited, err := Run(ctx, st2, codex{}, Options{Root: "testdata/codex", Limit: 1, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if limited.Inserted+limited.Standalone != 1 {
		t.Fatalf("limit: %+v", limited)
	}
}

func TestEncodeRaw(t *testing.T) {
	if got := encodeRaw(Record{LineHash: "abc"}); got != "abc" {
		t.Fatalf("bare = %s", got)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(encodeRaw(Record{LineHash: "abc", Extras: map[string]any{"codex.x": "y"}})), &m); err != nil {
		t.Fatal(err)
	}
	if m["line_hash"] != "abc" || m["codex.x"] != "y" {
		t.Fatalf("raw = %v", m)
	}
}
