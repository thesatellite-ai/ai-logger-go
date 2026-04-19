package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newStarCmd registers `ailog star <id>`. Starred entries are surfaced
// by `ailog templates` for reuse as prompt templates.
func newStarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "star <id>",
		Short: "Mark an entry as starred (reusable template)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setStar(cmd, args[0], true)
		},
	}
}

// newUnstarCmd is the inverse of newStarCmd.
func newUnstarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unstar <id>",
		Short: "Unstar an entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setStar(cmd, args[0], false)
		},
	}
}

// setStar is the shared helper for star/unstar — opens the store,
// resolves the id prefix, flips the bit.
func setStar(cmd *cobra.Command, prefix string, v bool) error {
	ctx := cmd.Context()
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	id, err := s.ResolveIDPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	if err := s.SetStarred(ctx, id, v); err != nil {
		return err
	}
	state := "unstarred"
	if v {
		state = "starred"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", shortID(id), state)
	return nil
}

// newTemplatesCmd lists every starred entry — the user's curated
// "prompt library".
func newTemplatesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List every starred entry (reusable prompt library)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			all, err := s.All(ctx)
			if err != nil {
				return err
			}
			// In-place filter — reuses backing array since we don't need `all` again.
			starred := all[:0]
			for _, e := range all {
				if e.Starred {
					starred = append(starred, e)
				}
			}
			if asJSON {
				return writeJSON(cmd, starred)
			}
			renderEntryList(cmd, starred)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
