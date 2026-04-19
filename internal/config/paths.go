// Package config resolves ailog paths and environment overrides.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home returns the ailog home directory. $AILOG_HOME wins, else ~/.ailog.
func Home() (string, error) {
	if v := os.Getenv("AILOG_HOME"); v != "" {
		return v, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(u, ".ailog"), nil
}

// DBPath returns the absolute path to the SQLite file.
func DBPath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "log.db"), nil
}

// EnsureHome creates the home dir with 0700 perms if missing.
func EnsureHome() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(h, 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", h, err)
	}
	return h, nil
}

// ClaudeSkillsDir returns the user-level Claude Code skills directory.
// Override with $AILOG_CLAUDE_HOME for testing.
func ClaudeSkillsDir() (string, error) {
	if v := os.Getenv("AILOG_CLAUDE_HOME"); v != "" {
		return filepath.Join(v, "skills"), nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claude", "skills"), nil
}

// ClaudeSettingsPath returns path to ~/.claude/settings.json.
func ClaudeSettingsPath() (string, error) {
	if v := os.Getenv("AILOG_CLAUDE_HOME"); v != "" {
		return filepath.Join(v, "settings.json"), nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claude", "settings.json"), nil
}
