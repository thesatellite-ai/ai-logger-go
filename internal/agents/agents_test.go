package agents_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
	tf "github.com/khanakia/ai-logger/internal/testfixtures"
)

// allHomes writes a home of every agent under a fresh user home.
func allHomes(t *testing.T) string {
	t.Helper()
	home := tf.Isolate(t)
	tf.ClaudeHome(t, home)
	paths := tf.CodexHome(t, home, map[string]string{tf.CodexSession: tf.CodexCurrent(), tf.CodexLegacyID: tf.CodexLegacy()},
		tf.CodexSession, tf.CodexLegacyID)
	// The legacy rollout is the older one, as on a real machine.
	old := time.Date(2025, 12, 1, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(paths[1], old, old); err != nil {
		t.Fatal(err)
	}
	tf.GeminiHome(t, home)
	tf.PiHome(t, home)
	tf.OpencodeHome(t, home)
	return home
}

func registry(t *testing.T) agents.Registry {
	t.Helper()
	reg, err := agents.FromOS()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// turnLine renders a turn for comparison: role, interim mark, text.
func turnLine(tu agentio.Turn) string {
	mark := ""
	if tu.Interim {
		mark = "~"
	}
	return tu.Role.String() + mark + ":" + tu.Text
}

// readAll lists every transcript of every home of a and reads its turns.
func readAll(t *testing.T, a agents.Agent) (keys []string, turns []string) {
	t.Helper()
	hs, err := a.Homes()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hs {
		ts, err := a.Transcripts(context.Background(), h)
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range ts {
			keys = append(keys, tr.Key)
			if _, err := tr.Turns(0, func(tu agentio.Turn) error {
				turns = append(turns, turnLine(tu))
				return nil
			}); err != nil {
				t.Fatalf("%s: %v", tr.Key, err)
			}
		}
	}
	return keys, turns
}

func TestRegistry_ToolsAndHooks(t *testing.T) {
	tf.Isolate(t)
	reg := registry(t)
	if !slices.Equal(reg.Tools(), agents.ToolNames()) {
		t.Fatalf("Tools %v != ToolNames %v", reg.Tools(), agents.ToolNames())
	}
	var hooked []string
	for _, a := range reg.WithHooks() {
		hooked = append(hooked, a.Tool)
	}
	if want := []string{agents.ToolClaude, agents.ToolCodex, agents.ToolGemini}; !slices.Equal(hooked, want) {
		t.Fatalf("WithHooks = %v, want %v", hooked, want)
	}
	if _, ok := reg.ByTool("cursor"); ok {
		t.Fatal("unknown tool found")
	}
	for _, a := range reg.All() {
		if a.TestedMax == "" {
			t.Errorf("%s has no TestedMax (drift watchdog off)", a.Tool)
		}
		if _, ok := agentio.TurnFilesOf(a.Neutral); !ok {
			t.Errorf("%s has no path-based turn reader", a.Tool)
		}
	}
}

func TestTranscripts_EveryAgentAndHome(t *testing.T) {
	home := allHomes(t)
	reg := registry(t)
	cases := []struct {
		tool  string
		files int
		turns []string
	}{
		{agents.ToolClaude, 3, []string{
			"user:Explain goroutines briefly.", "assistant:", "assistant:Lightweight threads.\nScheduled by the runtime.", "user:And channels?",
			"user:Find the config file.", "assistant:It is config.yaml.", // subagent run
			"user:Hello from bob.", "assistant:Hi bob.", // ~/.claude-bob
		}},
		{agents.ToolCodex, 2, []string{
			"user:What is two plus two?", "assistant:Four.", "user:Thanks", "assistant:You are welcome.",
			"user:How do I list files?", "assistant~:Looking at the folder.", "assistant:Use ls.",
		}},
		{agents.ToolGemini, 1, []string{"user:Summarise the README.", "assistant:It explains setup.", "assistant:It explains setup."}},
		{agents.ToolPi, 1, []string{"user:List the tests.", "assistant:There are three."}},
		{agents.ToolOpencode, 2, []string{"user:Rename the module.", "assistant:Renamed it.", "user:Add a test.", "assistant:Added one."}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			a, ok := reg.ByTool(c.tool)
			if !ok {
				t.Fatal("not registered")
			}
			keys, turns := readAll(t, a)
			if len(keys) != c.files {
				t.Fatalf("transcripts = %v", keys)
			}
			for _, k := range keys {
				if !strings.HasPrefix(k, home) {
					t.Fatalf("transcript %s outside the temp home", k)
				}
			}
			if strings.Join(turns, "|") != strings.Join(c.turns, "|") {
				t.Fatalf("turns:\n got %q\nwant %q", turns, c.turns)
			}
		})
	}
}

func TestTranscripts_OpencodeKeysAndOrder(t *testing.T) {
	home := allHomes(t)
	a, _ := registry(t).ByTool(agents.ToolOpencode)
	hs, err := a.Homes()
	if err != nil || len(hs) != 1 {
		t.Fatalf("homes %v %v", hs, err)
	}
	ts, err := a.Transcripts(context.Background(), hs[0])
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, ".local/share/opencode/opencode.db") + "#" + tf.OpencodeDB
	if ts[1].Key != db || ts[1].Path != "" || ts[1].Title != "Bob db" {
		t.Fatalf("db transcript = %+v", ts[1])
	}
	if ts[0].Path == "" || ts[0].Key != ts[0].Path || ts[0].Title != "Alice legacy" {
		t.Fatalf("legacy transcript = %+v", ts[0])
	}
	if !ts[0].Updated.Before(ts[1].Updated) {
		t.Fatal("transcripts not oldest first")
	}
}

