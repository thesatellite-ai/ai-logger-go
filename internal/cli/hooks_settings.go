package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/jsonedit"
)

// Claude Code settings.json keys and values ailog reads or writes.
const (
	claudeHooksKey    = "hooks"
	claudeTypeCommand = "command"

	// backupTimeLayout names settings backups settings.json.bak-<stamp>.
	backupTimeLayout = "20060102-150405"
	backupInfix      = ".bak-"

	// newSettingsPerm is the mode for a settings.json ailog creates; an
	// existing file keeps its own mode.
	newSettingsPerm fs.FileMode = 0o644

	ailogBinaryName = "ailog"
)

// errNotObject is returned when a settings value ailog must descend
// into has an unexpected JSON type. ailog refuses to touch such a file
// rather than overwrite a value it does not understand.
var errNotObject = errors.New("unexpected JSON type")

// shellSafePath matches paths that can sit unquoted in a shell command.
var shellSafePath = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// hookCommandLine is the command string ailog installs for one event.
func hookCommandLine(bin, sub string) string {
	if !shellSafePath.MatchString(bin) {
		bin = "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	}
	return bin + " hook " + sub
}

// isAilogCommandFor reports whether cmd is ailog's own hook command for
// the adapter sub ("claude-code prompt"), whatever binary path it uses:
// `<path>/ailog hook claude-code prompt`, optionally quoted. A command
// that merely mentions ailog (a wrapper script, an echo) is not ours.
func isAilogCommandFor(cmd, sub string) bool {
	cmd = strings.TrimSpace(cmd)
	suffix := " hook " + sub
	if !strings.HasSuffix(cmd, suffix) {
		return false
	}
	bin := strings.TrimSpace(strings.TrimSuffix(cmd, suffix))
	quoted := len(bin) >= 2 && (bin[0] == '\'' || bin[0] == '"') && bin[len(bin)-1] == bin[0]
	switch {
	case quoted:
		bin = bin[1 : len(bin)-1]
	case strings.ContainsAny(bin, " \t;&|<>$`"):
		return false // a compound shell command, not a bare binary path
	}
	base := strings.TrimSuffix(filepath.Base(bin), ".exe")
	return base == ailogBinaryName
}

// isOwnHook reports whether cmd is ailog's hook for sub: either the
// exact command this binary would write (covers a binary not named
// "ailog", such as a renamed build) or any ailog binary's command.
func isOwnHook(cmd, bin, sub string) bool {
	return strings.TrimSpace(cmd) == hookCommandLine(bin, sub) || isAilogCommandFor(cmd, sub)
}

// hookGroup is the slice of a settings.json hook group ailog inspects.
type hookGroup struct {
	Hooks []struct {
		Command string `json:"command"`
	} `json:"hooks"`
}

// eventHasAilog reports whether the event's group array already holds
// ailog's command for sub.
func eventHasAilog(value []byte, bin, sub string) (bool, error) {
	var groups []hookGroup
	if err := json.Unmarshal(value, &groups); err != nil {
		return false, err
	}
	for _, g := range groups {
		for _, h := range g.Hooks {
			if isOwnHook(h.Command, bin, sub) {
				return true, nil
			}
		}
	}
	return false, nil
}

// sortedEvents returns the spec's events in a stable order so repeated
// runs produce identical files.
func sortedEvents(events map[string]string) []string {
	names := make([]string, 0, len(events))
	for e := range events {
		names = append(names, e)
	}
	sort.Strings(names)
	return names
}

// newHookEntry / newHookGroupT are the shapes ailog writes; structs
// (not maps) so keys come out in Claude's own order: type, command.
type newHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type newHookGroupT struct {
	Hooks []newHookEntry `json:"hooks"`
}

// newHookGroup is the group ailog adds: one command hook, no matcher.
func newHookGroup(command string) newHookGroupT {
	return newHookGroupT{Hooks: []newHookEntry{{Type: claudeTypeCommand, Command: command}}}
}

// indentUnit returns the document's indentation unit, or "" for a
// compact single-line document (new items are then written compact).
func indentUnit(doc []byte) string {
	if !jsonedit.Multiline(doc) {
		return ""
	}
	return jsonedit.IndentUnit(doc)
}

// planClaudeInstall returns settings.json with ailog's hook added to
// each event that lacks it. Every other byte is preserved: other hooks,
// other keys, key order and formatting. An event that already carries
// ailog's command (any binary path) is left alone, so a second run
// returns the input unchanged. added lists the events that changed.
func planClaudeInstall(doc []byte, bin string, events map[string]string) (out []byte, added []string, err error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}\n")
	}
	for _, event := range sortedEvents(events) {
		sub := events[event]
		next, changed, err := addHook(doc, event, bin, sub)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", event, err)
		}
		if changed {
			added = append(added, event)
			doc = next
		}
	}
	return doc, added, nil
}

