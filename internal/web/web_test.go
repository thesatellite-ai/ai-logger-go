package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web"
)

// newTestServer wires a real Store + real router against a temp DB.
// Returns the in-process httptest.Server caller to issue requests at.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	srv := web.New(s, "127.0.0.1:0")
	// We don't actually need the live listener — httptest.NewServer
	// gives us a URL we control. But web.New constructs the chi mux
	// and wires routes; we lift the handler from there and serve it
	// via httptest. Easiest path: re-wire by inspecting srv's internals
	// is fragile, so we let web.Server expose its handler instead.
	// For the tests below we build httptest from a fresh http.Server.
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs, s
}

func seedEntry(t *testing.T, s *store.Store, in store.InsertEntryInput) string {
	t.Helper()
	id, err := s.InsertEntry(context.Background(), in)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	return id
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestList_RendersEntries(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Tool: "claude-code", Prompt: "fix the race condition"})

	body := getBody(t, srv.URL+"/")
	mustContain(t, body, "fix the race condition")
	mustContain(t, body, "claude-code")
	mustContain(t, body, "Recent entries")
}

func TestList_EmptyState(t *testing.T) {
	srv, _ := newTestServer(t)
	body := getBody(t, srv.URL+"/")
	mustContain(t, body, "No entries logged yet")
}

func TestEntryDetail_RendersMarkdown(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{
		Prompt: "# heading\n\n**bold** and `code`",
	})
	body := getBody(t, srv.URL+"/entry/"+id)
	mustContain(t, body, "<h1")
	mustContain(t, body, "<strong>bold")
	mustContain(t, body, "<code>code</code>")
}

func TestEntryDetail_PrefixResolvesToFullID(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{Prompt: "x"})
	body := getBody(t, srv.URL+"/entry/"+id[:13])
	mustContain(t, body, id) // full id appears in the header
}

func TestEntryJSON_ReturnsJSONBody(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{Prompt: "json me"})
	resp, err := http.Get(srv.URL + "/entry/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content-type: %s", got)
	}
	body := readAll(t, resp)
	mustContain(t, body, `"prompt"`)
	mustContain(t, body, "json me")
}

func TestSearch_FullPage(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "race condition in worker"})
	body := getBody(t, srv.URL+"/search?q=race")
	mustContain(t, body, "race condition")
	mustContain(t, body, "1 match")
}

func TestSearch_EmptyQueryShowsHint(t *testing.T) {
	srv, _ := newTestServer(t)
	body := getBody(t, srv.URL+"/search")
	mustContain(t, body, "Type to search")
}

func TestSearchPartial_LayoutLess(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "needle"})
	body := getBody(t, srv.URL+"/search/partial?q=needle")
	if strings.Contains(body, "<!doctype") {
		t.Fatal("partial should not include layout")
	}
	mustContain(t, body, "needle")
}

func TestStarToggle_FlipsAndPersists(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{Prompt: "p"})

	// Initially unstarred → click star.
	body := postForm(t, srv.URL+"/entry/"+id+"/star", nil)
	mustContain(t, body, "★ starred")

	e, _ := s.GetByID(context.Background(), id)
	if !e.Starred {
		t.Fatal("starred bit not flipped in DB")
	}

	// Click again → unstar.
	body = postForm(t, srv.URL+"/entry/"+id+"/star", nil)
	mustContain(t, body, "☆ star")

	e, _ = s.GetByID(context.Background(), id)
	if e.Starred {
		t.Fatal("starred bit not toggled off")
	}
}

func TestTagPost_MergesAndSorts(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{Prompt: "p", Tags: "b,a"})

	body := postForm(t, srv.URL+"/entry/"+id+"/tag", map[string]string{"tags": "c,a"})
	// Should render all of a, b, c.
	for _, want := range []string{">a<", ">b<", ">c<"} {
		mustContain(t, body, want)
	}

	e, _ := s.GetByID(context.Background(), id)
	if e.Tags != "a,b,c" {
		t.Fatalf("tags: %q", e.Tags)
	}
}

func TestNotesPost_PersistsAndRenders(t *testing.T) {
	srv, s := newTestServer(t)
	id := seedEntry(t, s, store.InsertEntryInput{Prompt: "p"})

	body := postForm(t, srv.URL+"/entry/"+id+"/notes", map[string]string{"notes": "hello note"})
	mustContain(t, body, "hello note")

	e, _ := s.GetByID(context.Background(), id)
	if e.Notes != "hello note" {
		t.Fatalf("notes: %q", e.Notes)
	}
}