func TestTranscripts_OpencodeWithoutDatabase(t *testing.T) {
	home := allHomes(t)
	if err := os.Remove(filepath.Join(home, ".local/share/opencode/opencode.db")); err != nil {
		t.Fatal(err)
	}
	a, _ := registry(t).ByTool(agents.ToolOpencode)
	keys, _ := readAll(t, a)
	if len(keys) != 1 {
		t.Fatalf("keys = %v", keys)
	}
}

func TestClaudeHomeEnv_OnlyThatHome(t *testing.T) {
	home := allHomes(t)
	t.Setenv(agents.ClaudeHomeEnv, filepath.Join(home, ".claude-bob"))
	a, _ := registry(t).ByTool(agents.ToolClaude)
	hs, err := a.Homes()
	if err != nil || len(hs) != 1 || hs[0].Dir() != filepath.Join(home, ".claude-bob") {
		t.Fatalf("homes = %v, %v", hs, err)
	}
	_, turns := readAll(t, a)
	if strings.Join(turns, "|") != "user:Hello from bob.|assistant:Hi bob." {
		t.Fatalf("turns = %q", turns)
	}
}

func TestClaude_SubagentMetaWithoutTranscriptSkipped(t *testing.T) {
	home := allHomes(t)
	tf.Write(t, home, ".claude/projects/-work-alice/"+tf.ClaudeSession+"/subagents/agent-gone.meta.json", `{"agentType":"x"}`)
	a, _ := registry(t).ByTool(agents.ToolClaude)
	keys, _ := readAll(t, a)
	for _, k := range keys {
		if strings.Contains(k, "agent-gone") {
			t.Fatal("a subagent run without a transcript was listed")
		}
	}
}

