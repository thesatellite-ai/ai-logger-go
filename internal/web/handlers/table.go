package handlers

import (
	"net/http"
	"strings"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/views"
)

// Table handles GET /table — the full datagrid.
//
// Query params:
//
//	sort      ent field name; unknown → defaults to created_at
//	dir       "asc" | "desc" (default desc)
//	f_<key>   per-column filter value, e.g. f_tool=claude-code
//	limit     page size (default 500, capped at 2000)
//
// Honors HX-Request: when set, returns just the <tbody> fragment so
// the filter form's htmx swap can replace it in place.
func (h *Handlers) Table(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	qp := r.URL.Query()

	sort := qp.Get("sort")
	dir := qp.Get("dir")
	if dir != "asc" && dir != "desc" {
		dir = "desc"
	}

	filters := collectFilterParams(qp)

	entries, err := h.store.Browse(ctx, store.BrowseInput{
		Sort:    sort,
		Dir:     dir,
		Filters: filters,
		Limit:   parseLimit(qp.Get("limit"), 500),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// htmx swap — just the tbody.
	if r.Header.Get("HX-Request") == "true" {
		if err := views.TableBody(entries).Render(ctx, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Full page render.
	data := views.TableData{
		Entries: entries,
		Sort:    sort,
		Dir:     dir,
		Filters: filters,
	}
	if err := views.TablePage(data).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// collectFilterParams extracts every `f_<key>=value` from the URL into
// a flat map keyed by `<key>`. Empty values are dropped so the Browse
// filter logic doesn't do no-op Contains matches on them.
func collectFilterParams(qp map[string][]string) map[string]string {
	out := map[string]string{}
	for k, vs := range qp {
		if !strings.HasPrefix(k, "f_") || len(vs) == 0 {
			continue
		}
		v := strings.TrimSpace(vs[0])
		if v == "" {
			continue
		}
		out[strings.TrimPrefix(k, "f_")] = v
	}
	return out
}
