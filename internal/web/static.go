package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// staticFS is the embedded asset bundle: htmx, custom CSS.
// Tailwind is loaded from the CDN play script in the layout for v0.2;
// v0.3 will swap in a precomputed CSS file.
//
//go:embed static/*
var staticFS embed.FS

// mountStatic serves /static/* from the embedded FS. The fs.Sub strips
// the leading "static/" prefix so requests look natural.
func mountStatic(r chi.Router) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// embed declared above guarantees this exists at compile time;
		// a panic here means the binary was tampered with.
		panic("web: static FS subtree missing: " + err.Error())
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
}