func TestCodexAnnotator(t *testing.T) {
	allHomes(t)
	a, _ := registry(t).ByTool(agents.ToolCodex)
	hs, _ := a.Homes()
	ts, err := a.Transcripts(context.Background(), hs[0])
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]agents.Transcript{}
	for _, tr := range ts {
		byID[tr.ConversationID] = tr
	}
	cur := byID[tf.CodexSession].Annotator()
	got := cur(agentio.Turn{Time: time.Date(2026, 2, 1, 9, 0, 3, 0, time.UTC)})
	if got.PermissionMode != "on-request" || got.Terminal != "Codex Desktop" || got.TerminalTitle != "vscode" {
		t.Fatalf("columns = %+v", got)
	}
	want := map[string]any{
		"codex.model_provider": "openai", "codex.sandbox_type": "workspace-write", "codex.collab_mode": "default",
		"codex.reasoning_effort": "medium", "codex.timezone": "UTC", "codex.network_access": false,
	}
	for k, v := range want {
		if got.Extras[k] != v {
			t.Errorf("%s = %v, want %v", k, got.Extras[k], v)
		}
	}
	// A turn stamped before the first record takes the header state.
	early := cur(agentio.Turn{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
	if early.Terminal != "Codex Desktop" {
		t.Fatalf("early turn = %+v", early)
	}
	legacy := byID[tf.CodexLegacyID].Annotator()(agentio.Turn{Time: time.Date(2025, 12, 1, 8, 0, 2, 0, time.UTC)})
	if legacy.PermissionMode != "never" || legacy.Terminal != "" || legacy.Extras["codex.sandbox_type"] != "read-only" {
		t.Fatalf("legacy = %+v", legacy)
	}
	if _, has := legacy.Extras["codex.model_provider"]; has {
		t.Fatal("absent provider was stored")
	}
}

func TestCodexAnnotator_UnreadableRollout(t *testing.T) {
	home := tf.Isolate(t)
	p := tf.CodexHome(t, home, map[string]string{tf.CodexSession: tf.CodexCurrent()}, tf.CodexSession)[0]
	a, _ := registry(t).ByTool(agents.ToolCodex)
	hs, _ := a.Homes()
	ts, _ := a.Transcripts(context.Background(), hs[0])
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if got := ts[0].Annotator()(agentio.Turn{}); got.Terminal != "" || got.Extras != nil {
		t.Fatalf("annotation of a missing rollout = %+v", got)
	}
}

func TestOwnerAndTurnsAt(t *testing.T) {
	home := allHomes(t)
	reg := registry(t)
	claude, _ := reg.ByTool(agents.ToolClaude)
	main := filepath.Join(home, ".claude/projects/-work-alice/"+tf.ClaudeSession+".jsonl")
	ref, err := claude.Owner(main)
	if err != nil || ref.ConversationID != tf.ClaudeSession || ref.Home.Dir() != filepath.Join(home, ".claude") {
		t.Fatalf("owner = %+v, %v", ref, err)
	}
	codex, _ := reg.ByTool(agents.ToolCodex)
	if _, err := codex.Owner(filepath.Join(home, "not-a-rollout.txt")); !errors.Is(err, agentio.ErrNotFound) {
		t.Fatalf("owner of a foreign path: %v", err)
	}
	var n int
	next, err := claude.TurnsAt(main, 0, func(agentio.Turn) error { n++; return nil })
	if err != nil || n != 4 || next == 0 {
		t.Fatalf("TurnsAt: %d turns, next %d, %v", n, next, err)
	}
	again := 0
	if _, err := claude.TurnsAt(main, next, func(agentio.Turn) error { again++; return nil }); err != nil || again != 0 {
		t.Fatalf("reading from the end gave %d turns, %v", again, err)
	}
}

func TestOpencodeAnnotator(t *testing.T) {
	allHomes(t)
	a, _ := registry(t).ByTool(agents.ToolOpencode)
	hs, _ := a.Homes()
	ts, _ := a.Transcripts(context.Background(), hs[0])
	ann := ts[0].Annotator()
	var got []agents.Annotation
	if _, err := ts[0].Turns(0, func(tu agentio.Turn) error { got = append(got, ann(tu)); return nil }); err != nil {
		t.Fatal(err)
	}
	user, answer := got[0], got[1]
	if user.Model != "" || user.Extras["opencode.agent"] != "build" || user.Extras["opencode.project_id"] != "global" {
		t.Fatalf("user = %+v", user)
	}
	if answer.Model != "oc-model" || answer.Extras["opencode.cost_usd"] != 0.25 || answer.Extras["opencode.mode"] != "build" {
		t.Fatalf("answer = %+v", answer)
	}
	if bad := ann(agentio.Turn{Raw: agentio.Raw("[")}); bad.Model != "" || bad.Extras["opencode.project_id"] != "global" {
		t.Fatalf("undecodable message = %+v", bad)
	}
}
