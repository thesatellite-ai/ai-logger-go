// Package context collects everything ailog needs to know about the
// current invocation environment — cwd, git, shell, terminal, session —
// into a single Context struct the store can consume directly.
package context

import (
	"context"
	"os"
	"path/filepath"
)

// Context is the full, resolved environment for one ailog invocation.
// Marshalable to JSON for `ailog debug context`.
type Context struct {
	CWD     string      `json:"cwd"`
	Project string      `json:"project"`
	Git     GitInfo     `json:"git"`
	Env     EnvInfo     `json:"env"`
	Session SessionInfo `json:"session"`
}

// Collect assembles a Context from the current process. Fast (~50ms when
// git is present, <1ms otherwise).
//
// Project resolution priority:
//  1. Canonical "host/owner/repo" parsed from `git config --get
//     remote.origin.url`.
//  2. If no remote or unparseable: the basename of cwd — so entries
//     captured outside a git repo still have something meaningful to
//     group by ("ai-logger" instead of an empty string).
//
// The UI strips the "host/" prefix for display, so a value like
// "github.com/khanakia/ai-logger" shows as "khanakia/ai-logger", and a
// basename-fallback value like "notes" shows as-is.
func Collect(ctx context.Context) Context {
	cwd, _ := os.Getwd()
	g := CollectGit(ctx, cwd)
	e := CollectEnv()
	s := CollectSession()
	project := CanonicalProject(g.Remote)
	if project == "" && cwd != "" {
		project = filepath.Base(cwd)
	}
	return Context{
		CWD:     cwd,
		Project: project,
		Git:     g,
		Env:     e,
		Session: s,
	}
}
