// Package handlers holds the HTTP handler funcs grouped by resource.
// Each handler is a method on Handlers so it has access to the shared
// store and (later) other dependencies. Handlers don't do business
// logic — they read input, call store methods, render templ
// components, and return.
package handlers

import (
	"net/http"

	"github.com/khanakia/ai-logger/internal/store"
)

// Handlers is the dependency container for HTTP handlers. Construct
// once via New and pass into the router.
type Handlers struct {
	store *store.Store
}

// New returns a Handlers ready to wire into chi.
func New(s *store.Store) *Handlers {
	return &Handlers{store: s}
}

// Healthz is the liveness probe used by the local launcher to verify
// the server bound successfully. Always 200, no DB hit.
func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
