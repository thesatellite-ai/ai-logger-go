// Package store is the single entry point for persistence. Nothing
// outside this package talks to ent or raw SQL directly. The package
// is split across several files for readability:
//
//	store.go         — Open/Close, Store struct, basic insert + attach
//	store_query.go   — read-side: search, get, prefix resolution, recent, session
//	store_curate.go  — mutate-side: tags/star/notes/redact/purge + stats
//	fts.go           — FTS5 virtual table DDL + helpers
//	ids.go           — UUID v7 generator
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/khanakia/ai-logger/ent"
	"github.com/khanakia/ai-logger/ent/entry"
)

// Entry is a convenience alias so callers don't need to import the ent
// package directly.
type Entry = ent.Entry

// InsertEntryInput carries everything needed to persist a fresh prompt
// row. Only Prompt is meaningfully required; every other field
// gracefully falls back to zero values. Field comments mirror the
// schema-level comments in ent/schema/entry.go — see there for source
// (which hook / tool produces each field).
type InsertEntryInput struct {
	Tool        string
	ToolVersion string

	CWD        string
	Project    string
	RepoOwner  string
	RepoName   string
	RepoRemote string
	GitBranch  string
	GitCommit  string

	SessionID     string
	SessionName   string
	TurnIndex     int
	ParentEntryID string

	Hostname      string
	User          string
	Shell         string
	Terminal      string
	TerminalTitle string
	TTY           string
	PID           int

	Prompt   string
	Response string
	Model    string
	Raw      string

	// Usage / runtime metadata — populated when known (mostly Stop hook).
	TokensIn         int
	TokensOut        int
	TokensCacheRead  int // Anthropic prompt-cache hit; claude-code only.
	TokensCacheWrite int // Anthropic prompt-cache write; claude-code only.
	StopReason       string
	PermissionMode   string // claude-code only

	Tags string

	// CreatedAt overrides the row's created_at when non-nil. Live capture
	// (hooks / `ailog add`) leaves this nil so the schema default
	// (time.Now) wins. Backfill paths (importer) set it from the source
	// transcript so historical entries land at their original timestamps.
	CreatedAt *time.Time
	// TranscriptAt is when the agent wrote the turn into its transcript
	// (imports); nil for a live-hook row.
	TranscriptAt *time.Time
}

// AttachResponseInput carries the rich set of per-turn metadata a Stop
// hook can produce. Pass empty values for fields the upstream tool
// doesn't expose; only non-empty / non-zero fields are written.
type AttachResponseInput struct {
	EntryID string

	Response string
	Model    string

	TokensIn         int
	TokensOut        int
	TokensCacheRead  int
	TokensCacheWrite int

	StopReason     string
	PermissionMode string
	ToolVersion    string
}

// Store is the public facade over ent + the raw FTS5 helpers.
// Callers obtain one with Open() and close with Close().
type Store struct {
	client *ent.Client
	// db owns the connection pool: Close, migrations, backups and
	// starting transactions. Row reads and writes go through q.
	db *sql.DB
	// q runs row reads and writes: db itself, or the *sql.Tx of a Store
	// handed out by InTx, so raw SQL and ent share one transaction.
	q    querier
	path string
	// inTx marks a Store handed out by InTx; nesting is refused because
	// SQLite has no nested BEGIN and a second pool transaction would wait
	// on the first one's write lock.
	inTx bool
}

// querier is what *sql.DB and *sql.Tx share for row reads and writes.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txDriver is an ent driver bound to an open *sql.Tx. ent's update and
// delete builders call Driver.Tx for their own statements; here that
// joins the open transaction (dialect.NopTx: no commit of its own)
// instead of starting a second one on a connection that holds no
// transaction. Close is a no-op: InTx ends the transaction.
type txDriver struct{ *entsql.Driver }

// Tx joins the open transaction.
func (d txDriver) Tx(context.Context) (dialect.Tx, error) { return dialect.NopTx(d), nil }

// Close leaves the transaction to InTx.
func (txDriver) Close() error { return nil }

// ErrNestedTx is returned by InTx on a Store InTx already handed out.
var ErrNestedTx = errors.New("store: nested transaction")

