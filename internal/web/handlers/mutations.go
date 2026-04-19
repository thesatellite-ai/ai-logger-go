package handlers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/form/v4"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// formDecoder is the shared go-playground/form decoder. Caching it avoids
// re-building the type-tag map on every request — it's safe for
// concurrent use after construction.
var formDecoder = form.NewDecoder()

// EntryStar handles POST /entry/{id}/star — toggles the starred bit
// and returns the re-rendered StarButton fragment.
func (h *Handlers) EntryStar(w http.ResponseWriter, r *http.Request) {
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
	if err := h.store.SetStarred(ctx, id, !e.Starred); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Re-fetch so the rendered fragment reflects the new state.
	e2, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.StarButton(e2).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// tagInput is the typed form payload for the tag editor. The "tags"
// field is a comma-separated list; merging happens server-side so a
// repeated tag doesn't create dupes.
type tagInput struct {
	Tags string `form:"tags"`
}

// EntryTag handles POST /entry/{id}/tag — merges the new tags into the
// existing CSV and returns the re-rendered TagsEditor fragment.
func (h *Handlers) EntryTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := h.store.ResolveIDPrefix(ctx, chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in tagInput
	if err := formDecoder.Decode(&in, r.Form); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	merged := mergeTagsCSV(e.Tags, in.Tags)
	if err := h.store.SetTags(ctx, id, merged); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	e2, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TagsEditor(e2).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// notesInput is the typed form payload for the notes editor.
type notesInput struct {
	Notes string `form:"notes"`
}

// EntryNotes handles POST /entry/{id}/notes — replaces the notes
// column verbatim and re-renders the editor.
func (h *Handlers) EntryNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := h.store.ResolveIDPrefix(ctx, chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in notesInput
	if err := formDecoder.Decode(&in, r.Form); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.SetNotes(ctx, id, in.Notes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	e2, err := h.store.GetByID(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.NotesEditor(e2).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// mergeTagsCSV merges two CSV tag strings: trimmed, deduped, sorted.
// Same logic as internal/cli/tag.go's mergeTags — duplicated here on
// purpose so the web layer doesn't import the cli package. If we add
// a third caller, promote this to a shared helper.
func mergeTagsCSV(existing, incoming string) string {
	set := map[string]struct{}{}
	for _, t := range splitTags(existing) {
		set[t] = struct{}{}
	}
	for _, t := range splitTags(incoming) {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// splitTags is the local copy of the CSV splitter — see mergeTagsCSV's
// note on duplication.
func splitTags(csv string) []string {
	parts := strings.Split(csv, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
