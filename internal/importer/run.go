package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/khanakia/ai-logger/internal/redact"
	"github.com/khanakia/ai-logger/internal/store"
)

// Options controls one Run() invocation. Zero-value defaults are sane:
// every home of the agent, no since-cutoff, no limit.
type Options struct {
	// Home imports only the agent home at this folder (`--from`). A
	// folder inside a home that is not a home itself (an older ailog's
	// transcript root such as ~/.claude/projects) is taken as its parent
	// home. "" = every home agentio finds.
	Home  string
	Since time.Time // skip records strictly older than this
	Limit int       // stop after N records (0 = no cap)
	Force bool      // ignore watermarks and cursors; read every transcript from the start

	// Strict=true escalates the format-drift warning to a hard reject:
	// the transcript's watermark is not stamped and the path is printed
	// with an error tag. Off by default — most users want best-effort
	// imports.
	Strict bool

	// Verbose=true prints "imported <transcript> (n records)" per file to Out.
	Verbose bool
	Out     io.Writer

	// projectCache maps cwd → resolved git context. Populated lazily by
	// resolveProject so we don't shell out to git for every record (many
	// records share a cwd; transcripts can be long).
	projectCache map[string]projectInfo
}

// projectInfo is the per-cwd cached resolution.
type projectInfo struct {
	Project, Owner, Name, Remote, Branch, Commit string
}

// Stats is the per-Run summary returned to the CLI for printing.
type Stats struct {
	Homes          int
	Files          int
	FilesScanned   int
	FilesSkipped   int // skipped via watermark
	Records        int
	RecordsSkipped int // dedup hits (already imported) or filtered out
	// RecordsMatched counts turns found already stored under another
	// key (part of RecordsSkipped).
	RecordsMatched int
	Inserted       int // user-side prompts written to entries
	Attached       int // assistant-side responses paired with prompts
	Standalone     int // assistant Records with no prior user turn
	// Updated counts stored answers replaced by a rewritten copy of the
	// same message (same message id, new bytes).
	Updated int

	// Drift watchdog counters. Files where the anchor check passed at
	// least once are "healthy"; files with answers but zero anchor
	// passes are "suspect" (likely upstream schema drift).
	FilesHealthy int
	FilesSuspect int
}

// Run imports every transcript of agent a into st. Every side effect
// goes through the passed store; the agent is only read. Safely
// re-runnable: the dedup keys make repeated runs idempotent and the
// watermarks make them fast.
func Run(ctx context.Context, st *store.Store, a agents.Agent, opts Options) (Stats, error) {
	var stats Stats
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.projectCache == nil {
		opts.projectCache = map[string]projectInfo{}
	}

	homes, err := importHomes(a, opts.Home)
	if err != nil {
		return stats, err
	}
	stats.Homes = len(homes)
	var all []agents.Transcript
	for _, h := range homes {
		ts, err := a.Transcripts(ctx, h)
		if err != nil {
			return stats, fmt.Errorf("%s: list %s: %w", a.Tool, h.Dir(), err)
		}
		all = append(all, ts...)
	}
	stats.Files = len(all)

	// Per-session prompt the next answer of this run attaches to,
	// mirroring the live hooks' pairing of a response-less entry with the
	// next assistant message.
	pending := map[string]string{} // sessionID → entry id
	for _, t := range all {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		stop, err := importTranscript(ctx, st, a, t, &opts, &stats, pending)
		if err != nil {
			return stats, err
		}
		if stop {
			return stats, nil
		}
	}
	return stats, nil
}

// importHomes resolves the homes a run reads: the one at dir, or every
// home of the agent. Disk · Read.
func importHomes(a agents.Agent, dir string) ([]agentio.Home, error) {
	if dir == "" {
		hs, err := a.Homes()
		if err != nil {
			return nil, fmt.Errorf("%s: homes: %w", a.Tool, err)
		}
		return hs, nil
	}
	dir = expandHome(dir)
	if ok, err := a.Neutral.IsHome(dir); err == nil && !ok {
		// An older ailog's --from named the transcript root inside the
		// home (~/.claude/projects, ~/.codex/sessions, …/storage).
		if parent := filepath.Dir(dir); parent != dir {
			if ok, err := a.Neutral.IsHome(parent); err == nil && ok {
				dir = parent
			}
		}
	}
	h, err := a.HomeAt(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: --from %s: %w", a.Tool, dir, err)
	}
	return []agentio.Home{h}, nil
}

