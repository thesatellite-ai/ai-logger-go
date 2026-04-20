// Package render produces plain-text representations of entries /
// sessions for export. Separate from internal/web/views (which targets
// HTML via templ) and from internal/cli/export.go (which handles batch
// dumps) — this package is the shared primitive those call into when
// they need a canonical Markdown rendering.
//
// A future `ailog export --session <id>` should route through
// SessionMarkdown too, so the web-download and CLI-export outputs stay
// byte-identical.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/ent"
)

// SessionMarkdown renders a whole session (ordered by turn_index) as a
// single Markdown document with YAML frontmatter.
//
// Frontmatter carries session-level metadata (id, name, tool, project,
// first/last turn time, turn count, aggregate token usage). Each turn
// renders as two H2 sections — "Turn N · You" + "Turn N · Assistant"
// — separated by `---` rules so the output reads well in any markdown
// viewer (Obsidian, GitHub, plain preview) while staying parseable.
//
// Model id is stamped on the assistant header only; per-turn tokens
// are deliberately omitted to keep the document readable. The
// frontmatter carries the aggregate.
func SessionMarkdown(sessionID, sessionName string, entries []*ent.Entry) []byte {
	var b strings.Builder

	meta := summarize(sessionID, sessionName, entries)
	writeFrontmatter(&b, meta)

	// Top-of-body human-readable title — redundant with frontmatter for
	// readers that don't parse it, but useful for anyone previewing.
	if sessionName != "" {
		fmt.Fprintf(&b, "# %s\n\n", sessionName)
	} else {
		fmt.Fprintf(&b, "# Session %s\n\n", shortID(sessionID))
	}

	for i, e := range entries {
		writeTurn(&b, i+1, e)
		if i < len(entries)-1 {
			b.WriteString("\n---\n\n")
		}
	}
	return []byte(b.String())
}

// sessionMeta is the frontmatter payload. Fields default to zero when
// the data is unavailable — the writer skips zeroed fields so the
// output stays tight.
type sessionMeta struct {
	ID         string
	Name       string
	Tool       string
	Project    string
	FirstAt    time.Time
	LastAt     time.Time
	TurnCount  int
	TokensIn   int
	TokensOut  int
	TokensCR   int // cache read
	ModelSeen  []string
}

func summarize(sessionID, sessionName string, entries []*ent.Entry) sessionMeta {
	m := sessionMeta{
		ID:        sessionID,
		Name:      sessionName,
		TurnCount: len(entries),
	}
	seenModel := map[string]bool{}
	for i, e := range entries {
		if m.Tool == "" && e.Tool != "" {
			m.Tool = e.Tool
		}
		if m.Project == "" && e.Project != "" {
			m.Project = e.Project
		}
		if e.Model != "" && !seenModel[e.Model] {
			seenModel[e.Model] = true
			m.ModelSeen = append(m.ModelSeen, e.Model)
		}
		m.TokensIn += e.TokenCountIn
		m.TokensOut += e.TokenCountOut
		m.TokensCR += e.TokenCountCacheRead
		if i == 0 || e.CreatedAt.Before(m.FirstAt) {
			m.FirstAt = e.CreatedAt
		}
		if e.CreatedAt.After(m.LastAt) {
			m.LastAt = e.CreatedAt
		}
	}
	return m
}

