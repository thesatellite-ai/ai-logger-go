// Package web is the embedded HTML UI for ailog. It serves a small,
// localhost-by-default site over HTTP. Templates are written in templ
// (compiled to Go; see views/), and interactivity is handled with htmx
// — no client-side build step, no Node, no SPA bundle.
//
// The package layout:
//
//	server.go       — http.Server setup + chi router wiring
//	render.go       — markdown + syntax-highlight helpers
//	routes.go       — single source of truth for paths (used by both the
//	                  router and templ url builders)
//	handlers/       — one file per resource group
//	views/          — templ files (.templ → generated _templ.go)
//	static/         — embedded htmx + CSS
package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/khanakia/ai-logger/internal/web/handlers"
)

// Server is the long-lived HTTP server. Construct with New, run with Run.
type Server struct {
	store *store.Store
	addr  string
	srv   *http.Server
}

// New builds the chi router, registers handlers, and returns a Server
// ready to Run. addr is a host:port (e.g. "127.0.0.1:8090"). Bind to
// 127.0.0.1 by default — only the caller should opt into network
// exposure.
func New(s *store.Store, addr string) *Server {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(30 * time.Second))

	h := handlers.New(s)
	mountRoutes(r, h)
	mountStatic(r)

	return &Server{
		store: s,
		addr:  addr,
		srv: &http.Server{
			Addr:              addr,
			Handler:           r,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Run starts the HTTP listener and blocks until Shutdown or an error.
// http.ErrServerClosed is treated as a normal shutdown and returned as nil.
func (s *Server) Run() error {
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	return nil
}

// RunListener serves on a pre-created listener. Use this when the
// caller needs to control the bind (e.g. `:0` for OS-assigned ports)
// and wants the listener handed to http.Server without a re-bind —
// avoids the TOCTOU race of "probe then listen".
func (s *Server) RunListener(l net.Listener) error {
	if err := s.srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve %s: %w", l.Addr().String(), err)
	}
	return nil
}

// Shutdown gracefully stops the server, draining in-flight requests up
// to the supplied context's deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Addr returns the bound address (useful when the caller passed :0 to
// let the OS pick a port).
func (s *Server) Addr() string {
	return s.addr
}

// Handler exposes the wired chi router so tests can build an
// httptest.Server around it without reusing the real listener. Not
// part of the production surface — exists for testing.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}
