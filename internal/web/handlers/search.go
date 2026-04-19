package handlers

import (
	"net/http"
	"strconv"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// searchLimit caps the result set per request. Same value for both
// page and partial — no need for separate knobs at v0.2.
const searchLimit = 50

// Search handles GET /search?q=… — the full standalone page, suitable
// for bookmarking or linking. Renders nothing-found when q is empty,
// not an error.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query().Get("q")
	entries := []*store.Entry(nil)
	if query != "" {
		var err error
		entries, err = h.store.Search(ctx, query, store.SearchFilter{
			Project:   r.URL.Query().Get("project"),
			Tool:      r.URL.Query().Get("tool"),
			SessionID: r.URL.Query().Get("session"),
			Branch:    r.URL.Query().Get("branch"),
			Limit:     parseLimit(r.URL.Query().Get("limit"), searchLimit),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
	query := r.URL.Query().Get("q")
	entries := []*store.Entry(nil)
	if query != "" {
		var err error
		entries, err = h.store.Search(ctx, query, store.SearchFilter{
			Project:   r.URL.Query().Get("project"),
			Tool:      r.URL.Query().Get("tool"),
			SessionID: r.URL.Query().Get("session"),
			Branch:    r.URL.Query().Get("branch"),
			Limit:     parseLimit(r.URL.Query().Get("limit"), searchLimit),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.SearchResults(query, entries).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
