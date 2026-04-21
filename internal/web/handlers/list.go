package handlers

import (
	"net/http"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// listLimit is the default page size for the home list. v0.2 keeps it
// simple (single fetch, no cursor) — load-more pagination lands later.
const listLimit = 50

// List handles GET / — renders the most recent entries.
//
// Replaces the stub in stubs.go (Go's method-set rules let one method
// definition win when stubs.go's stub is removed; we delete its entry
// in this commit). Until then the build error is the safety net telling
// us the migration is incomplete.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	entries, err := h.store.Recent(ctx, listLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stats, err := h.store.ComputeStats(ctx, store.StatsRange{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.List(entries, stats.Total).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