// importTranscript reads one transcript from its stored cursor and writes
// its new turns. stop=true when --limit was reached.
func importTranscript(ctx context.Context, st *store.Store, a agents.Agent, t agents.Transcript, opts *Options, stats *Stats, pending map[string]string) (stop bool, err error) {
	mtime, size := t.Updated, int64(0)
	if t.Path != "" {
		info, err := os.Stat(t.Path)
		if err != nil {
			fmt.Fprintf(opts.Out, "skip %s: %v\n", t.Key, err)
			return false, nil
		}
		mtime, size = info.ModTime(), info.Size()
	}
	var from agentio.Offset
	if !opts.Force {
		wm, ok, err := st.ImportFileWatermark(ctx, t.Key)
		if err == nil && ok {
			if wm.MtimeUnixNano == mtime.UnixNano() && wm.Size == size {
				stats.FilesSkipped++
				return false, nil
			}
			// A file that shrank was rewritten: its old cursor means
			// nothing, read it from the start (dedup keeps it safe).
			if size >= wm.Size {
				from = agentio.Offset(wm.Offset)
			}
		}
	}
	stats.FilesScanned++

	var (
		fileRecords    int    // records written
		fileAnchorOK   int    // answers that passed the anchor check
		fileAnchorElig int    // answers seen
		fileMaxVer     string // newest agent version seen in this transcript
		limitHit       bool
	)
	annotate := t.Annotator()
	next, readErr := t.Turns(from, func(turn agentio.Turn) error {
		r, ok := recordOf(a, t, turn, annotate(turn))
		if !ok {
			return nil
		}
		stats.Records++
		if opts.Limit > 0 && stats.Records > opts.Limit {
			limitHit = true
			return errStopParsing
		}
		if !opts.Since.IsZero() && !r.Timestamp.IsZero() && r.Timestamp.Before(opts.Since) {
			stats.RecordsSkipped++
			return nil
		}
		// Drift book-keeping counts every answer that flowed past,
		// written or deduplicated, so a re-import keeps the signal.
		if r.Role == RoleAssistant {
			fileAnchorElig++
			if anchor(r) {
				fileAnchorOK++
			}
		}
		if r.ToolVersion != "" && (fileMaxVer == "" || newerThan(r.ToolVersion, fileMaxVer)) {
			fileMaxVer = r.ToolVersion
		}
		written, err := storeRecord(ctx, st, r, opts, stats, pending)
		if written {
			fileRecords++
		}
		return err
	})
	if readErr != nil && !errors.Is(readErr, errStopParsing) {
		fmt.Fprintf(opts.Out, "parse %s: %v\n", t.Key, readErr)
		return false, nil
	}
	if limitHit {
		// The cursor is past a turn that was not stored: keep the old
		// watermark so the next run reads it again.
		return true, nil
	}

	// Drift watchdog: a transcript with answers but none carrying usage.
	// Newer than agentio's tested version + --strict: hard reject (no
	// watermark, so a fixed reader walks the file again).
	suspect := fileAnchorElig > 0 && fileAnchorOK == 0
	newer := fileMaxVer != "" && newerThan(fileMaxVer, a.TestedMax)
	if suspect {
		stats.FilesSuspect++
		tag := "warn"
		if newer && opts.Strict {
			tag = "REJECT"
		} else if newer {
			tag = "WARN(newer-version)"
		}
		fmt.Fprintf(opts.Out,
			"%s %s: %d assistant record(s), 0 carried token usage — possible upstream schema drift (observed %s=%q, agentio tested=%q)\n",
			tag, t.Key, fileAnchorElig, a.Tool, fileMaxVer, a.TestedMax)
		if newer && opts.Strict {
			return false, nil
		}
	} else if fileAnchorElig > 0 || fileRecords > 0 {
		stats.FilesHealthy++
	}

	_ = st.ImportFileMark(ctx, t.Key, mtime, size, int64(next))
	if opts.Verbose && fileRecords > 0 {
		fmt.Fprintf(opts.Out, "imported %s (%d records)\n", t.Key, fileRecords)
	}
	return false, nil
}