// InTx runs fn with a Store whose ent client and raw SQL all use one
// transaction: committed when fn returns nil, rolled back otherwise.
// Many small writes (ailog dedupe's merges) then pay for one commit
// instead of one each. The Store passed to fn is valid only inside fn
// and must not be closed.
func (s *Store) InTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.inTx {
		return ErrNestedTx
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	drv := txDriver{entsql.NewDriver(dialect.SQLite, entsql.Conn{ExecQuerier: tx})}
	txStore := &Store{client: ent.NewClient(ent.Driver(drv)), db: s.db, q: tx, path: s.path, inTx: true}
	if err := fn(txStore); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Open prepares the SQLite file at path, applies pragmas (WAL, NORMAL
// sync, 5s busy timeout), runs ent migrations, and creates the FTS5
// virtual table. Safe to call on an existing DB — every step is
// idempotent.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty db path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)",
		abs,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	s := &Store{client: client, db: db, q: db, path: abs}

	// Importer bookkeeping tables — IF NOT EXISTS, fast (~µs) on warm
	// DBs, no schema-version bump needed because additive raw SQL.
	if err := ensureImportTables(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Fast path: check the stored schema version marker. If it matches
	// our compile-time SchemaVersion, the DB was migrated by a previous
	// binary of the same build — skip Schema.Create entirely.
	//
	// This keeps Open() well under a millisecond on warm DBs, instead
	// of paying ent's ~few-ms schema-inspection cost on every single
	// CLI invocation / hook fire.
	if currentSchemaVersion(ctx, db) == SchemaVersion {
		return s, nil
	}

	// Cold / upgraded DB: run the full migration.
	if err := s.MigrateApply(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureSchemaMetaTable(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := writeSchemaVersion(ctx, db, SchemaVersion); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases ent's client and the underlying SQL connection pool.
func (s *Store) Close() error {
	if err := s.client.Close(); err != nil {
		return err
	}
	return s.db.Close()
}

// Path returns the absolute path to the underlying SQLite file.
func (s *Store) Path() string { return s.path }

// InsertEntry writes a new entry, assigns a UUID v7, and mirrors the
// searchable fields into the FTS5 index. Returns the new entry id.
//
// turn_index and parent_entry_id are auto-computed from the session
// chain when both are zero/empty — i.e. when the caller doesn't have
// the previous turn's id handy (the common case for hooks).
func (s *Store) InsertEntry(ctx context.Context, in InsertEntryInput) (string, error) {
	id := NewID()
	if in.TurnIndex == 0 && in.SessionID != "" {
		// Count existing turns in this session → that's our index.
		n, err := s.client.Entry.Query().
			Where(entry.SessionIDEQ(in.SessionID)).
			Count(ctx)
		if err == nil {
			in.TurnIndex = n
		}
		// Link to the previous turn so the parent chain is walkable.
		if in.ParentEntryID == "" {
			prev, err := s.client.Entry.Query().
				Where(entry.SessionIDEQ(in.SessionID)).
				Order(ent.Desc(entry.FieldTurnIndex)).
				Limit(1).
				IDs(ctx)
			if err == nil && len(prev) > 0 {
				in.ParentEntryID = prev[0]
			}
		}
	}
	create := s.client.Entry.Create().
		SetID(id).
		SetTool(in.Tool).
		SetToolVersion(in.ToolVersion)
	if in.CreatedAt != nil {
		create = create.SetCreatedAt(*in.CreatedAt)
	}
	if in.TranscriptAt != nil {
		create = create.SetTranscriptAt(*in.TranscriptAt)
	}
	_, err := create.
		SetCwd(in.CWD).
		SetProject(in.Project).
		SetRepoOwner(in.RepoOwner).
		SetRepoName(in.RepoName).
		SetRepoRemote(in.RepoRemote).
		SetGitBranch(in.GitBranch).
		SetGitCommit(in.GitCommit).
		SetSessionID(in.SessionID).
		SetSessionName(in.SessionName).
		SetTurnIndex(in.TurnIndex).
		SetParentEntryID(in.ParentEntryID).
		SetHostname(in.Hostname).
		SetUser(in.User).
		SetShell(in.Shell).
		SetTerminal(in.Terminal).
		SetTerminalTitle(in.TerminalTitle).
		SetTty(in.TTY).
		SetPid(in.PID).
		SetPrompt(in.Prompt).
		SetResponse(in.Response).
		SetModel(in.Model).
		SetRaw(in.Raw).
		SetTokenCountIn(in.TokensIn).
		SetTokenCountOut(in.TokensOut).
		SetTokenCountCacheRead(in.TokensCacheRead).
		SetTokenCountCacheCreate(in.TokensCacheWrite).
		SetStopReason(in.StopReason).
		SetPermissionMode(in.PermissionMode).
		SetTags(in.Tags).
		Save(ctx)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}
	if err := ftsInsert(ctx, s.q, id, in.Prompt, in.Response, ""); err != nil {
		return "", fmt.Errorf("fts index: %w", err)
	}
	return id, nil
}

// AttachResponse fills in the response and any per-turn metadata the
// Stop hook produced (model, token usage including cache, stop_reason,
// permission_mode, tool version), then re-syncs the FTS5 index.
//
// Only non-empty / non-zero fields are written — pass partial input
// from adapters that don't expose every field.
func (s *Store) AttachResponse(ctx context.Context, in AttachResponseInput) error {
	u := s.client.Entry.UpdateOneID(in.EntryID).
		SetResponse(in.Response)
	if in.Model != "" {
		u = u.SetModel(in.Model)
	}
	if in.TokensIn > 0 {
		u = u.SetTokenCountIn(in.TokensIn)
	}
	if in.TokensOut > 0 {
		u = u.SetTokenCountOut(in.TokensOut)
	}
	if in.TokensCacheRead > 0 {
		u = u.SetTokenCountCacheRead(in.TokensCacheRead)
	}
	if in.TokensCacheWrite > 0 {
		u = u.SetTokenCountCacheCreate(in.TokensCacheWrite)
	}
	if in.StopReason != "" {
		u = u.SetStopReason(in.StopReason)
	}
	if in.PermissionMode != "" {
		u = u.SetPermissionMode(in.PermissionMode)
	}
	if in.ToolVersion != "" {
		u = u.SetToolVersion(in.ToolVersion)
	}
	if _, err := u.Save(ctx); err != nil {
		return fmt.Errorf("attach response: %w", err)
	}
	return ftsUpdateResponse(ctx, s.q, in.EntryID, in.Response)
}
