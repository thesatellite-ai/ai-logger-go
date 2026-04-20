package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// Sessions handles GET /sessions — filtered / sorted / paginated list.
//
// Query params:
//
//	q       substring match on session_id | session_name | tool
//	sort    ended_at | started_at | turn_count | session_name
//	dir     asc | desc
//	page    1-indexed page number (default 1)
//	size    rows per page (default 50, capped 200)
//
// HX-Request header → only the #ailog-sessions-list fragment is returned
// so the search input can swap it in place.
func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	qp := r.URL.Query()

	sort := qp.Get("sort")
	dir := qp.Get("dir")
	if dir != "asc" && dir != "desc" {
		dir = "desc"
	}

	size := parseLimit(qp.Get("size"), 20)
	if size > 200 {
		size = 200
	}
	page := parseIntDefault(qp.Get("page"), 1)
	if page < 1 {
		page = 1
	}

	in := store.BrowseSessionsInput{
		Query:  strings.TrimSpace(qp.Get("q")),
		Sort:   sort,
		Dir:    dir,
		Limit:  size,
		Offset: (page - 1) * size,
	}

	summaries, total, err := h.store.BrowseSessions(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Clamp stale page URLs to the last real page.
	if total > 0 {
		maxPage := (total + size - 1) / size
		if page > maxPage {
			page = maxPage
			in.Offset = (page - 1) * size
			summaries, _, err = h.store.BrowseSessions(ctx, in)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if r.Header.Get("HX-Request") == "true" {
		if err := views.SessionsList(summaries).Render(ctx, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	data := views.SessionsData{
		Summaries: summaries,
		Query:     in.Query,
		Sort:      sort,
		Dir:       dir,
		Page:      page,
		PageSize:  size,
		Total:     total,
	}
	if err := views.Sessions(data).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SessionDetail handles GET /session/{id} — threaded conversation view.
func (h *Handlers) SessionDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := chi.URLParam(r, "id")
	entries, err := h.store.SessionEntries(ctx, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Resolve display name from the latest non-empty value.
	var name string
	for _, e := range entries {
		if e.SessionName != "" {
			name = e.SessionName
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.SessionDetail(sessionID, name, entries).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SessionRename handles POST /session/{id}/name with form field "name".
// Returns the same form re-rendered (htmx outerHTML swap).
func (h *Handlers) SessionRename(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if _, err := h.store.RenameSession(ctx, sessionID, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Re-render just the form so htmx can swap it in place.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.SessionNameForm(sessionID, name).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
