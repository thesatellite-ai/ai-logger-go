// Package redact replaces common secret patterns with [redacted] markers.
// Best-effort, not a guarantee — regex-based scrubbers always miss things.
package redact

import "regexp"

var patterns = []*regexp.Regexp{
	// AWS access key id
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// GitHub personal access tokens (classic + fine-grained)
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82,}`),
	// OpenAI API keys
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	// Anthropic API keys
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
	// Slack tokens
	regexp.MustCompile(`xox[abpr]-[A-Za-z0-9-]{10,}`),
	// JWT — three dot-separated base64url segments starting with eyJ
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`),
	// Private key blocks
	regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]+?-----END [A-Z ]+PRIVATE KEY-----`),
}

const marker = "[redacted]"

// Scrub returns s with every match replaced by [redacted].
func Scrub(s string) string {
	if s == "" {
		return s
	}
	for _, p := range patterns {
		s = p.ReplaceAllString(s, marker)
	}
	return s
}
