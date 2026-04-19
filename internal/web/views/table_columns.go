package views

// Package-level column registry for /table. Every column the datagrid
// exposes is declared here — a single source of truth consumed by the
// table header, the filter row, the cell renderer, and the column-chooser
// checkboxes. Add a column = add one entry here + one case in tableCell.
//
// Field semantics:
//
//	Key         stable id, appears in localStorage and DOM class names
//	Label       display label in header + chooser
//	SortField   ent field name to pass to Store.Browse; "" = not sortable
//	FilterKey   key passed to Store.Browse filters map; "" = not filterable
//	Default     true = visible on first visit (user can hide via chooser)
//	Align       "left" | "right" | "center"
//	Hint        what this column shows (used in chooser tooltips)
type Column struct {
	Key       string
	Label     string
	SortField string
	FilterKey string
	Default   bool
	Align     string
	Hint      string
}

// TableColumns is the ordered column list. Order matters — it's the
// left-to-right render order in the table.
//
// Column-chooser defaults (the ones with Default:true) were chosen to
// answer "when did I do what, in which project, how did it cost?".
// Everything else is one click away in the chooser.
var TableColumns = []Column{
	{Key: "time", Label: "Time", SortField: "created_at", Default: true, Hint: "When the entry was captured"},
	{Key: "id", Label: "ID", Hint: "Full UUID"},
	{Key: "tool", Label: "Tool", SortField: "tool", FilterKey: "tool", Default: true, Hint: "Which AI tool produced this turn"},
	{Key: "tool_version", Label: "Ver", SortField: "tool_version", Hint: "Tool version (e.g. Claude Code 2.1.114)"},
	{Key: "model", Label: "Model", SortField: "model", FilterKey: "model", Default: true, Hint: "Model id (opus-4-7 etc)"},
	{Key: "project", Label: "Project", SortField: "project", FilterKey: "project", Default: true, Hint: "host/owner/repo"},
	{Key: "repo_owner", Label: "Owner", SortField: "repo_owner", Hint: "Parsed from git remote"},
	{Key: "repo_name", Label: "Repo", SortField: "repo_name", Hint: "Parsed from git remote"},
	{Key: "repo_remote", Label: "Remote", Hint: "Raw git remote.origin.url"},
	{Key: "branch", Label: "Branch", SortField: "git_branch", FilterKey: "git_branch", Default: true, Hint: "git rev-parse --abbrev-ref HEAD"},
	{Key: "commit", Label: "Commit", SortField: "git_commit", Hint: "git rev-parse --short HEAD"},
	{Key: "cwd", Label: "CWD", Hint: "Working directory at capture time"},
	{Key: "session_id", Label: "Session", SortField: "session_id", FilterKey: "session_id", Hint: "Short id of the grouping session"},
	{Key: "session_name", Label: "Session name", Hint: "User-assigned label"},
	{Key: "turn", Label: "Turn", SortField: "turn_index", Default: true, Align: "right", Hint: "0-based index within the session"},
	{Key: "parent", Label: "Parent", Hint: "Previous turn id in the session chain"},
	{Key: "hostname", Label: "Host", FilterKey: "hostname", Hint: "os.Hostname()"},
	{Key: "user", Label: "User", FilterKey: "user", Hint: "$USER env"},
	{Key: "shell", Label: "Shell", Hint: "basename $SHELL"},
	{Key: "terminal", Label: "Term", Hint: "$TERM_PROGRAM"},
	{Key: "terminal_title", Label: "Term title", Hint: "Best-effort term title"},
	{Key: "tty", Label: "TTY", Hint: "Controlling tty"},
	{Key: "pid", Label: "PID", SortField: "pid", Align: "right", Hint: "Parent process id"},
	{Key: "prompt", Label: "Prompt", Default: true, Hint: "User's prompt, truncated"},
	{Key: "response", Label: "Response", Hint: "Assistant response, truncated"},
	{Key: "raw", Label: "Raw", Hint: "Provenance blob (transcript path / import hash)"},
	{Key: "tokens_in", Label: "Tok in", SortField: "token_count_in", Default: true, Align: "right", Hint: "Input tokens (Anthropic)"},
	{Key: "tokens_out", Label: "Tok out", SortField: "token_count_out", Default: true, Align: "right", Hint: "Output tokens"},
	{Key: "cache_read", Label: "Cache rd", SortField: "token_count_cache_read", Default: true, Align: "right", Hint: "Prompt cache HIT — cache_read_input_tokens"},
	{Key: "cache_create", Label: "Cache wr", SortField: "token_count_cache_create", Align: "right", Hint: "Prompt cache WRITE — cache_creation_input_tokens"},
	{Key: "stop_reason", Label: "Stop", SortField: "stop_reason", FilterKey: "stop_reason", Default: true, Hint: "end_turn / tool_use / max_tokens / stop_sequence"},
	{Key: "permission_mode", Label: "Perm", SortField: "permission_mode", FilterKey: "permission_mode", Default: true, Hint: "default / acceptEdits / bypassPermissions / plan"},
	{Key: "tags", Label: "Tags", FilterKey: "tags", Hint: "User-applied tags"},
	{Key: "starred", Label: "★", SortField: "starred", Align: "center", Default: true, Hint: "Starred / template"},
	{Key: "notes", Label: "Notes", Hint: "User annotation, truncated"},
}

// DefaultHiddenKeys returns the chooser keys that should start hidden,
// used when nothing is in localStorage yet.
func DefaultHiddenKeys() []string {
	var out []string
	for _, c := range TableColumns {
		if !c.Default {
			out = append(out, c.Key)
		}
	}
	return out
}

// shortModel drops "claude-" from model ids so "claude-opus-4-7" →
// "opus-4-7" — saves cell width without losing meaning.
func shortModel(m string) string {
	if len(m) > 7 && m[:7] == "claude-" {
		return m[7:]
	}
	return m
}

// compactInt renders 1234 → "1.2k", 12345 → "12k", 1234567 → "1.2M".
// Zero renders as empty so the table doesn't clutter with 0s.
func compactInt(n int) string {
	if n <= 0 {
		return ""
	}
	switch {
	case n < 1000:
		return itoa(n)
	case n < 10000:
		whole := n / 1000
		frac := (n % 1000) / 100
		if frac == 0 {
			return itoa(whole) + "k"
		}
		return itoa(whole) + "." + itoa(frac) + "k"
	case n < 1000000:
		return itoa(n/1000) + "k"
	default:
		whole := n / 1000000
		frac := (n % 1000000) / 100000
		if frac == 0 {
			return itoa(whole) + "M"
		}
		return itoa(whole) + "." + itoa(frac) + "M"
	}
}

// itoa is just strconv.Itoa — local alias to avoid polluting imports
// in the templ-generated file (which would make codegen noisier).
func itoa(n int) string {
	// inline to dodge the strconv import dance in the generated file
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
