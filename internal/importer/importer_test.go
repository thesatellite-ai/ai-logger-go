package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
	"github.com/khanakia/ai-logger/internal/store"
	tf "github.com/khanakia/ai-logger/internal/testfixtures"
)

// Every fixture comes from internal/testfixtures: made-up content (alice,
// bob) in the real wire shapes, written into a temp user home.

type env struct {
	home   string
	reg    agents.Registry
	st     *store.Store
	dbPath string
	out    bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{home: tf.Isolate(t)}
	e.dbPath = filepath.Join(t.TempDir(), "log.db")
	st, err := store.Open(context.Background(), e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e.st = st
	return e
}

func (e *env) agent(t *testing.T, tool string) agents.Agent {
	t.Helper()
	if len(e.reg.All()) == 0 {
		reg, err := agents.FromOS()
		if err != nil {
			t.Fatal(err)
		}
		e.reg = reg
	}
	a, ok := e.reg.ByTool(tool)
	if !ok {
		t.Fatalf("no agent %s", tool)
	}
	return a
}

func (e *env) run(t *testing.T, tool string, opts Options) Stats {
	t.Helper()
	opts.Out = &e.out
	s, err := Run(context.Background(), e.st, e.agent(t, tool), opts)
	if err != nil {
		t.Fatalf("run %s: %v\n%s", tool, err, e.out.String())
	}
	return s
}

func (e *env) session(t *testing.T, id string) []*store.Entry {
	t.Helper()
	es, err := e.st.SessionEntries(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// count runs a COUNT(*) query on the log database.
func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) entries(t *testing.T) int { return e.count(t, `SELECT COUNT(*) FROM entries`) }

// legacyHash is how every older ailog keyed a transcript line: SHA-256
// of the line's bytes.
func legacyHash(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:])
}

func TestRun_ClaudeEveryHomeAndIdempotent(t *testing.T) {
	e := newEnv(t)
	tf.ClaudeHome(t, e.home)
	s := e.run(t, agents.ToolClaude, Options{})
	if s.Homes != 2 || s.Files != 3 || s.Inserted != 4 || s.Attached != 3 || s.Standalone != 0 || s.FilesSuspect != 0 {
		t.Fatalf("stats = %+v\n%s", s, e.out.String())
	}
	alice := e.session(t, tf.ClaudeSession)
	if len(alice) != 3 {
		t.Fatalf("alice entries = %d", len(alice))
	}
	first := alice[0]
	if first.Prompt != "Explain goroutines briefly." || first.Response != "Lightweight threads.\nScheduled by the runtime." ||
		first.Model != "claude-test" || first.StopReason != "end_turn" || first.TokenCountIn != 12 || first.TokenCountOut != 6 ||
		first.TokenCountCacheRead != 3 || first.TokenCountCacheCreate != 2 || first.ToolVersion != "2.1.300" ||
		first.Tool != agents.ToolClaude || first.Cwd != "/work/alice" || first.Project != "alice" || first.SessionName != "" {
		t.Fatalf("first turn = %+v", first)
	}
	if first.CreatedAt.UTC() != time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("created_at = %v (transcript time lost)", first.CreatedAt)
	}
	if alice[1].Prompt != "And channels?" || alice[1].Response != "" {
		t.Fatalf("second prompt = %+v", alice[1])
	}
	if alice[2].Prompt != "Find the config file." || alice[2].Response != "It is config.yaml." {
		t.Fatalf("subagent turn = %+v", alice[2])
	}
	if bob := e.session(t, tf.ClaudeBobSession); len(bob) != 1 || bob[0].Response != "Hi bob." {
		t.Fatalf("bob = %+v", bob)
	}

	again := e.run(t, agents.ToolClaude, Options{Force: true})
	if again.Inserted != 0 || again.Attached != 0 || again.Standalone != 0 || again.RecordsSkipped != 7 {
		t.Fatalf("forced re-run not idempotent: %+v", again)
	}
	skipped := e.run(t, agents.ToolClaude, Options{})
	if skipped.FilesSkipped != 3 || skipped.Records != 0 {
		t.Fatalf("watermark did not skip unchanged files: %+v", skipped)
	}
}