// storeRecord writes one record unless it is already stored, and records
// its dedup keys. written=true when a row was inserted or a response
// attached.
func storeRecord(ctx context.Context, st *store.Store, r Record, opts *Options, stats *Stats, pending map[string]string) (written bool, err error) {
	text := redact.Scrub(r.Text)

	// 1. Dedup keys: the record's bytes, then its message id.
	id, ok, err := st.ImportLineEntry(ctx, r.LineHash)
	if err != nil {
		return false, fmt.Errorf("dedup lookup: %w", err)
	}
	if ok {
		recordKeys(ctx, st, r, id)
		stats.RecordsSkipped++
		return false, nil
	}
	if id, ok, err = st.ImportLineEntry(ctx, r.MsgKey); err != nil {
		return false, fmt.Errorf("dedup lookup: %w", err)
	} else if ok {
		recordKeys(ctx, st, r, id)
		if r.Role == RoleAssistant && r.LineHash != "" {
			// Known message, new bytes: the agent rewrote it (Gemini
			// re-appends an answer once its token count is known; a
			// database row is updated in place). The newer copy
			// replaces the stored answer and its usage.
			if err := attachAssistant(ctx, st, id, r, text); err != nil {
				return false, err
			}
			stats.Updated++
			return true, nil
		}
		stats.RecordsSkipped++
		return false, nil
	}
	switch r.Role {
	case RoleUser:
		// 2. Same prompt already stored at the same transcript time.
		if id, ok, err := st.FindImportedEntry(ctx, store.ImportedTurn{
			Tool: r.Tool, SessionID: r.SessionID, CreatedAt: r.Timestamp, Prompt: text,
		}); err != nil {
			return false, fmt.Errorf("dedup match: %w", err)
		} else if ok {
			recordKeys(ctx, st, r, id)
			stats.RecordsSkipped++
			stats.RecordsMatched++
			return false, nil
		}
		id, err := writeUserEntry(ctx, st, r, text, opts)
		if err != nil {
			return false, err
		}
		stats.Inserted++
		pending[r.SessionID] = id
		recordKeys(ctx, st, r, id)
		return true, nil

	case RoleAssistant:
		// 2. The latest prompt before this answer already holds it.
		latest, latestResp, haveLatest, err := st.LatestTurn(ctx, r.Tool, r.SessionID, r.Timestamp)
		if err != nil {
			return false, fmt.Errorf("dedup match: %w", err)
		}
		if haveLatest && latestResp == text {
			recordKeys(ctx, st, r, latest)
			stats.RecordsSkipped++
			stats.RecordsMatched++
			return false, nil
		}
		target, ok := pending[r.SessionID]
		if !ok && haveLatest && latestResp == "" {
			// A prompt stored by an earlier run (read from a cursor, or
			// an answer Codex held back until its token count landed).
			target, ok = latest, true
		}
		if ok && target != "" {
			if err := attachAssistant(ctx, st, target, r, text); err != nil {
				return false, err
			}
			stats.Attached++
			delete(pending, r.SessionID)
			recordKeys(ctx, st, r, target)
			return true, nil
		}
		// Standalone answer — its own row so the response isn't lost.
		id, err := writeStandaloneAssistant(ctx, st, r, text, opts)
		if err != nil {
			return false, err
		}
		stats.Standalone++
		recordKeys(ctx, st, r, id)
		return true, nil
	}
	return false, nil
}

// recordKeys records both dedup keys of r for entry id (a key already
// recorded keeps its entry). Best effort: a failed write only costs a
// later dedup lookup.
func recordKeys(ctx context.Context, st *store.Store, r Record, id string) {
	_ = st.ImportLineRecord(ctx, r.LineHash, r.Source, id)
	_ = st.ImportLineRecord(ctx, r.MsgKey, r.Source, id)
}

// errStopParsing is the sentinel the emit callback returns when --limit
// is reached. Run() catches it without surfacing it as an error.
var errStopParsing = errors.New("importer: stop parsing")

// writeUserEntry persists one user-side Record as a fresh entry, with
// project / git fields resolved from the record's CWD. Returns the
// entry id so the driver can pair the next assistant Record to it.
func writeUserEntry(ctx context.Context, st *store.Store, r Record, text string, opts *Options) (string, error) {
	proj, owner, name, remote, branch, commit := resolveProject(ctx, r.CWD, opts)
	in := store.InsertEntryInput{
		Tool:           r.Tool,
		ToolVersion:    r.ToolVersion,
		CWD:            r.CWD,
		Project:        proj,
		RepoOwner:      owner,
		RepoName:       name,
		RepoRemote:     remote,
		GitBranch:      branch,
		GitCommit:      commit,
		SessionID:      r.SessionID,
		SessionName:    r.SessionName,
		Terminal:       r.Terminal,
		TerminalTitle:  r.TerminalTitle,
		Prompt:         text,
		Raw:            encodeRaw(r),
		PermissionMode: r.PermissionMode,
	}
	if !r.Timestamp.IsZero() {
		ts := r.Timestamp
		in.CreatedAt = &ts
	}
	return st.InsertEntry(ctx, in)
}

