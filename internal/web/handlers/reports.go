package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// Stats handles GET /stats — counts dashboard, optionally filtered to
// a date range via ?from=YYYY-MM-DD&to=YYYY-MM-DD query params.
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rng := parseStatsRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	s, err := h.store.ComputeStats(ctx, rng)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Stats(s).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// parseStatsRange turns raw from/to query params into a StatsRange.
// Empty / malformed inputs yield a zero-value range (no filter), so a
// half-filled form (only From, or only To) still degrades gracefully
// to "all time" rather than 500ing.
//
// Dates are interpreted as local-time midnight. The returned To is
// exclusive — the user-supplied "to=2026-04-21" means "through the end
// of 2026-04-21", which internally is "< 2026-04-22 00:00 local".
func parseStatsRange(fromStr, toStr string) store.StatsRange {
	var rng store.StatsRange
	const layout = "2006-01-02"
	if fromStr != "" {
		if t, err := time.ParseInLocation(layout, fromStr, time.Local); err == nil {
			rng.From = t
		}
	}
	if toStr != "" {
		if t, err := time.ParseInLocation(layout, toStr, time.Local); err == nil {
			// Advance by 24h so the range is inclusive of the end date.
			rng.To = t.AddDate(0, 0, 1)
		}
	}
	// Guard against inverted ranges (From > To). Discard both so the
	// page falls back to all-time instead of returning nothing.
	if !rng.From.IsZero() && !rng.To.IsZero() && rng.From.After(rng.To) {
		return store.StatsRange{}
	}
	return rng
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
	stats, err := h.store.ComputeStats(ctx, store.StatsRange{})
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