func TestRun_CodexCurrentAndLegacy(t *testing.T) {
	e := newEnv(t)
	tf.CodexHome(t, e.home, map[string]string{tf.CodexSession: tf.CodexCurrent(), tf.CodexLegacyID: tf.CodexLegacy()},
		tf.CodexSession, tf.CodexLegacyID)
	s := e.run(t, agents.ToolCodex, Options{})
	if s.Inserted != 3 || s.Attached != 3 || s.Standalone != 0 || s.FilesSuspect != 0 {
		t.Fatalf("stats = %+v\n%s", s, e.out.String())
	}
	cur := e.session(t, tf.CodexSession)
	if len(cur) != 1 {
		t.Fatalf("commentary stored as a turn: %+v", cur)
	}
	c := cur[0]
	if c.Prompt != "How do I list files?" || c.Response != "Use ls." || c.Model != "gpt-test-1" ||
		c.TokenCountIn != 200 || c.TokenCountOut != 27 || c.TokenCountCacheRead != 80 ||
		c.PermissionMode != "on-request" || c.Terminal != "Codex Desktop" || c.TerminalTitle != "vscode" || c.ToolVersion != "0.160.0" {
		t.Fatalf("codex turn = %+v", c)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(c.Raw), &raw); err != nil {
		t.Fatalf("raw = %q: %v", c.Raw, err)
	}
	if raw["codex.reasoning_effort"] != "medium" || raw["line_hash"] == "" {
		t.Fatalf("raw = %v", raw)
	}
	leg := e.session(t, tf.CodexLegacyID)
	if len(leg) != 2 || leg[0].Response != "Four." || leg[0].TokenCountIn != 50 || leg[1].Response != "You are welcome." {
		t.Fatalf("legacy = %+v", leg)
	}
}

func TestRun_GeminiRewrittenAnswerReplaces(t *testing.T) {
	e := newEnv(t)
	tf.GeminiHome(t, e.home)
	s := e.run(t, agents.ToolGemini, Options{})
	if s.Inserted != 1 || s.Attached != 1 || s.Updated != 1 {
		t.Fatalf("stats = %+v", s)
	}
	es := e.session(t, tf.GeminiSession)
	if len(es) != 1 || es[0].Response != "It explains setup." || es[0].TokenCountIn != 300 || es[0].TokenCountOut != 10 ||
		es[0].TokenCountCacheRead != 100 || es[0].Cwd != "/work/alice" || es[0].Tool != agents.ToolGemini {
		t.Fatalf("gemini = %+v", es)
	}
	if again := e.run(t, agents.ToolGemini, Options{Force: true}); again.Updated != 0 || again.Inserted != 0 {
		t.Fatalf("re-run rewrote the answer again: %+v", again)
	}
}

func TestRun_PiAndOpencode(t *testing.T) {
	e := newEnv(t)
	tf.PiHome(t, e.home)
	tf.OpencodeHome(t, e.home)
	if s := e.run(t, agents.ToolPi, Options{}); s.Inserted != 1 || s.Attached != 1 {
		t.Fatalf("pi stats = %+v", s)
	}
	pi := e.session(t, tf.PiSession)
	if len(pi) != 1 || pi[0].Response != "There are three." || pi[0].Cwd != "/work/bob" || pi[0].Model != "prov/pi-model" {
		t.Fatalf("pi = %+v", pi)
	}
	if s := e.run(t, agents.ToolOpencode, Options{}); s.Inserted != 2 || s.Attached != 2 || s.Files != 2 {
		t.Fatalf("opencode stats = %+v\n%s", s, e.out.String())
	}
	leg := e.session(t, tf.OpencodeLegacy)
	if len(leg) != 1 || leg[0].Response != "Renamed it." || leg[0].Model != "oc-model" || leg[0].SessionName != "Alice legacy" ||
		leg[0].TokenCountOut != 8 || leg[0].TokenCountCacheRead != 4 || leg[0].TokenCountCacheCreate != 1 {
		t.Fatalf("opencode legacy = %+v", leg)
	}
	if !strings.Contains(leg[0].Raw, `"opencode.project_id":"global"`) {
		t.Fatalf("raw = %s", leg[0].Raw)
	}
	db := e.session(t, tf.OpencodeDB)
	if len(db) != 1 || db[0].Prompt != "Add a test." || db[0].Response != "Added one." || db[0].ToolVersion != "1.18.30" {
		t.Fatalf("opencode db = %+v", db)
	}
}