// writeFrontmatter emits a YAML block. Empty / zero-valued fields are
// skipped so the frontmatter doesn't carry noise like `model: ""`.
func writeFrontmatter(b *strings.Builder, m sessionMeta) {
	b.WriteString("---\n")
	kv := func(k, v string) {
		if v == "" {
			return
		}
		fmt.Fprintf(b, "%s: %s\n", k, yamlQuote(v))
	}
	kvi := func(k string, v int) {
		if v == 0 {
			return
		}
		fmt.Fprintf(b, "%s: %d\n", k, v)
	}
	kvTime := func(k string, t time.Time) {
		if t.IsZero() {
			return
		}
		fmt.Fprintf(b, "%s: %s\n", k, t.UTC().Format(time.RFC3339))
	}

	kv("session_id", m.ID)
	kv("session_name", m.Name)
	kv("tool", m.Tool)
	kv("project", m.Project)
	kvTime("first_turn_at", m.FirstAt)
	kvTime("last_turn_at", m.LastAt)
	kvi("turn_count", m.TurnCount)
	kvi("tokens_in", m.TokensIn)
	kvi("tokens_out", m.TokensOut)
	kvi("tokens_cache_read", m.TokensCR)
	if len(m.ModelSeen) > 0 {
		fmt.Fprintf(b, "models: [%s]\n", strings.Join(quoteEach(m.ModelSeen), ", "))
	}
	b.WriteString("---\n\n")
}

// writeTurn emits one turn as two H2 sections: You (prompt) + Assistant
// (response, if present). Turn index is 1-based in the display even
// though entries use 0-based turn_index internally.
func writeTurn(b *strings.Builder, displayIndex int, e *ent.Entry) {
	fmt.Fprintf(b, "## Turn %d · You\n\n", displayIndex)
	b.WriteString(strings.TrimRight(e.Prompt, "\n"))
	b.WriteString("\n\n")

	if e.Response == "" {
		return
	}
	if e.Model != "" {
		fmt.Fprintf(b, "## Turn %d · Assistant · `%s`\n\n", displayIndex, e.Model)
	} else {
		fmt.Fprintf(b, "## Turn %d · Assistant\n\n", displayIndex)
	}
	b.WriteString(strings.TrimRight(e.Response, "\n"))
	b.WriteString("\n")
}

// SessionFilename returns the suggested download filename for a
// session, slugifying the name when available and falling back to a
// short-id-only form. Format: `<slug>-<shortid>.md` or
// `ailog-<shortid>.md`. Always lowercase, always ends in `.md`.
func SessionFilename(sessionID, sessionName string) string {
	short := shortID(sessionID)
	slug := slugify(sessionName)
	if slug == "" {
		return "ailog-" + short + ".md"
	}
	return slug + "-" + short + ".md"
}

// slugify turns a human-readable name into a filesystem-safe slug.
// Kept deliberately minimal — ASCII-lowercase, dashes for separators,
// drop the rest. Non-ASCII input degrades to an empty string, which
// the caller handles via the fallback filename.
func slugify(s string) string {
	var out []byte
	lastDash := true // treat start as "just saw a separator" so we don't lead with '-'
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
			lastDash = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			out = append(out, c)
			lastDash = false
		default:
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	// Trim trailing dash.
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	// Cap length so absurd names don't produce 200-char filenames.
	const maxLen = 60
	if len(out) > maxLen {
		out = out[:maxLen]
		// don't leave a trailing partial word cut at a dash boundary
		for len(out) > 0 && out[len(out)-1] == '-' {
			out = out[:len(out)-1]
		}
	}
	return string(out)
}

// shortID is a local 8-char prefix of a session id (first block of a
// UUID, first 8 chars of any other format). Just for filename
// disambiguation — not used for lookups.
func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i >= 0 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// yamlQuote minimally escapes a value for YAML single-line emission.
// Safe for the values we pass (session names, ids, tool strings).
// If the string contains a special YAML character, wrap in double
// quotes and escape embedded quotes / backslashes.
func yamlQuote(s string) string {
	needsQuote := false
	for _, c := range s {
		if c == ':' || c == '#' || c == '"' || c == '\'' || c == '\\' || c == '\n' || c == '\r' {
			needsQuote = true
			break
		}
	}
	if !needsQuote && strings.TrimSpace(s) == s {
		return s
	}
	// Double-quote form with minimal escaping.
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// quoteEach wraps each string in yamlQuote. For list rendering.
func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = yamlQuote(s)
	}
	return out
}

