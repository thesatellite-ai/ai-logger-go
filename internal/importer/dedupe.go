package importer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
)

// DedupeStats is what Dedupe found (dry run) or merged.
type DedupeStats struct {
	// Sessions is the number of sessions holding a repeated prompt.
	Sessions int
	// Merged counts imported rows folded into the live-hook row of the
	// same turn, per tool.
	Merged map[string]int
}

// Total is the number of merged rows over every tool.
func (d DedupeStats) Total() int {
	n := 0
	for _, v := range d.Merged {
		n += v
	}
	return n
}

// Dedupe finds imported rows that duplicate a prompt a live hook already
// stored, by the importer's own claim rule: same tool and session, same
// trimmed prompt text, the hook row's capture time within claimWindow of
// the imported row's transcript time, the hook row not linked to an
// import key, the nearest one taken and each hook row used once. Unless
// dryRun, each such imported row is merged into its hook row
// (store.MergeInto: the hook row keeps its values and gains the import's
// keys, answer and usage where it has none; the imported row is
// deleted). The caller makes the backup first.
func Dedupe(ctx context.Context, st *store.Store, dryRun bool) (DedupeStats, error) {
	out := DedupeStats{Merged: map[string]int{}}
	pairs, err := st.DuplicateSessions(ctx)
	if err != nil {
		return out, err
	}
	out.Sessions = len(pairs)
	for _, p := range pairs {
		tool, sid := p[0], p[1]
		rows, err := st.SessionRows(ctx, tool, sid)
		if err != nil {
			return out, err
		}
		used := map[string]bool{}
		for _, imp := range rows {
			if !imp.Linked || imp.Prompt == "" {
				continue
			}
			hook := nearestHook(rows, imp, used)
			if hook == "" {
				continue
			}
			used[hook] = true
			out.Merged[tool]++
			if dryRun {
				continue
			}
			if err := st.MergeInto(ctx, imp.ID, hook); err != nil {
				return out, fmt.Errorf("%s session %s: %w", tool, sid, err)
			}
		}
	}
	return out, nil
}

// nearestHook is the unlinked row with imp's prompt captured nearest to
// imp's transcript time within claimWindow, skipping rows already used;
// "" when none. Pure.
func nearestHook(rows []store.SessionRow, imp store.SessionRow, used map[string]bool) string {
	want := strings.TrimSpace(imp.Prompt)
	best, bestGap := "", time.Duration(0)
	for _, r := range rows {
		if r.Linked || used[r.ID] || r.ID == imp.ID || strings.TrimSpace(r.Prompt) != want {
			continue
		}
		gap := r.At().Sub(imp.At())
		if gap < 0 {
			gap = -gap
		}
		if gap <= claimWindow && (best == "" || gap < bestGap) {
			best, bestGap = r.ID, gap
		}
	}
	return best
}