// TestMigration_OldDatabaseKeys is the dedup-continuity proof (plan T3):
// a log.db written by an older ailog keys every imported line by the
// SHA-256 of its bytes. Reading the same transcripts through agentio must
// hit those keys (Turn.Raw is the same bytes), write nothing, and add the
// message-id keys for the same entries.
func TestMigration_OldDatabaseKeys(t *testing.T) {
	e := newEnv(t)
	main := tf.ClaudeHome(t, e.home)
	ctx := context.Background()
	lines := tf.ClaudeLines()
	// What the old importer stored for alice's main transcript: prompt 1
	// with its answer, prompt 2, each keyed by its line hash.
	u1, err := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: tf.ClaudeSession,
		Prompt: "Explain goroutines briefly.", Raw: legacyHash(lines[0])})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AttachResponse(ctx, store.AttachResponseInput{EntryID: u1, Response: "Lightweight threads.\nScheduled by the runtime."}); err != nil {
		t.Fatal(err)
	}
	u2, err := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: agents.ToolClaude, SessionID: tf.ClaudeSession,
		Prompt: "And channels?", Raw: legacyHash(lines[4])})
	if err != nil {
		t.Fatal(err)
	}
	for line, id := range map[string]string{lines[0]: u1, lines[2]: u1, lines[4]: u2} {
		if err := e.st.ImportLineRecord(ctx, legacyHash(line), main, id); err != nil {
			t.Fatal(err)
		}
	}
	before := e.entries(t)

	s := e.run(t, agents.ToolClaude, Options{Home: filepath.Join(e.home, ".claude")})
	if s.RecordsMatched != 0 {
		t.Fatalf("legacy keys missed, fell back to text match: %+v", s)
	}
	// Only the subagent run (never imported before) is new: 1 prompt + 1 answer.
	if got := e.entries(t) - before; got != 1 || s.Inserted != 1 || s.Attached != 1 {
		t.Fatalf("new rows = %d, stats = %+v", got, s)
	}
	for _, uuid := range []string{"u-1", "a-1", "u-2"} {
		key := "msg:" + agents.ToolClaude + ":" + tf.ClaudeSession + ":" + uuid
		if n := e.count(t, `SELECT COUNT(*) FROM import_lines WHERE line_hash = ?`, key); n != 1 {
			t.Fatalf("message-id key %s not back-filled", key)
		}
	}
	if n := e.count(t, `SELECT COUNT(*) FROM import_lines WHERE line_hash = ? AND entry_id = ?`,
		"msg:"+agents.ToolClaude+":"+tf.ClaudeSession+":a-1", u1); n != 1 {
		t.Fatal("answer's message-id key points at another entry")
	}
}

