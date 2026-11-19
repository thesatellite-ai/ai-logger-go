package importer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/khanakia/ai-logger/internal/agents"
	"github.com/khanakia/ai-logger/internal/store"
	tf "github.com/khanakia/ai-logger/internal/testfixtures"
)

// ist is the zone the live hook stamps rows in on the owner's machine:
// hook rows and imported rows (UTC) must compare as instants.
var ist = time.FixedZone("IST", 5*3600+1800)

// hookRow writes what the live prompt hook (and, with resp, its Stop
// hook) stores: capture time, terminal, the transcript path as raw.
// testHost is the hostname a live hook records on every row it writes.
const testHost = "mac"

func hookRow(t *testing.T, e *env, session, prompt, resp string, at time.Time) string {
	t.Helper()
	ctx := context.Background()
	local := at.In(ist)
	id, err := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: session, Prompt: prompt,
		Terminal: "ghostty", PermissionMode: "plan", Raw: "/transcripts/" + session + ".jsonl", Hostname: testHost, CreatedAt: &local})
	if err != nil {
		t.Fatal(err)
	}
	if resp != "" {
		if err := e.st.AttachResponse(ctx, store.AttachResponseInput{EntryID: id, Response: resp}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// mainTranscript writes only alice's main transcript (no subagent run).
func mainTranscript(t *testing.T, e *env, lines ...string) {
	t.Helper()
	tf.Write(t, e.home, ".claude/settings.json", "{}")
	tf.Write(t, e.home, ".claude/projects/-work-alice/"+tf.ClaudeSession+".jsonl", strings.Join(lines, "\n")+"\n")
}

var u1At = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC) // ClaudeLines()[0]

func TestClaim_HookRowThenImport(t *testing.T) {
	e := newEnv(t)
	cl := tf.ClaudeLines()
	mainTranscript(t, e, cl...)
	hook := hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "", u1At.Add(90*time.Millisecond))

	s := e.run(t, agents.ToolClaude, Options{})
	if s.Linked != 1 || s.Inserted != 1 || s.Attached != 1 || s.Standalone != 0 {
		t.Fatalf("stats = %+v", s)
	}
	es := e.session(t, tf.ClaudeSession)
	if len(es) != 2 {
		t.Fatalf("rows = %d, want 2 (claimed hook row + 'And channels?')", len(es))
	}
	h := es[0]
	if h.ID != hook || h.Response != "Lightweight threads.\nScheduled by the runtime." || h.TokenCountIn != 12 || h.Model != "claude-test" ||
		h.ToolVersion != "2.1.300" || h.TranscriptAt == nil || !h.TranscriptAt.Equal(u1At) {
		t.Fatalf("claimed row = %+v", h)
	}
	if h.Terminal != "ghostty" || h.PermissionMode != "plan" || !h.CreatedAt.Equal(u1At.Add(90*time.Millisecond)) ||
		!strings.HasPrefix(h.Raw, "/transcripts/") {
		t.Fatalf("hook values overwritten: %+v", h)
	}
	for _, key := range []string{legacyHash(cl[0]), "msg:claude-code:" + tf.ClaudeSession + ":u-1", legacyHash(cl[2])} {
		if id, ok, _ := e.st.ImportLineEntry(context.Background(), key); !ok || id != hook {
			t.Fatalf("key %s -> %q", key, id)
		}
	}
	if again := e.run(t, agents.ToolClaude, Options{Force: true}); again.Inserted+again.Standalone+again.Linked != 0 || e.entries(t) != 2 {
		t.Fatalf("re-run changed rows: %+v", again)
	}
}

// TestClaim_StopHookAnswerKept: the hook row already holds the answer the
// Stop hook captured. The transcript's answer is linked to it (no
// standalone copy) and only fills what the hook lacked.
func TestClaim_StopHookAnswerKept(t *testing.T) {
	e := newEnv(t)
	cl := tf.ClaudeLines()
	mainTranscript(t, e, cl[0], cl[2])
	hook := hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "Lightweight threads.\nScheduled by the runtime.", u1At.Add(time.Second))
	s := e.run(t, agents.ToolClaude, Options{})
	if s.Linked != 1 || s.RecordsMatched != 1 || e.entries(t) != 1 {
		t.Fatalf("stats = %+v, rows %d", s, e.entries(t))
	}
	h := e.session(t, tf.ClaudeSession)[0]
	if h.ID != hook || h.TokenCountIn != 12 || h.StopReason != "end_turn" {
		t.Fatalf("hook row = %+v", h)
	}
}

// TestClaim_StopHookAnswerDiffers: the hook kept the final answer; an
// earlier interim text of the transcript does not replace it.
func TestClaim_StopHookAnswerDiffers(t *testing.T) {
	e := newEnv(t)
	cl := tf.ClaudeLines()
	mainTranscript(t, e, cl[0], cl[2])
	hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "Final words.", u1At.Add(time.Second))
	e.run(t, agents.ToolClaude, Options{})
	es := e.session(t, tf.ClaudeSession)
	if es[0].Response != "Final words." {
		t.Fatalf("hook answer overwritten: %q", es[0].Response)
	}
}

