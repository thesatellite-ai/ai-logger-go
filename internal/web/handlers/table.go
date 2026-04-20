package handlers

import (
	"net/http"
	"strconv"
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
//	page      1-indexed page number (default 1)
//	size      rows per page (default 100, capped 500)
//
// Honors HX-Request: when set, returns just the <tbody> fragment so
// the filter inputs' htmx swap can replace it in place.
func (h *Handlers) Table(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	qp := r.URL.Query()

	sort := qp.Get("sort")
	dir := qp.Get("dir")
	if dir != "asc" && dir != "desc" {
		dir = "desc"
	}

	filters := collectFilterParams(qp)

	size := parseLimit(qp.Get("size"), 20)
	if size > 500 {
		size = 500
	}
	page := parseIntDefault(qp.Get("page"), 1)
	if page < 1 {
		page = 1
	}

	in := store.BrowseInput{
		Sort:    sort,
		Dir:     dir,
		Filters: filters,
		Limit:   size,
		Offset:  (page - 1) * size,
	}

	total, err := h.store.BrowseCount(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Clamp the page number to the last real page after counting — a
	// stale bookmarked URL asking for page 99 on a 3-page dataset
	// should quietly show page 3 rather than an empty page 99.
	if total > 0 {
		maxPage := (total + size - 1) / size
		if page > maxPage {
			page = maxPage
			in.Offset = (page - 1) * size
		}
	}

	entries, err := h.store.Browse(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// htmx swap — just the tbody. Pager stays stale until full reload,
	// which is a deliberate trade-off: swapping the pager too would
	// cost an oob response and add complexity.
	if r.Header.Get("HX-Request") == "true" {
		if err := views.TableBody(entries).Render(ctx, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	data := views.TableData{
		Entries:  entries,
		Sort:     sort,
		Dir:      dir,
		Filters:  filters,
		Page:     page,
		PageSize: size,
		Total:    total,
	}
	if err := views.TablePage(data).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// collectFilterParams extracts every `f_<key>=value` from the URL into
// a flat map keyed by `<key>`. Empty values are dropped so Browse's
// filter logic doesn't run no-op Contains matches on them.
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

// parseIntDefault is a tiny strconv wrapper — invalid input falls back
// to the default instead of erroring.
func parseIntDefault(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
