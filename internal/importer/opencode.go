package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// opencode parses the opencode CLI's per-session storage tree at
// ~/.local/share/opencode/storage. Unlike claude-code (one JSONL per
// session) and codex (one rollout JSONL per session), opencode shards
// each session across THREE directories:
//
//	storage/session/<scope>/<sessionID>.json     ← session metadata
//	storage/message/<sessionID>/msg_*.json       ← message envelopes
//	storage/part/<msgID>/prt_*.json              ← per-message parts
//
// Discover returns one path per session metadata file. Parse(file)
// walks the matching message directory in chronological order, and
// for each message gathers the text-typed parts. The Discover/Parse
// abstraction was designed for "one file = one transcript", which
// fits if we treat the session metadata file as the canonical handle.
//
// Watermark behavior: session.json is rewritten by opencode whenever
// a new message lands (time.updated is bumped), so the file mtime is
// a reliable per-session activity proxy.
type opencode struct{}

func init() { Register(opencode{}) }

func (opencode) Name() string        { return "opencode" }
func (opencode) DefaultRoot() string { return expandHome("~/.local/share/opencode/storage") }

// LastKnownVersion is the highest opencode CLI release this parser
// was validated against — set from session.json's "version" field.
func (opencode) LastKnownVersion() string { return "1.1.14" }

// Anchor: assistant messages should carry token usage. opencode writes
// msg.tokens.{input,output,reasoning,cache.{read,write}} on every
// completed assistant turn. Zero across a real session means the
// msg.tokens shape moved.
func (opencode) Anchor(r Record) bool {
	return r.Role == RoleAssistant && r.TokensIn > 0
}

// Discover lists every session metadata file. The "scope" subdir
// (typically "global") is irrelevant to us; we just want the leaves.
func (opencode) Discover(_ context.Context, root string) ([]string, error) {
	sessionRoot := filepath.Join(root, "session")
	var out []string
	err := filepath.WalkDir(sessionRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(p) == ".json" {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// opencodeSession is the shape of session/<scope>/ses_*.json.
type opencodeSession struct {
	ID        string `json:"id"`
	Version   string `json:"version"` // opencode CLI version
	ProjectID string `json:"projectID"`
	Directory string `json:"directory"` // cwd at session creation
	Title     string `json:"title"`     // user-facing session name
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// opencodeMessage is the shape of message/<sessionID>/msg_*.json.
//
// User and assistant messages share most fields but disagree on a few:
//   - user role: model is nested {providerID, modelID}
//   - assistant role: modelID + providerID are top-level
//
// We accept both shapes via the optional Model object.
type opencodeMessage struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionID"`
	Role       string `json:"role"`
	ParentID   string `json:"parentID"`
	Agent      string `json:"agent"`
	Mode       string `json:"mode"`
	ModelID    string `json:"modelID"`    // assistant
	ProviderID string `json:"providerID"` // assistant
	Model      *struct {
		ModelID    string `json:"modelID"`
		ProviderID string `json:"providerID"`
	} `json:"model,omitempty"` // user-style nested
	Path *struct {
		Cwd  string `json:"cwd"`
		Root string `json:"root"`
	} `json:"path,omitempty"`
	Cost   float64 `json:"cost"`
	Tokens *struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens,omitempty"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
}

// opencodePart is the shape of part/<messageID>/prt_*.json. We only
// pull text-typed parts; tool/reasoning/step-start/step-finish parts
// describe the assistant's internal pipeline, not user-facing content.
type opencodePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (opencode) Parse(_ context.Context, sessionPath string, emit func(Record) error) error {
	root := opencodeStorageRoot(sessionPath)
	if root == "" {
		return nil
	}

	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		return err
	}
	var sess opencodeSession
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil
	}
	if sess.ID == "" {
		return nil
	}

	msgDir := filepath.Join(root, "message", sess.ID)
	msgFiles, err := readJSONFiles(msgDir)
	if err != nil {
		// Session has no message directory yet (created-but-empty).
		// Nothing to import; not an error.
		return nil
	}

	// opencode message ids are sortable lexically when prefixed with
	// the same session epoch but we sort by time.created to be safe
	// across renamed/migrated stores.
	type msgEntry struct {
		path string
		raw  []byte
		msg  opencodeMessage
	}
	all := make([]msgEntry, 0, len(msgFiles))
	for _, mf := range msgFiles {
		b, err := os.ReadFile(mf)
		if err != nil {
			continue
		}
		var m opencodeMessage
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		all = append(all, msgEntry{path: mf, raw: b, msg: m})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].msg.Time.Created < all[j].msg.Time.Created })

	for _, m := range all {
		text := readMessageText(root, m.msg.ID)
		if text == "" {
			continue
		}
		ts := time.UnixMilli(m.msg.Time.Created).UTC()

		cwd := sess.Directory
		if m.msg.Path != nil && m.msg.Path.Cwd != "" {
			cwd = m.msg.Path.Cwd
		}
		modelID := m.msg.ModelID
		providerID := m.msg.ProviderID
		if modelID == "" && m.msg.Model != nil {
			modelID = m.msg.Model.ModelID
			providerID = m.msg.Model.ProviderID
		}

		rec := Record{
			Tool:        "opencode",
			ToolVersion: sess.Version,
			SessionID:   sess.ID,
			SessionName: sess.Title,
			CWD:         cwd,
			Text:        text,
			Timestamp:   ts,
			LineHash:    hashFile(m.raw),
			Extras: opencodeExtras(providerID, m.msg.Agent, m.msg.Mode,
				sess.ProjectID, m.msg.Cost),
		}
		switch m.msg.Role {
		case "user":
			rec.Role = RoleUser
		case "assistant":
			rec.Role = RoleAssistant
			rec.Model = modelID
			if m.msg.Tokens != nil {
				// reasoning tokens are billed as output (matches codex
				// folding), so we sum them in.
				rec.TokensIn = m.msg.Tokens.Input
				rec.TokensOut = m.msg.Tokens.Output + m.msg.Tokens.Reasoning
				rec.TokensCacheRead = m.msg.Tokens.Cache.Read
				rec.TokensCacheWrite = m.msg.Tokens.Cache.Write
			}
		default:
			continue
		}
		if err := emit(rec); err != nil {
			return err
		}
	}
	return nil
}

