package context

import (
	"os"

	"github.com/google/uuid"
)

// SessionInfo resolves the current session id. If AILOG_SESSION_ID is
// unset a fresh UUID v7 is generated and returned — callers may choose
// to surface this as a warning since the value won't persist across
// CLI invocations unless the caller exports it.
type SessionInfo struct {
	ID       string
	Tool     string
	Name     string // AILOG_SESSION_NAME override
	WasFresh bool   // true when we generated a new id (env was unset)
}

func CollectSession() SessionInfo {
	id := os.Getenv("AILOG_SESSION_ID")
	fresh := false
	if id == "" {
		if u, err := uuid.NewV7(); err == nil {
			id = u.String()
			fresh = true
		}
	}
	tool := os.Getenv("AILOG_TOOL")
	if tool == "" {
		tool = "manual"
	}
	return SessionInfo{
		ID:       id,
		Tool:     tool,
		Name:     os.Getenv("AILOG_SESSION_NAME"),
		WasFresh: fresh,
	}
}
