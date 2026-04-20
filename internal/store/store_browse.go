package store

import (
	"context"
	"fmt"

	"github.com/khanakia/ai-logger/ent"
	"github.com/khanakia/ai-logger/ent/entry"
)

// BrowseInput drives the /table page's server-side sort + filter +
// pagination.
//
// Sort is an ent field name from the whitelist below; empty means
// "default" (created_at DESC). Dir is "asc" or "desc" — anything else
// is treated as desc.
//
// Filters is a free-form map where keys are ent field names and
// values are the substring to Contains-match (for text) or exact match
// (for starred). Unknown keys are ignored — the allow-list is
// enforced by the handler's param parsing.
//
// Offset + Limit are straight SQL-style page cursoring; see
// BrowseCount for the matching total.
type BrowseInput struct {
	Sort    string
	Dir     string
	Filters map[string]string
	Limit   int
	Offset  int
}

// browseSortWhitelist is the set of ent fields safe to sort by. Any
// field outside this list is silently rejected so users can't craft a
// URL that makes ent build a bad query.
var browseSortWhitelist = map[string]string{
	"created_at":               entry.FieldCreatedAt,
	"tool":                     entry.FieldTool,
	"tool_version":             entry.FieldToolVersion,
	"model":                    entry.FieldModel,
	"project":                  entry.FieldProject,
	"repo_owner":               entry.FieldRepoOwner,
	"repo_name":                entry.FieldRepoName,
	"git_branch":               entry.FieldGitBranch,
	"git_commit":               entry.FieldGitCommit,
	"turn_index":               entry.FieldTurnIndex,
	"session_id":               entry.FieldSessionID,
	"token_count_in":           entry.FieldTokenCountIn,
	"token_count_out":          entry.FieldTokenCountOut,
	"token_count_cache_read":   entry.FieldTokenCountCacheRead,
	"token_count_cache_create": entry.FieldTokenCountCacheCreate,
	"stop_reason":              entry.FieldStopReason,
	"permission_mode":          entry.FieldPermissionMode,
	"starred":                  entry.FieldStarred,
	"pid":                      entry.FieldPid,
}

// Browse returns entries narrowed by Filters and ordered by Sort/Dir.
// Used by the /table page.
func (s *Store) Browse(ctx context.Context, in BrowseInput) ([]*Entry, error) {
	q := s.client.Entry.Query()

	// Apply filters — substring match for text columns, exact match
	// for the few enum-like ones.
	for key, val := range in.Filters {
		val = trimSpace(val)
		if val == "" {
			continue
		}
		switch key {
		case "tool":
			q = q.Where(entry.ToolContainsFold(val))
		case "tool_version":
			q = q.Where(entry.ToolVersionContainsFold(val))
		case "model":
			q = q.Where(entry.ModelContainsFold(val))
		case "project":
			q = q.Where(entry.ProjectContainsFold(val))
		case "git_branch":
			q = q.Where(entry.GitBranchContainsFold(val))
		case "git_commit":
			q = q.Where(entry.GitCommitContainsFold(val))
		case "session_id":
			q = q.Where(entry.SessionIDContainsFold(val))
		case "stop_reason":
			q = q.Where(entry.StopReasonContainsFold(val))
		case "permission_mode":
			q = q.Where(entry.PermissionModeContainsFold(val))
		case "hostname":
			q = q.Where(entry.HostnameContainsFold(val))
		case "user":
			q = q.Where(entry.UserContainsFold(val))
		case "tags":
			q = q.Where(entry.TagsContainsFold(val))
		case "starred":
			// "yes" / "true" / "1" → only starred
			if val == "yes" || val == "true" || val == "1" {
				q = q.Where(entry.StarredEQ(true))
			}
		default:
			// unknown filter key — ignore rather than error, lets the
			// UI send extras harmlessly
		}
	}

	// Apply sort — default to created_at desc.
	field, ok := browseSortWhitelist[in.Sort]
	if !ok {
		field = entry.FieldCreatedAt
	}
	if in.Dir == "asc" {
		q = q.Order(ent.Asc(field))
	} else {
		q = q.Order(ent.Desc(field))
	}

	if in.Limit <= 0 {
		in.Limit = 100
	}
	q = q.Limit(in.Limit)
	if in.Offset > 0 {
		q = q.Offset(in.Offset)
	}

	out, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	return out, nil
}

// BrowseCount returns the number of rows BrowseInput's filters would
// match WITHOUT the limit/offset/sort. Used by pagination to render
// "page N of M" without a second full scan.
//
// Filters apply identically to Browse() — same whitelist, same
// ContainsFold semantics for text columns.
func (s *Store) BrowseCount(ctx context.Context, in BrowseInput) (int, error) {
	q := s.client.Entry.Query()
	for key, val := range in.Filters {
		val = trimSpace(val)
		if val == "" {
			continue
		}
		switch key {
		case "tool":
			q = q.Where(entry.ToolContainsFold(val))
		case "tool_version":
			q = q.Where(entry.ToolVersionContainsFold(val))
		case "model":
			q = q.Where(entry.ModelContainsFold(val))
		case "project":
			q = q.Where(entry.ProjectContainsFold(val))
		case "git_branch":
			q = q.Where(entry.GitBranchContainsFold(val))
		case "git_commit":
			q = q.Where(entry.GitCommitContainsFold(val))
		case "session_id":
			q = q.Where(entry.SessionIDContainsFold(val))
		case "stop_reason":
			q = q.Where(entry.StopReasonContainsFold(val))
		case "permission_mode":
			q = q.Where(entry.PermissionModeContainsFold(val))
		case "hostname":
			q = q.Where(entry.HostnameContainsFold(val))
		case "user":
			q = q.Where(entry.UserContainsFold(val))
		case "tags":
			q = q.Where(entry.TagsContainsFold(val))
		case "starred":
			if val == "yes" || val == "true" || val == "1" {
				q = q.Where(entry.StarredEQ(true))
			}
		}
	}
	n, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("browse count: %w", err)
	}
	return n, nil
}

// trimSpace is a tiny helper kept local so store doesn't import strings
// just for this (tests already do).
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
