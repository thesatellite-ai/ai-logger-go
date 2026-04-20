package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/khanakia/ai-logger/internal/redact"
	"github.com/khanakia/ai-logger/internal/store"
)

// Options controls one Run() invocation. Zero-value defaults are sane:
// no since-cutoff, no limit, write to all-discovered files.
type Options struct {
	Root  string    // override the source's DefaultRoot when non-empty
	Since time.Time // skip records strictly older than this
	Limit int       // stop after N records (0 = no cap)
	Force bool      // ignore the per-file mtime watermark; reparse everything

	// Strict=true escalates the format-drift warning to a hard reject:
	// the file's records get rolled back conceptually (we don't stamp
	// the watermark) and the file path is printed with an error tag.
	// Off by default — most users want best-effort imports.
	Strict bool

	// Verbose=true prints "imported file (n records)" per file to Out.
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
	Files          int
	FilesScanned   int
	FilesSkipped   int // skipped via mtime watermark
	Records        int
	RecordsSkipped int // dedup hits (already imported) or filtered out
	Inserted       int // user-side prompts written to entries
	Attached       int // assistant-side responses paired with prompts
	Standalone     int // assistant Records with no prior user turn

	// Drift watchdog counters. Files where Anchor returned true at
	// least once are "healthy"; files with anchor-eligible records but
	// zero anchor passes are "suspect" (likely upstream schema drift).
	FilesHealthy int
	FilesSuspect int
}

// Run drives one source end-to-end against a store. It's pure — every
// side effect goes through the passed Store / Source. The driver is
// designed to be safely re-runnable: the per-line dedup table makes
// repeated runs idempotent, and the per-file mtime watermark makes
// repeated runs fast.
func Run(ctx context.Context, st *store.Store, src Source, opts Options) (Stats, error) {
	var stats Stats
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.projectCache == nil {
		opts.projectCache = map[string]projectInfo{}
	}

	root := opts.Root
	if root == "" {
		root = src.DefaultRoot()
	}
	if root == "" {
		return stats, fmt.Errorf("importer: source %s has no root and --from not provided", src.Name())
	}

	files, err := src.Discover(ctx, root)
	if err != nil {
		return stats, fmt.Errorf("discover: %w", err)
	}
	stats.Files = len(files)

	// Process oldest files first so the per-session turn ordering aligns
	// with creation order — important when a transcript spans rotated
	// files.
	sortByMtime(files)

	// Per-session counter the driver uses to attach assistant responses
	// to the prompt they followed. Mirrors the live-hook behavior of
	// pairing the first response-less entry in a session with the next
	// assistant message.
	pendingPrompt := map[string]string{} // sessionID → entry id

	for _, f := range files {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		// mtime watermark — skip files that haven't changed since the
		// last successful import (unless --force).
		info, statErr := os.Stat(f)
		if statErr != nil {
			fmt.Fprintf(opts.Out, "skip %s: %v\n", f, statErr)
			continue
		}
		if !opts.Force {
			wm, ok, err := st.ImportFileWatermark(ctx, f)
			if err == nil && ok && wm.MtimeUnixNano == info.ModTime().UnixNano() && wm.Size == info.Size() {
				stats.FilesSkipped++
				continue
			}
		}
		stats.FilesScanned++

		var (
			fileRecords      int
			fileAnchorOK     int    // records where Source.Anchor returned true
			fileAnchorElig   int    // records where Anchor *should* fire (assistant-with-text etc.)
			fileMaxVer       string // highest tool_version observed in this file
		)
		parseErr := src.Parse(ctx, f, func(r Record) error {
			stats.Records++
			if opts.Limit > 0 && stats.Records > opts.Limit {
				return errStopParsing
			}
			if !opts.Since.IsZero() && !r.Timestamp.IsZero() && r.Timestamp.Before(opts.Since) {
				stats.RecordsSkipped++
				return nil
			}
			r.SourceFile = f

			// Drift watchdog book-keeping. We track whichever assistant
			// records flowed past, regardless of whether they end up
			// inserted vs deduped — a re-import shouldn't lose the
			// signal just because rows were already in the DB.
			if r.Role == RoleAssistant {
				fileAnchorElig++
				if src.Anchor(r) {
					fileAnchorOK++
				}
			}
			if r.ToolVersion != "" && versionCmp(r.ToolVersion, fileMaxVer) > 0 {
				fileMaxVer = r.ToolVersion
			}

			// Per-line dedup: if we've already absorbed this line's
			// SHA, the row already exists. Cheap indexed lookup.
			if r.LineHash != "" {
				exists, err := st.ImportLineExists(ctx, r.LineHash)
				if err != nil {
					return fmt.Errorf("dedup lookup: %w", err)
				}
				if exists {
					stats.RecordsSkipped++
					return nil
				}
			}

			switch r.Role {
			case RoleUser:
				id, err := writeUserEntry(ctx, st, r, &opts)
				if err != nil {
					return err
				}
				stats.Inserted++
				pendingPrompt[r.SessionID] = id
				if r.LineHash != "" {
					_ = st.ImportLineRecord(ctx, r.LineHash, f, id)
				}
			case RoleAssistant:
				if id, ok := pendingPrompt[r.SessionID]; ok && id != "" {
					if err := attachAssistant(ctx, st, id, r); err != nil {
						return err
					}
					stats.Attached++
					delete(pendingPrompt, r.SessionID)
					if r.LineHash != "" {
						_ = st.ImportLineRecord(ctx, r.LineHash, f, id)
					}
				} else {
					// Standalone assistant — write as its own row so
					// the response isn't lost. Common when a transcript
					// is mid-conversation and the user line was already
					// consumed in a prior import.
					id, err := writeStandaloneAssistant(ctx, st, r, &opts)
					if err != nil {
						return err
					}
					stats.Standalone++
					if r.LineHash != "" {
						_ = st.ImportLineRecord(ctx, r.LineHash, f, id)
					}
				}
			}
			fileRecords++
			return nil
		})
		if parseErr != nil && !errors.Is(parseErr, errStopParsing) {
			fmt.Fprintf(opts.Out, "parse %s: %v\n", f, parseErr)
			continue
		}

		// Drift watchdog. A file is "suspect" when it had at least one
		// anchor-eligible record (e.g. an assistant turn) but the
		// per-source Anchor predicate never passed. The most likely
		// cause is that the upstream tool renamed the field we sample
		// (claude usage.input_tokens → ?). Severity depends on the
		// observed tool version vs the parser's LastKnownVersion:
		//   - newer than known + --strict: hard reject (skip watermark, surface as error)
		//   - newer than known: warn loudly
		//   - same/older: warn (probably us, not them)
		suspect := fileAnchorElig > 0 && fileAnchorOK == 0
		newer := fileMaxVer != "" && versionCmp(fileMaxVer, src.LastKnownVersion()) > 0
		if suspect {
			stats.FilesSuspect++
			tag := "warn"
			if newer && opts.Strict {
				tag = "REJECT"
			} else if newer {
				tag = "WARN(newer-version)"
			}
			fmt.Fprintf(opts.Out,
				"%s %s: %d assistant record(s), 0 carried token usage — possible upstream schema drift (observed %s=%q, parser known=%q)\n",
				tag, f, fileAnchorElig, src.Name(), fileMaxVer, src.LastKnownVersion())
			if newer && opts.Strict {
				// Don't stamp the watermark so a fixed parser re-walks
				// the file. We still kept any records the parse did
				// emit (per-line dedup makes that safe).
				continue
			}
		} else if fileRecords > 0 {
			stats.FilesHealthy++
		}

		// Only stamp the watermark when the parser actually emitted
		// records. Earlier no-op parser skeletons (codex / opencode)
		// would otherwise lock the file out forever — when their real
		// parser ships, the watermark would still match and the file
		// would be skipped. Empty-but-parseable files are cheap to
		// re-walk; a future "saw 0 records intentionally" parser can
		// emit a sentinel if we ever need the fast path back.
		if fileRecords > 0 {
			_ = st.ImportFileMark(ctx, f, info.ModTime(), info.Size())
		}

		if opts.Verbose && fileRecords > 0 {
			fmt.Fprintf(opts.Out, "imported %s (%d records)\n", f, fileRecords)
		}

		if errors.Is(parseErr, errStopParsing) {
			return stats, nil
		}
	}
	return stats, nil
}

