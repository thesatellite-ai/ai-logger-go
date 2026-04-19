package handlers

import (
	"net/http"

	"github.com/khanakia/ai-logger/internal/web/views"
)

// tableLimit is how many rows the table page renders. Higher than the
// List cap because density is the point — a scan-over-many view.
const tableLimit = 200

// Table handles GET /table — dense tabular view of entries.
func (h *Handlers) Table(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	entries, err := h.store.Recent(ctx, tableLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TablePage(entries).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
