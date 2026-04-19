package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// Sessions handles GET /sessions — list every distinct session.
func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	summaries, err := h.store.Sessions(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Sessions(summaries).Render(ctx, w); err != nil {
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
