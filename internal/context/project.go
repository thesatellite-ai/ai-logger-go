package context

import (
	"regexp"
	"strings"
)

// CanonicalRepo splits a raw remote URL into ("github.com", "owner", "repo").
// Empty strings are returned when a component can't be parsed.
// Accepts the common forms:
//
//	git@github.com:owner/repo.git
//	https://github.com/owner/repo.git
//	https://github.com/owner/repo
//	ssh://git@github.com/owner/repo.git
func CanonicalRepo(remote string) (host, owner, name string) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return
	}
	// Strip trailing ".git"
	remote = strings.TrimSuffix(remote, ".git")

	// scp-like: git@host:owner/repo
	if m := reSCP.FindStringSubmatch(remote); m != nil {
		return m[1], m[2], m[3]
	}
	// URL-like: scheme://[user@]host/owner/repo
	if m := reURL.FindStringSubmatch(remote); m != nil {
		return m[1], m[2], m[3]
	}
	return
}

var (
	reSCP = regexp.MustCompile(`^[^@]+@([^:]+):([^/]+)/(.+)$`)
	reURL = regexp.MustCompile(`^[a-z]+://(?:[^@]+@)?([^/]+)/([^/]+)/(.+)$`)
)

// CanonicalProject returns "host/owner/repo" or "" when the remote is unparseable.
func CanonicalProject(remote string) string {
	host, owner, name := CanonicalRepo(remote)
	if host == "" || owner == "" || name == "" {
		return ""
	}
	return host + "/" + owner + "/" + name
}