// readMessageText concatenates every text-typed part for a message.
// Returns "" if there are no parts or no text-typed ones.
func readMessageText(root, messageID string) string {
	dir := filepath.Join(root, "part", messageID)
	files, err := readJSONFiles(dir)
	if err != nil {
		return ""
	}
	// Sort to keep multi-part text deterministic.
	sort.Strings(files)
	var out string
	for _, pf := range files {
		b, err := os.ReadFile(pf)
		if err != nil {
			continue
		}
		var p opencodePart
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		if p.Type != "text" || p.Text == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += p.Text
	}
	return out
}

// readJSONFiles lists *.json children of dir. Returns an empty slice
// (not an error) when the directory is missing.
func readJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out, nil
}

// opencodeStorageRoot recovers the storage root from a session path
// like ".../storage/session/global/ses_xxx.json" → ".../storage". Used
// to relocate the matching message + part trees.
func opencodeStorageRoot(sessionPath string) string {
	dir := filepath.Dir(sessionPath)        // .../storage/session/global
	parent := filepath.Dir(dir)             // .../storage/session
	if filepath.Base(parent) != "session" { // sanity check
		return ""
	}
	return filepath.Dir(parent) // .../storage
}

// opencodeExtras packs opencode-specific provenance into the Record
// extras map. session.title is intentionally NOT here — it goes into
// the entry's session_name column directly so search / sessions list
// pick it up natively.
func opencodeExtras(provider, agent, mode, projectID string, cost float64) map[string]any {
	m := map[string]any{}
	addNonEmpty := func(k, v string) {
		if v != "" {
			m["opencode."+k] = v
		}
	}
	addNonEmpty("provider_id", provider)
	addNonEmpty("agent", agent)
	addNonEmpty("mode", mode)
	addNonEmpty("project_id", projectID)
	if cost > 0 {
		m["opencode.cost_usd"] = cost
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// hashFile is the SHA-256 hex of an entire JSON file's bytes — the
// per-message idempotency key (opencode files are small enough that
// hashing them in full is the simplest stable identifier).
func hashFile(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
