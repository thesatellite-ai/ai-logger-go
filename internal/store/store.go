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
// gracefully falls back to zero values.
type InsertEntryInput struct {
	Tool          string
	CWD           string
	Project       string
	RepoOwner     string
	RepoName      string
	RepoRemote    string
	GitBranch     string
	GitCommit     string
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
	Prompt        string
	Response      string
	Model         string
	Raw           string
	TokensIn      int
	TokensOut     int
	Tags          string
}

// Store is the public facade over ent + the raw FTS5 helpers.
// Callers obtain one with Open() and close with Close().
type Store struct {
	client *ent.Client
	db     *sql.DB
	path   string
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

	if err := client.Schema.Create(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ent migrate: %w", err)
	}
	if err := applyFTSMigration(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{client: client, db: db, path: abs}, nil
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
	_, err := s.client.Entry.Create().
		SetID(id).
		SetTool(in.Tool).
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
		SetTags(in.Tags).
		Save(ctx)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}
	if err := ftsInsert(ctx, s.db, id, in.Prompt, in.Response, ""); err != nil {
		return "", fmt.Errorf("fts index: %w", err)
	}
	return id, nil
}

// AttachResponse fills in the response (and optional model + tokens)
// on an existing entry, then re-syncs the FTS5 index for that row.
// Used by Stop hooks to attach the assistant turn to the prompt entry
// inserted earlier in the same session.
func (s *Store) AttachResponse(ctx context.Context, entryID, response, model string, tokensOut int) error {
	u := s.client.Entry.UpdateOneID(entryID).
		SetResponse(response)
	if model != "" {
		u = u.SetModel(model)
	}
	if tokensOut > 0 {
		u = u.SetTokenCountOut(tokensOut)
	}
	if _, err := u.Save(ctx); err != nil {
		return fmt.Errorf("attach response: %w", err)
	}
	return ftsUpdateResponse(ctx, s.db, entryID, response)
}
