// Package context collects everything ailog needs to know about the
// current invocation environment — cwd, git, shell, terminal, session —
// into a single Context struct the store can consume directly.
package context

import (
	"context"
	"os"
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
func Collect(ctx context.Context) Context {
	cwd, _ := os.Getwd()
	g := CollectGit(ctx, cwd)
	e := CollectEnv()
	s := CollectSession()
	project := CanonicalProject(g.Remote)
	return Context{
		CWD:     cwd,
		Project: project,
		Git:     g,
		Env:     e,
		Session: s,
	}
}