// TestMigration_OldKeysCodexOpencodeCRLF extends the continuity proof to
// the other formats older ailog imported: Codex event lines, opencode
// message files (hashed whole) and a CRLF-terminated Claude line (the old
// scanner dropped the "\r").
func TestMigration_OldKeysCodexOpencodeCRLF(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seed := func(prompt, session, tool string, keys ...string) {
		t.Helper()
		id, err := e.st.InsertEntry(ctx, store.InsertEntryInput{Tool: tool, SessionID: session, Prompt: prompt})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			if err := e.st.ImportLineRecord(ctx, k, "old", id); err != nil {
				t.Fatal(err)
			}
		}
	}
	cx := strings.Split(strings.TrimSpace(tf.CodexLegacy()), "\n")
	tf.CodexHome(t, e.home, map[string]string{tf.CodexLegacyID: tf.CodexLegacy()}, tf.CodexLegacyID)
	seed("What is two plus two?", tf.CodexLegacyID, agents.ToolCodex, legacyHash(cx[2]), legacyHash(cx[3]))
	seed("Thanks", tf.CodexLegacyID, agents.ToolCodex, legacyHash(cx[5]), legacyHash(cx[6]))

	tf.OpencodeHome(t, e.home)
	st := filepath.Join(e.home, ".local/share/opencode/storage/message", tf.OpencodeLegacy)
	var fileKeys []string
	for _, name := range []string{"msg_0f0000000001aliceAAAAAA01.json", "msg_0f0000000002aliceAAAAAA02.json"} {
		b, err := os.ReadFile(filepath.Join(st, name))
		if err != nil {
			t.Fatal(err)
		}
		fileKeys = append(fileKeys, legacyHash(string(b)))
	}
	seed("Rename the module.", tf.OpencodeLegacy, agents.ToolOpencode, fileKeys...)

	cl := tf.ClaudeLines()
	tf.Write(t, e.home, ".claude/settings.json", "{}")
	tf.Write(t, e.home, ".claude/projects/-work-alice/"+tf.ClaudeSession+".jsonl", cl[0]+"\r\n")
	seed("Explain goroutines briefly.", tf.ClaudeSession, agents.ToolClaude, legacyHash(cl[0]))

	rows := e.entries(t)
	for _, tool := range []string{agents.ToolCodex, agents.ToolOpencode, agents.ToolClaude} {
		s := e.run(t, tool, Options{})
		if s.RecordsMatched != 0 {
			t.Fatalf("%s: legacy keys missed: %+v", tool, s)
		}
	}
	// Only bob's database session is new for opencode.
	if got := e.entries(t) - rows; got != 1 {
		t.Fatalf("new rows = %d, want 1 (bob's opencode.db session)", got)
	}
}

// TestMigration_RowsWithoutKeys covers rows stored under a key the
// current reader does not produce (another encoding, a lost import_lines
// table): the text match finds them, nothing is written twice.
func TestMigration_RowsWithoutKeys(t *testing.T) {
	e := newEnv(t)
	tf.CodexHome(t, e.home, map[string]string{tf.CodexLegacyID: tf.CodexLegacy()}, tf.CodexLegacyID)
	e.run(t, agents.ToolCodex, Options{})
	rows := e.entries(t)
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM import_lines`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s := e.run(t, agents.ToolCodex, Options{Force: true})
	if e.entries(t) != rows || s.Inserted != 0 || s.Attached != 0 || s.Standalone != 0 || s.RecordsMatched != 4 {
		t.Fatalf("rows %d -> %d, stats %+v", rows, e.entries(t), s)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM import_lines`); n != 4 {
		t.Fatalf("keys not recorded again: %d", n)
	}
}

