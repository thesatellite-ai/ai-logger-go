package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/config"
	"github.com/khanakia/ai-logger/internal/store"
)

// openStore opens the ailog DB at the resolved home path.
func openStore(ctx context.Context) (*store.Store, error) {
	path, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("no ailog database at %s — run `ailog init` first", path)
	}
	return store.Open(ctx, path)
}

// readStdin returns stdin content if data is piped in; empty string otherwise.
func readStdinIfPiped() (string, error) {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if (fi.Mode() & os.ModeCharDevice) != 0 {
		return "", nil // terminal, no pipe
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseSince accepts durations like "24h", "30m", "7d", "2w" and returns
// a time.Time cutoff. Empty string returns zero time (no filter).
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	// Support d/w which time.ParseDuration doesn't.
	var mult time.Duration
	switch s[len(s)-1] {
	case 'd':
		mult = 24 * time.Hour
		s = s[:len(s)-1]
	case 'w':
		mult = 7 * 24 * time.Hour
		s = s[:len(s)-1]
	}
	if mult > 0 {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse since: %w", err)
		}
		return time.Now().Add(-time.Duration(float64(mult) * n)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse since: %w", err)
	}
	return time.Now().Add(-d), nil
}

// shortID returns a 13-char display prefix ("019da607-dfac"). UUID v7's
// first 8 hex chars are a ms timestamp so they collide within the same
// millisecond — 12 hex digits + the hyphen almost never do.
func shortID(id string) string {
	if len(id) < 13 {
		return id
	}
	return id[:13]
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