// writeStandaloneAssistant persists an assistant Record that has no
// matching prompt in the current import pass — store everything we can
// on the response side so the row's still searchable.
func writeStandaloneAssistant(ctx context.Context, st *store.Store, r Record, text string, opts *Options) (string, error) {
	proj, owner, name, remote, branch, commit := resolveProject(ctx, r.CWD, opts)
	in := store.InsertEntryInput{
		Tool:             r.Tool,
		ToolVersion:      r.ToolVersion,
		CWD:              r.CWD,
		Project:          proj,
		RepoOwner:        owner,
		RepoName:         name,
		RepoRemote:       remote,
		GitBranch:        branch,
		GitCommit:        commit,
		SessionID:        r.SessionID,
		SessionName:      r.SessionName,
		Terminal:         r.Terminal,
		TerminalTitle:    r.TerminalTitle,
		Response:         text,
		Model:            r.Model,
		Raw:              encodeRaw(r),
		TokensIn:         r.TokensIn,
		TokensOut:        r.TokensOut,
		TokensCacheRead:  r.TokensCacheRead,
		TokensCacheWrite: r.TokensCacheWrite,
		StopReason:       r.StopReason,
		PermissionMode:   r.PermissionMode,
	}
	if !r.Timestamp.IsZero() {
		ts := r.Timestamp
		in.CreatedAt = &ts
	}
	return st.InsertEntry(ctx, in)
}

// attachAssistant attaches a Record's response + metadata to an
// existing user-prompt entry — the import-side mirror of the live Stop
// hook's AttachResponse call.
func attachAssistant(ctx context.Context, st *store.Store, entryID string, r Record, text string) error {
	return st.AttachResponse(ctx, store.AttachResponseInput{
		EntryID:          entryID,
		Response:         text,
		Model:            r.Model,
		TokensIn:         r.TokensIn,
		TokensOut:        r.TokensOut,
		TokensCacheRead:  r.TokensCacheRead,
		TokensCacheWrite: r.TokensCacheWrite,
		StopReason:       r.StopReason,
		PermissionMode:   r.PermissionMode,
		ToolVersion:      r.ToolVersion,
	})
}

// encodeRaw renders a Record's provenance into the `raw` column. The
// historical convention is a bare line hash; when Extras are present
// (codex), we promote raw to a small JSON object so the Codex-specific
// metadata (sandbox / collab mode / model_provider / …) survives the
// import. Per-line dedup runs through the import_lines table, not raw,
// so changing the shape here is safe.
func encodeRaw(r Record) string {
	if len(r.Extras) == 0 {
		return r.LineHash
	}
	out := map[string]any{"line_hash": r.LineHash}
	for k, v := range r.Extras {
		out[k] = v
	}
	b, err := json.Marshal(out)
	if err != nil {
		return r.LineHash
	}
	return string(b)
}

// resolveProject derives ("host/owner/repo", owner, name, remote,
// branch, commit) from a cwd by shelling out to git. Cached per cwd
// for the lifetime of the Run so we don't pay 3 git invocations per
// transcript line. Same project-fallback logic as live capture: git
// remote → canonical project; otherwise basename of cwd.
func resolveProject(ctx context.Context, cwd string, opts *Options) (proj, owner, name, remote, branch, commit string) {
	if cwd == "" {
		return
	}
	if cached, ok := opts.projectCache[cwd]; ok {
		return cached.Project, cached.Owner, cached.Name, cached.Remote, cached.Branch, cached.Commit
	}
	g := aictx.CollectGit(ctx, cwd)
	pi := projectInfo{
		Owner:  g.Owner,
		Name:   g.Name,
		Remote: g.Remote,
		Branch: g.Branch,
		Commit: g.Commit,
	}
	pi.Project = aictx.CanonicalProject(g.Remote)
	if pi.Project == "" {
		pi.Project = filepath.Base(cwd)
	}
	opts.projectCache[cwd] = pi
	return pi.Project, pi.Owner, pi.Name, pi.Remote, pi.Branch, pi.Commit
}

// expandHome turns "~/foo" into "$HOME/foo". Returns the input
// unchanged if it doesn't start with "~". Sources call this from
// DefaultRoot() so the literal returned path is OS-absolute.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
