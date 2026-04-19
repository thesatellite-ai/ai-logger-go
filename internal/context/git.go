package context

import (
	"context"
	"os/exec"
	"strings"
)

// GitInfo is everything we can learn from a git working tree at `cwd`.
// All fields empty if cwd is not inside a git repo or git is missing.
type GitInfo struct {
	Remote string // raw remote.origin.url
	Branch string
	Commit string // short sha
	Host   string // parsed from Remote
	Owner  string
	Name   string
}

// CollectGit runs 3 short git commands in the working directory. Any
// failure yields empty fields rather than an error — missing git context
// is a normal state.
func CollectGit(ctx context.Context, cwd string) GitInfo {
	var g GitInfo
	g.Remote = runGit(ctx, cwd, "config", "--get", "remote.origin.url")
	g.Branch = runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	g.Commit = runGit(ctx, cwd, "rev-parse", "--short", "HEAD")
	if g.Remote != "" {
		g.Host, g.Owner, g.Name = CanonicalRepo(g.Remote)
	}
	return g
}

func runGit(ctx context.Context, cwd string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
