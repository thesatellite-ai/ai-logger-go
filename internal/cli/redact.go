package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newRedactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "redact <id>",
		Short: "Replace an entry's prompt/response/notes with [redacted]",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			id, err := s.ResolveIDPrefix(ctx, args[0])
			if err != nil {
				return err
			}
			if err := s.Redact(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s redacted\n", shortID(id))
			return nil
		},
	}
}

func newPurgeCmd() *cobra.Command {
	var before string
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Hard-delete entries older than --before (destructive)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if before == "" {
				return fmt.Errorf("--before required (RFC3339 date or duration like 90d)")
			}
			cutoff, err := parseCutoff(before)
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintf(cmd.ErrOrStderr(), "refusing to purge without --yes (would delete entries before %s)\n", cutoff.Format(time.RFC3339))
				return nil
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			n, err := s.PurgeBefore(ctx, cutoff)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "purged %d entries\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&before, "before", "", "cutoff: RFC3339 date (2025-01-01) or duration (90d)")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm destructive action")
	return cmd
}

func parseCutoff(s string) (time.Time, error) {
	// Try duration form first.
	if t, err := parseSince(s); err == nil && !t.IsZero() {
		return t, nil
	}
	// RFC3339 / date.
	layouts := []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q as date or duration", s)
}
