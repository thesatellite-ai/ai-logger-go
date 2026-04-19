package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// newTagCmd registers `ailog tag <id> <csv>`. New tags are merged into
// any existing tags on the entry; the result is the unique sorted union.
func newTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag <id> <tag1,tag2,...>",
		Short: "Add or replace tags on an entry (comma-separated)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			id, err := s.ResolveIDPrefix(ctx, args[0])
			if err != nil {
				return err
			}
			e, err := s.GetByID(ctx, id)
			if err != nil {
				return err
			}
			merged := mergeTags(e.Tags, args[1])
			if err := s.SetTags(ctx, id, merged); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s tags: %s\n", shortID(id), merged)
			return nil
		},
	}
}

// mergeTags returns the sorted, deduped union of two CSV tag strings.
// Empty pieces and surrounding whitespace are dropped.
func mergeTags(existing, incoming string) string {
	set := map[string]struct{}{}
	for _, t := range splitTags(existing) {
		set[t] = struct{}{}
	}
	for _, t := range splitTags(incoming) {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// splitTags parses a CSV tag string into trimmed, non-empty tokens.
func splitTags(csv string) []string {
	parts := strings.Split(csv, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