// addHook adds one ailog group to hooks.<event> unless present.
func addHook(doc []byte, event, bin, sub string) ([]byte, bool, error) {
	unit := indentUnit(doc)
	group := newHookGroup(hookCommandLine(bin, sub))
	root, err := jsonedit.Root(doc)
	if err != nil {
		return nil, false, err
	}
	hooksIt, ok := root.Find(claudeHooksKey)
	if !ok {
		ind := root.ItemIndent(doc)
		m, err := jsonedit.Member(claudeHooksKey, map[string][]newHookGroupT{event: {group}}, ind, unit)
		if err != nil {
			return nil, false, err
		}
		return jsonedit.Append(doc, root, m), true, nil
	}
	hooks, err := jsonedit.Parse(doc, hooksIt.ValStart)
	if err != nil || !hooks.Object {
		return nil, false, fmt.Errorf("%q is not an object: %w", claudeHooksKey, errNotObject)
	}
	evIt, ok := hooks.Find(event)
	if !ok {
		m, err := jsonedit.Member(event, []newHookGroupT{group}, hooks.ItemIndent(doc), unit)
		if err != nil {
			return nil, false, err
		}
		return jsonedit.Append(doc, hooks, m), true, nil
	}
	present, err := eventHasAilog(evIt.Value(doc), bin, sub)
	if err != nil {
		return nil, false, fmt.Errorf("hooks.%s is not an array of hook groups: %w", event, errNotObject)
	}
	if present {
		return doc, false, nil
	}
	arr, err := jsonedit.Parse(doc, evIt.ValStart)
	if err != nil || arr.Object {
		return nil, false, fmt.Errorf("hooks.%s is not an array: %w", event, errNotObject)
	}
	g, err := jsonedit.Encode(group, arr.ItemIndent(doc), unit)
	if err != nil {
		return nil, false, err
	}
	return jsonedit.Append(doc, arr, g), true, nil
}

// planClaudeUninstall returns settings.json without ailog's hook
// commands for the given events. Only commands isOwnHook
// recognises are removed; a group, event or the hooks object is dropped
// only when ailog's entries were all it held. Everything else keeps its
// bytes.
func planClaudeUninstall(doc []byte, bin string, events map[string]string) (out []byte, removed int, err error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		return doc, 0, nil
	}
	for {
		next, ok, err := removeOneHook(doc, bin, events)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			return doc, removed, nil
		}
		doc = next
		removed++
	}
}

// removeOneHook removes the first ailog command it finds, collapsing
// containers that become empty. ok=false means nothing was left.
func removeOneHook(doc []byte, bin string, events map[string]string) ([]byte, bool, error) {
	root, err := jsonedit.Root(doc)
	if err != nil {
		return nil, false, err
	}
	hooksIt, ok := root.Find(claudeHooksKey)
	if !ok {
		return doc, false, nil
	}
	hooks, err := jsonedit.Parse(doc, hooksIt.ValStart)
	if err != nil || !hooks.Object {
		return doc, false, nil // not ours to interpret
	}
	for _, event := range sortedEvents(events) {
		evIt, ok := hooks.Find(event)
		if !ok {
			continue
		}
		groups, err := jsonedit.Parse(doc, evIt.ValStart)
		if err != nil || groups.Object {
			continue
		}
		for gi, gIt := range groups.Items {
			group, err := jsonedit.Parse(doc, gIt.ValStart)
			if err != nil || !group.Object {
				continue
			}
			innerIt, ok := group.Find(claudeHooksKey)
			if !ok {
				continue
			}
			inner, err := jsonedit.Parse(doc, innerIt.ValStart)
			if err != nil || inner.Object {
				continue
			}
			for hi, hIt := range inner.Items {
				var h struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(hIt.Value(doc), &h) != nil || !isOwnHook(h.Command, bin, events[event]) {
					continue
				}
				switch {
				case len(inner.Items) > 1:
					return jsonedit.Rebuild(doc, inner, without(inner.Items, hi)), true, nil
				case len(groups.Items) > 1:
					return jsonedit.Rebuild(doc, groups, without(groups.Items, gi)), true, nil
				case len(hooks.Items) > 1:
					return jsonedit.Rebuild(doc, hooks, without(hooks.Items, indexOf(hooks.Items, event))), true, nil
				default:
					return jsonedit.Rebuild(doc, root, without(root.Items, indexOf(root.Items, claudeHooksKey))), true, nil
				}
			}
		}
	}
	return doc, false, nil
}

func without(items []jsonedit.Item, i int) []jsonedit.Item {
	out := make([]jsonedit.Item, 0, len(items)-1)
	out = append(out, items[:i]...)
	return append(out, items[i+1:]...)
}

func indexOf(items []jsonedit.Item, key string) int {
	for i, it := range items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

// writeSettings writes after over path, first saving the current bytes
// to path.bak-<timestamp>. A symlinked settings.json is written through
// to its target so the link survives. The write is atomic (temp file +
// rename) and keeps the file's mode. Returns the backup path ("" when
// the file did not exist before).
func writeSettings(path string, before, after []byte, existed bool) (backup string, err error) {
	target := path
	perm := newSettingsPerm
	if existed {
		if t, err := filepath.EvalSymlinks(path); err == nil {
			target = t
		}
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		perm = info.Mode().Perm()
		backup, err = backupPath(target, time.Now())
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(backup, before, perm); err != nil {
			return "", fmt.Errorf("backup %s: %w", backup, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return backup, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".tmp-*")
	if err != nil {
		return backup, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(after); err != nil {
		_ = tmp.Close()
		return backup, err
	}
	if err := tmp.Close(); err != nil {
		return backup, err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmpName, target)
}

// backupPath returns an unused path.bak-<stamp>[-N] name.
func backupPath(path string, now time.Time) (string, error) {
	base := path + backupInfix + now.Format(backupTimeLayout)
	candidate := base
	for n := 1; ; n++ {
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		candidate = base + "-" + strconv.Itoa(n)
	}
}

// readSettings returns the file's bytes and whether it existed.
func readSettings(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