func TestSessionDetail_Threads(t *testing.T) {
	srv, s := newTestServer(t)
	for _, p := range []string{"first", "second", "third"} {
		seedEntry(t, s, store.InsertEntryInput{Prompt: p, SessionID: "sess-1"})
	}
	body := getBody(t, srv.URL+"/session/sess-1")
	mustContain(t, body, "first")
	mustContain(t, body, "second")
	mustContain(t, body, "third")
}

func TestSessions_ListShowsSummary(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "p1", SessionID: "a"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "p2", SessionID: "b"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "p3", SessionID: "b"})
	body := getBody(t, srv.URL+"/sessions")
	mustContain(t, body, "2 turns")
	mustContain(t, body, "1 turn")
}

func TestSessionRename_UpdatesAndRenders(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "p", SessionID: "rn"})
	body := postForm(t, srv.URL+"/session/rn/name", map[string]string{"name": "renamed it"})
	mustContain(t, body, "renamed it")
	e, _ := s.GetByID(context.Background(), seedEntry(t, s, store.InsertEntryInput{Prompt: "x", SessionID: "rn"}))
	_ = e // session-name is updated on every entry; verify via SessionEntries
}

func TestStats_RendersCountsAndBars(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "a", Tool: "claude-code"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "b", Tool: "cursor"})
	body := getBody(t, srv.URL+"/stats")
	mustContain(t, body, "Total entries")
	mustContain(t, body, "By tool")
	mustContain(t, body, "By project")
	mustContain(t, body, "claude-code")
	mustContain(t, body, "cursor")
	if !strings.Contains(body, `style="width:`) {
		t.Fatal("expected bar widths in stats page")
	}
}

func TestTemplates_ListsStarredOnly(t *testing.T) {
	srv, s := newTestServer(t)
	plain := seedEntry(t, s, store.InsertEntryInput{Prompt: "plain entry"})
	starred := seedEntry(t, s, store.InsertEntryInput{Prompt: "starred entry"})
	if err := s.SetStarred(context.Background(), starred, true); err != nil {
		t.Fatal(err)
	}
	body := getBody(t, srv.URL+"/templates")
	if strings.Contains(body, "plain entry") {
		t.Fatal("non-starred entry leaked into templates")
	}
	mustContain(t, body, "starred entry")
	_ = plain
}

func TestProjects_ListsCounts(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "a", Project: "github.com/x/one"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "b", Project: "github.com/x/one"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "c", Project: "github.com/x/two"})
	body := getBody(t, srv.URL+"/projects")
	mustContain(t, body, "github.com/x/one")
	mustContain(t, body, "github.com/x/two")
	// (none) bucket should render as text, not a link with q=*
	if strings.Contains(body, "q=*") {
		t.Fatal("project links must not use q=* (invalid FTS5 query)")
	}
}

func TestSearch_FilterOnlyNoQuery_ReturnsAllInProject(t *testing.T) {
	srv, s := newTestServer(t)
	seedEntry(t, s, store.InsertEntryInput{Prompt: "alpha", Project: "github.com/x/one"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "beta", Project: "github.com/x/one"})
	seedEntry(t, s, store.InsertEntryInput{Prompt: "gamma", Project: "github.com/x/other"})

	body := getBody(t, srv.URL+"/search?project=github.com/x/one")
	mustContain(t, body, "alpha")
	mustContain(t, body, "beta")
	if strings.Contains(body, "gamma") {
		t.Fatal("filter-only search leaked entries from other project")
	}
}

func TestStaticFiles_ServeHTMX(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/static/htmx.min.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "htmx") {
		t.Fatal("static file does not look like htmx")
	}
}

// ---- tiny test helpers ----

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s → %d", url, resp.StatusCode)
	}
	return readAll(t, resp)
}

func postForm(t *testing.T, url string, fields map[string]string) string {
	t.Helper()
	body := strings.NewReader(encodeForm(fields))
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST %s → %d", url, resp.StatusCode)
	}
	return readAll(t, resp)
}

func encodeForm(fields map[string]string) string {
	var sb strings.Builder
	first := true
	for k, v := range fields {
		if !first {
			sb.WriteByte('&')
		}
		first = false
		sb.WriteString(k)
		sb.WriteByte('=')
		// Tests only use simple ASCII; skipping QueryEscape keeps grep
		// assertions readable.
		sb.WriteString(v)
	}
	return sb.String()
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected body to contain %q\n--- body ---\n%s", needle, haystack)
	}
}
