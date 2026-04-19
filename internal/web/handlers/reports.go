package handlers

import (
	"net/http"
	"sort"

	"github.com/khanakia/ai-logger/internal/web/views"
)

// Stats handles GET /stats — counts dashboard.
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s, err := h.store.ComputeStats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Stats(s).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Templates handles GET /templates — starred entries.
func (h *Handlers) Templates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := h.store.All(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	starred := all[:0] // reuse backing array — we don't need `all` again
	for _, e := range all {
		if e.Starred {
			starred = append(starred, e)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Templates(starred).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Projects handles GET /projects — distinct projects with entry counts,
// sorted descending. Derived from the same scan that powers Stats.
func (h *Handlers) Projects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := h.store.ComputeStats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]views.ProjectRow, 0, len(stats.ByProject))
	for p, n := range stats.ByProject {
		rows = append(rows, views.ProjectRow{Project: p, Count: n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Project < rows[j].Project
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Projects(rows).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