func TestClaim_SamePromptTwiceEachOwnRow(t *testing.T) {
	e := newEnv(t)
	at2 := u1At.Add(5 * time.Minute)
	p2 := `{"type":"user","uuid":"u-again","sessionId":"` + tf.ClaudeSession + `","cwd":"/work/alice","timestamp":"` + at2.Format(time.RFC3339Nano) + `","version":"2.1.300","message":{"role":"user","content":"Explain goroutines briefly."}}`
	mainTranscript(t, e, tf.ClaudeLines()[0], p2)
	second := hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "", at2.Add(200*time.Millisecond))
	first := hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "", u1At.Add(100*time.Millisecond))
	s := e.run(t, agents.ToolClaude, Options{})
	if s.Linked != 2 || s.Inserted != 0 || e.entries(t) != 2 {
		t.Fatalf("stats = %+v rows %d", s, e.entries(t))
	}
	for id, key := range map[string]string{first: "u-1", second: "u-again"} {
		if got, ok, _ := e.st.ImportLineEntry(context.Background(), "msg:claude-code:"+tf.ClaudeSession+":"+key); !ok || got != id {
			t.Fatalf("%s linked to %q, want %q", key, got, id)
		}
	}
}

func TestClaim_OutsideWindowOrOtherTextInserts(t *testing.T) {
	e := newEnv(t)
	mainTranscript(t, e, tf.ClaudeLines()[0])
	hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "", u1At.Add(claimWindow+time.Second))
	hookRow(t, e, tf.ClaudeSession, "Explain goroutines.", "", u1At)
	s := e.run(t, agents.ToolClaude, Options{})
	if s.Linked != 0 || s.Inserted != 1 || e.entries(t) != 3 {
		t.Fatalf("stats = %+v rows %d", s, e.entries(t))
	}
}

func TestDedupe_MergesImportDuplicates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cl := tf.ClaudeLines()
	// What an older ailog left: the hook row and an imported copy.
	hook := hookRow(t, e, tf.ClaudeSession, "Explain goroutines briefly.", "", u1At.Add(80*time.Millisecond))
	at := u1At
	imp, err := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: tf.ClaudeSession,
		Prompt: "Explain goroutines briefly.", Response: "Lightweight threads.", Model: "claude-test", TokensIn: 12, CreatedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.st.ImportLineRecord(ctx, legacyHash(cl[0]), "/f", imp)
	// A repeated prompt hours later with no hook row is not a duplicate.
	later := u1At.Add(3 * time.Hour)
	lone, _ := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: tf.ClaudeSession, Prompt: "Explain goroutines briefly.", CreatedAt: &later})
	_ = e.st.ImportLineRecord(ctx, "other-key", "/f", lone)

	dry, err := Dedupe(ctx, e.st, true)
	if err != nil || dry.Total() != 1 || dry.Merged[agents.ToolClaude] != 1 || e.entries(t) != 3 {
		t.Fatalf("dry run = %+v %v, rows %d", dry, err, e.entries(t))
	}
	real, err := Dedupe(ctx, e.st, false)
	if err != nil || real.Total() != 1 || e.entries(t) != 2 {
		t.Fatalf("run = %+v %v, rows %d", real, err, e.entries(t))
	}
	h, _ := e.st.GetByID(ctx, hook)
	if h.Response != "Lightweight threads." || h.TokenCountIn != 12 || h.Terminal != "ghostty" || h.TranscriptAt == nil {
		t.Fatalf("merged hook row = %+v", h)
	}
	if id, _, _ := e.st.ImportLineEntry(ctx, legacyHash(cl[0])); id != hook {
		t.Fatal("import key not moved to the hook row")
	}
	if again, _ := Dedupe(ctx, e.st, false); again.Total() != 0 {
		t.Fatalf("second dedupe merged again: %+v", again)
	}
	// After dedupe a re-import writes nothing new for the merged turn.
	mainTranscript(t, e, cl[0])
	if s := e.run(t, agents.ToolClaude, Options{}); s.Inserted != 0 {
		t.Fatalf("re-import after dedupe = %+v", s)
	}
}

// A prompt sent twice in a burst has two live captures. Once the importer
// (or a first dedupe) links one of them, it is still a live capture: no
// dedupe run may merge it into the other one, so a second run merges
// nothing. An old imported row with no key is never a merge target.
func TestDedupe_NeverMergesTwoLiveCaptures(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const prompt = "continue"
	first := hookRow(t, e, tf.ClaudeSession, prompt, "", u1At)
	second := hookRow(t, e, tf.ClaudeSession, prompt, "", u1At.Add(8*time.Second))
	_ = e.st.ImportLineRecord(ctx, "claimed-key", "/f", first) // the importer claimed the first capture
	oldAt := u1At.Add(30 * time.Second)
	oldImport, _ := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: tf.ClaudeSession, Prompt: prompt, CreatedAt: &oldAt})

	for run := 1; run <= 2; run++ {
		got, err := Dedupe(ctx, e.st, false)
		if err != nil || got.Total() != 0 {
			t.Fatalf("run %d = %+v %v, want nothing merged", run, got, err)
		}
	}
	for _, id := range []string{first, second, oldImport} {
		if _, err := e.st.GetByID(ctx, id); err != nil {
			t.Fatalf("row %s was merged away", id)
		}
	}
}