// errStopParsing is the sentinel the emit callback returns when --limit
// is reached. Run() catches it without surfacing it as an error.
var errStopParsing = errors.New("importer: stop parsing")

// writeUserEntry persists one user-side Record as a fresh entry, with
// project / git fields resolved from the record's CWD. Returns the
// entry id so the driver can pair the next assistant Record to it.
func writeUserEntry(ctx context.Context, st *store.Store, r Record, opts *Options) (string, error) {
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
		Prompt:         redact.Scrub(r.Text),
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
func writeStandaloneAssistant(ctx context.Context, st *store.Store, r Record, opts *Options) (string, error) {
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
		Response:         redact.Scrub(r.Text),
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
func attachAssistant(ctx context.Context, st *store.Store, entryID string, r Record) error {
	return st.AttachResponse(ctx, store.AttachResponseInput{
		EntryID:          entryID,
		Response:         redact.Scrub(r.Text),
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

// sortByMtime sorts a list of paths by mtime ascending (oldest first).
// Errors silently leave order unchanged for the affected file.
func sortByMtime(paths []string) {
	type pm struct {
		p string
		m time.Time
	}
	pms := make([]pm, len(paths))
	for i, p := range paths {
		if info, err := os.Stat(p); err == nil {
			pms[i] = pm{p, info.ModTime()}
		} else {
			pms[i] = pm{p, time.Time{}}
		}
	}
	sort.Slice(pms, func(i, j int) bool { return pms[i].m.Before(pms[j].m) })
	for i := range paths {
		paths[i] = pms[i].p
	}
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
