package web

import (
	"github.com/go-chi/chi/v5"
	"github.com/khanakia/ai-logger/internal/web/handlers"
)

// All path templates live here so the router and the templ url builders
// can't drift. When you add a route, define it here and reference it
// from both sides.
//
// Conventions:
//   - HTML page paths use plain words: "/", "/sessions", "/stats"
//   - htmx fragment paths end in /partial: "/search/partial"
//   - mutation paths read POST: "/entry/{id}/star"
//   - JSON/data paths end in .json: "/entry/{id}.json"
const (
	PathHealthz       = "/healthz"
	PathHome          = "/"
	PathTable         = "/table"
	PathEntry         = "/entry/{id}"
	PathEntryJSON     = "/entry/{id}.json"
	PathEntryStar     = "/entry/{id}/star"
	PathEntryTag      = "/entry/{id}/tag"
	PathEntryNotes    = "/entry/{id}/notes"
	PathSearch        = "/search"
	PathSearchPartial = "/search/partial"
	PathSessions      = "/sessions"
	PathSession       = "/session/{id}"
	PathSessionRename = "/session/{id}/name"
	PathProjects      = "/projects"
	PathTemplates     = "/templates"
	PathStats         = "/stats"
)

// mountRoutes wires every handler. Keep the order matching the consts
// above so additions stay easy to audit.
func mountRoutes(r chi.Router, h *handlers.Handlers) {
	r.Get(PathHealthz, h.Healthz)
	r.Get(PathHome, h.List)
	r.Get(PathTable, h.Table)
	r.Get(PathEntry, h.EntryDetail)
	r.Get(PathEntryJSON, h.EntryJSON)
	r.Post(PathEntryStar, h.EntryStar)
	r.Post(PathEntryTag, h.EntryTag)
	r.Post(PathEntryNotes, h.EntryNotes)
	r.Get(PathSearch, h.Search)
	r.Get(PathSearchPartial, h.SearchPartial)
	r.Get(PathSessions, h.Sessions)
	r.Get(PathSession, h.SessionDetail)
	r.Post(PathSessionRename, h.SessionRename)
	r.Get(PathProjects, h.Projects)
	r.Get(PathTemplates, h.Templates)
	r.Get(PathStats, h.Stats)
}

// URL builders — used by templ views to construct paths without
// hand-formatting them. Keeps id encoding consistent and makes
// route renames a one-place edit.

// URLEntry builds the detail URL for an entry id.
func URLEntry(id string) string { return "/entry/" + id }

// URLEntryStar builds the star-toggle endpoint for an entry id.
func URLEntryStar(id string) string { return "/entry/" + id + "/star" }

// URLEntryTag builds the tag-edit endpoint for an entry id.
func URLEntryTag(id string) string { return "/entry/" + id + "/tag" }

// URLEntryNotes builds the notes-edit endpoint for an entry id.
func URLEntryNotes(id string) string { return "/entry/" + id + "/notes" }

// URLSession builds the session view URL for a session id.
func URLSession(id string) string { return "/session/" + id }

// URLSessionRename builds the session-name endpoint for a session id.
func URLSessionRename(id string) string { return "/session/" + id + "/name" }
