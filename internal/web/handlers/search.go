package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// searchLimit caps the result set per request. Same value for both
// page and partial — no need for separate knobs at v0.2.
const searchLimit = 50

// Search handles GET /search?q=… — the full standalone page, suitable
// for bookmarking or linking. Empty q with structured filters
// (project / tool / session / branch) returns "everything matching the
// filters" — supports the project/sessions leaderboard links.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query, entries, err := h.runSearch(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Search(query, entries).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SearchPartial handles GET /search/partial — the htmx fragment
// returned to the search input as the user types. Same data, same
// renderer, but layout-less so it can swap into a parent.
func (h *Handlers) SearchPartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query, entries, err := h.runSearch(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.SearchResults(query, entries).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// runSearch is the shared resolver — both Search and SearchPartial use
// the same input parsing + store call, only the renderer differs.
//
// The empty-query path is intentional: with no `q` and no filters, we
// return nil so the view shows the "type to search" hint. With no `q`
// but ANY filter, we fall through to the store's filter-only path.
func (h *Handlers) runSearch(ctx ctxLike, r *http.Request) (string, []*store.Entry, error) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	// "*" is the conventional "match everything" — FTS5 doesn't accept
	// it as a query, so we transparently demote to filter-only mode.
	if query == "*" {
		query = ""
	}
	f := store.SearchFilter{
		Project:   r.URL.Query().Get("project"),
		Tool:      r.URL.Query().Get("tool"),
		SessionID: r.URL.Query().Get("session"),
		Branch:    r.URL.Query().Get("branch"),
		Limit:     parseLimit(r.URL.Query().Get("limit"), searchLimit),
	}
	if query == "" && !anyFilterSet(f) {
		return query, nil, nil
	}
	entries, err := h.store.Search(asContext(ctx), query, f)
	return query, entries, err
}

// anyFilterSet reports whether at least one structured filter is non-zero.
// Used to decide whether an empty `q` should return all-filtered or
// the empty-state hint.
func anyFilterSet(f store.SearchFilter) bool {
	return f.Project != "" || f.Tool != "" || f.SessionID != "" || f.Branch != ""
}

// parseLimit accepts a string from a query param and falls back to
// fallback when empty / invalid / non-positive.
func parseLimit(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
