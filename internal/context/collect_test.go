package context_test

import (
	"context"
	"testing"

	aictx "github.com/khanakia/ai-logger/internal/context"
)

func TestCollect_DoesNotPanicOutsideGit(t *testing.T) {
	t.Setenv("AILOG_SESSION_ID", "")
	t.Setenv("AILOG_TOOL", "")
	c := aictx.Collect(context.Background())
	if c.CWD == "" {
		t.Fatal("cwd should be set")
	}
	if c.Session.ID == "" {
		t.Fatal("session id should be auto-generated when env unset")
	}
	if !c.Session.WasFresh {
		t.Fatal("WasFresh should be true when AILOG_SESSION_ID was empty")
	}
	if c.Session.Tool != "manual" {
		t.Fatalf("tool should default to 'manual', got %q", c.Session.Tool)
	}
}

func TestCollect_RespectsSessionEnv(t *testing.T) {
	t.Setenv("AILOG_SESSION_ID", "11111111-1111-7111-8111-111111111111")
	t.Setenv("AILOG_TOOL", "claude-code")
	c := aictx.Collect(context.Background())
	if c.Session.ID != "11111111-1111-7111-8111-111111111111" {
		t.Fatalf("expected env session id, got %q", c.Session.ID)
	}
	if c.Session.WasFresh {
		t.Fatal("WasFresh should be false when env was set")
	}
	if c.Session.Tool != "claude-code" {
		t.Fatalf("tool: got %q", c.Session.Tool)
	}
}
