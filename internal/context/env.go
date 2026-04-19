package context

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EnvInfo groups machine/shell/terminal context pulled from env vars
// and os.* helpers. Cheap — no subprocesses.
type EnvInfo struct {
	Hostname      string
	User          string
	Shell         string // basename of $SHELL
	Terminal      string // $TERM_PROGRAM
	TerminalTitle string // $AILOG_TERMINAL_TITLE (explicit) or $ITERM_SESSION_ID / $WEZTERM_PANE
	TTY           string
	PID           int // parent pid (the caller process — typically claude / shell)
}

func CollectEnv() EnvInfo {
	host, _ := os.Hostname()
	return EnvInfo{
		Hostname:      host,
		User:          os.Getenv("USER"),
		Shell:         filepath.Base(os.Getenv("SHELL")),
		Terminal:      os.Getenv("TERM_PROGRAM"),
		TerminalTitle: firstNonEmpty(os.Getenv("AILOG_TERMINAL_TITLE"), os.Getenv("ITERM_SESSION_ID"), os.Getenv("WEZTERM_PANE")),
		TTY:           firstNonEmpty(os.Getenv("TTY"), readTTY()),
		PID:           parentPID(),
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func readTTY() string {
	// /dev/tty is a symlink on some systems; use ttyname via os if available.
	// For portability fall back to empty.
	fi, err := os.Stat("/dev/tty")
	if err != nil {
		return ""
	}
	_ = fi
	return ""
}

func parentPID() int {
	if s := os.Getenv("AILOG_PARENT_PID"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return os.Getppid()
}