func TestRun_IncrementalFromCursor(t *testing.T) {
	e := newEnv(t)
	main := tf.ClaudeHome(t, e.home)
	e.run(t, agents.ToolClaude, Options{})
	key := main
	wm, ok, err := e.st.ImportFileWatermark(context.Background(), key)
	if err != nil || !ok || wm.Offset == 0 {
		t.Fatalf("cursor not stored: %+v %v %v", wm, ok, err)
	}

	answer := `{"type":"assistant","uuid":"a-2","sessionId":"` + tf.ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T10:01:05.000Z","version":"2.1.300","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"Typed pipes."}],"usage":{"input_tokens":9,"output_tokens":2}}}`
	// Half a line: not consumed.
	tf.Append(t, main, answer[:40])
	if s := e.run(t, agents.ToolClaude, Options{}); s.Records != 0 {
		t.Fatalf("a partial line was read: %+v", s)
	}
	tf.Append(t, main, answer[40:]+"\n")
	s := e.run(t, agents.ToolClaude, Options{})
	// Read from the cursor: only the new answer, no re-read of old lines.
	if s.Records != 1 || s.RecordsSkipped != 0 || s.Attached != 1 {
		t.Fatalf("stats = %+v", s)
	}
	alice := e.session(t, tf.ClaudeSession)
	if alice[1].Prompt != "And channels?" || alice[1].Response != "Typed pipes." {
		t.Fatalf("answer read in a later run not attached to its prompt: %+v", alice[1])
	}
}

func TestRun_ShrunkTranscriptReadFromStart(t *testing.T) {
	e := newEnv(t)
	main := tf.ClaudeHome(t, e.home)
	e.run(t, agents.ToolClaude, Options{})
	rows := e.entries(t)
	lines := tf.ClaudeLines()
	extra := `{"type":"user","uuid":"u-9","sessionId":"` + tf.ClaudeSession + `","cwd":"/work/alice","timestamp":"2026-03-01T11:00:00.000Z","version":"2.1.300","message":{"role":"user","content":"Rewritten tail."}}`
	if err := os.WriteFile(main, []byte(lines[0]+"\n"+extra+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := e.run(t, agents.ToolClaude, Options{})
	if e.entries(t) != rows+1 || s.Inserted != 1 || s.RecordsSkipped != 1 {
		t.Fatalf("rows %d -> %d, stats %+v", rows, e.entries(t), s)
	}
}

// TestRun_CodexAnswerHeldForTokens: Codex writes the token count a few
// lines after the answer. A run between the two must not store the answer
// without usage; the next run stores it with usage, on its prompt.
func TestRun_CodexAnswerHeldForTokens(t *testing.T) {
	e := newEnv(t)
	all := strings.Split(strings.TrimSpace(tf.CodexCurrent()), "\n")
	head := strings.Join(all[:len(all)-1], "\n") + "\n" // everything but the last token_count
	p := tf.CodexHome(t, e.home, map[string]string{tf.CodexSession: head}, tf.CodexSession)[0]
	if s := e.run(t, agents.ToolCodex, Options{}); s.Inserted != 1 || s.Attached != 0 || s.Standalone != 0 {
		t.Fatalf("first run = %+v", s)
	}
	tf.Append(t, p, all[len(all)-1]+"\n")
	if s := e.run(t, agents.ToolCodex, Options{}); s.Attached != 1 || s.Inserted != 0 {
		t.Fatalf("second run = %+v", s)
	}
	es := e.session(t, tf.CodexSession)
	if len(es) != 1 || es[0].Response != "Use ls." || es[0].TokenCountIn != 200 {
		t.Fatalf("entries = %+v", es)
	}
}

func TestRun_LimitKeepsWatermark(t *testing.T) {
	e := newEnv(t)
	tf.ClaudeHome(t, e.home)
	if s := e.run(t, agents.ToolClaude, Options{Limit: 1}); s.Inserted+s.Standalone != 1 {
		t.Fatalf("limit = %+v", s)
	}
	s := e.run(t, agents.ToolClaude, Options{})
	if s.FilesSkipped != 0 || e.entries(t) != 4 {
		t.Fatalf("after limit: %+v, %d rows", s, e.entries(t))
	}
}

func TestRun_SinceFilter(t *testing.T) {
	e := newEnv(t)
	tf.ClaudeHome(t, e.home)
	s := e.run(t, agents.ToolClaude, Options{Since: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)})
	if s.Inserted != 1 || e.entries(t) != 1 { // only bob's turn is on or after 2 March
		t.Fatalf("since = %+v", s)
	}
}

func TestRun_FromHomeAndLegacyRoot(t *testing.T) {
	for _, from := range []string{".claude-bob", ".claude-bob/projects"} {
		t.Run(from, func(t *testing.T) {
			e := newEnv(t)
			tf.ClaudeHome(t, e.home)
			s := e.run(t, agents.ToolClaude, Options{Home: filepath.Join(e.home, from)})
			if s.Homes != 1 || s.Files != 1 || s.Inserted != 1 {
				t.Fatalf("stats = %+v", s)
			}
		})
	}
	e := newEnv(t)
	tf.ClaudeHome(t, e.home)
	if s := e.run(t, agents.ToolClaude, Options{Home: "~/.claude-bob"}); s.Inserted != 1 {
		t.Fatalf("~ not expanded: %+v", s)
	}
}

func TestRun_DriftWatchdog(t *testing.T) {
	newer := `{"type":"user","uuid":"d-u","sessionId":"dddd","cwd":"/work/alice","timestamp":"2026-04-01T10:00:00.000Z","version":"9.9.9","message":{"role":"user","content":"hi"}}
{"type":"assistant","uuid":"d-a","sessionId":"dddd","cwd":"/work/alice","timestamp":"2026-04-01T10:00:01.000Z","version":"9.9.9","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}
`
	for _, strict := range []bool{true, false} {
		e := newEnv(t)
		tf.Write(t, e.home, ".claude/settings.json", "{}")
		p := tf.Write(t, e.home, ".claude/projects/-work-alice/dddd.jsonl", newer)
		s := e.run(t, agents.ToolClaude, Options{Strict: strict})
		_, stamped, _ := e.st.ImportFileWatermark(context.Background(), p)
		wantTag := "WARN(newer-version)"
		if strict {
			wantTag = "REJECT"
		}
		if s.FilesSuspect != 1 || !strings.Contains(e.out.String(), wantTag) || stamped == strict {
			t.Fatalf("strict=%v: stats %+v stamped %v\n%s", strict, s, stamped, e.out.String())
		}
	}
}

func TestRecordOf(t *testing.T) {
	a := agents.Agent{Tool: "tool-x", UseTitle: true}
	tr := agents.Transcript{Key: "/k", ConversationID: "conv", Dir: "/d", Title: "T"}
	turn := agentio.Turn{ID: "m1", Role: agentio.RoleAssistant, Text: "x", Model: "p/m", Raw: agentio.Raw("raw"),
		Tokens: &agentio.TokenCounts{Input: 1, CachedInput: 2, CacheWrite: 3, Output: 4, Reasoning: 5}}
	r, ok := recordOf(a, tr, turn, agents.Annotation{Model: "m"})
	if !ok || r.SessionID != "conv" || r.CWD != "/d" || r.SessionName != "T" || r.Model != "m" || r.TokensIn != 1 ||
		r.TokensCacheRead != 2 || r.TokensCacheWrite != 3 || r.TokensOut != 9 || r.MsgKey != "msg:tool-x:conv:m1" ||
		r.LineHash != legacyHash("raw") || r.Source != "/k" {
		t.Fatalf("record = %+v", r)
	}
	for name, tu := range map[string]agentio.Turn{
		"interim": {Role: agentio.RoleAssistant, Text: "x", Interim: true},
		"empty":   {Role: agentio.RoleAssistant},
	} {
		if _, ok := recordOf(a, tr, tu, agents.Annotation{}); ok {
			t.Errorf("%s turn was kept", name)
		}
	}
	user, _ := recordOf(agents.Agent{Tool: "t"}, tr, agentio.Turn{Role: agentio.RoleUser, Text: "q", Model: "m"}, agents.Annotation{})
	if user.Model != "" || user.SessionName != "" || user.MsgKey != "" || user.LineHash != "" {
		t.Fatalf("user record = %+v", user)
	}
}

func TestNewerThan(t *testing.T) {
	cases := []struct {
		v, tested string
		want      bool
	}{
		{"0.169.0", "0.162.1", true},
		{"0.118.0-alpha.2", "0.118.0", false},
		{"2.1.9", "2.1.114", false},
		{"x", "1.0", false},
		{"1.0", "", false},
	}
	for _, c := range cases {
		if got := newerThan(c.v, c.tested); got != c.want {
			t.Errorf("newerThan(%q, %q) = %v", c.v, c.tested, got)
		}
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
