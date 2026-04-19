package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// EntryDetail handles GET /entry/{id}. The id may be a 13-char prefix
// (or longer); resolution lives in the store.
func (h *Handlers) EntryDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := h.store.ResolveIDPrefix(ctx, chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	e, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	nav := h.adjacentInSession(ctx, e.SessionID, e.ID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.EntryDetail(e, nav).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// EntryJSON handles GET /entry/{id}.json — full entry as JSON for
// agents / scripting. Same id resolution as the HTML view.
func (h *Handlers) EntryJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := h.store.ResolveIDPrefix(ctx, chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	e, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(e); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// adjacentInSession returns the prev/next entry ids in a session's
// turn-ordered slice. Empty strings when the entry has no session, or
// when it's at the head/tail. Errors are swallowed — the detail page
// still renders fine without nav links.
func (h *Handlers) adjacentInSession(ctx ctxLike, sessionID, currentID string) views.EntryNav {
	if sessionID == "" {
		return views.EntryNav{}
	}
	siblings, err := h.store.SessionEntries(asContext(ctx), sessionID)
	if err != nil {
		return views.EntryNav{}
	}
	for i, s := range siblings {
		if s.ID != currentID {
			continue
		}
		var nav views.EntryNav
		if i > 0 {
			nav.PrevID = siblings[i-1].ID
		}
		if i+1 < len(siblings) {
			nav.NextID = siblings[i+1].ID
		}
		return nav
	}
	return views.EntryNav{}
}
