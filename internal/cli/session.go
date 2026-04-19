package cli

import (
	"fmt"
	"os"

	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Inspect or annotate the current session",
	}
	cmd.AddCommand(newSessionNameCmd(), newSessionShowCmd(), newSessionIDCmd())
	return cmd
}

func newSessionIDCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "id",
		Short: "Print the current session id (from AILOG_SESSION_ID or freshly generated)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := aictx.CollectSession()
			fmt.Fprintln(cmd.OutOrStdout(), s.ID)
			if s.WasFresh {
				fmt.Fprintln(cmd.ErrOrStderr(), "note: AILOG_SESSION_ID was unset; generated fresh")
			}
			return nil
		},
	}
}

func newSessionNameCmd() *cobra.Command {
	var sessionIDFlag string
	cmd := &cobra.Command{
		Use:   "name <label>",
		Short: "Rename every entry in a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sid := sessionIDFlag
			if sid == "" {
				sid = os.Getenv("AILOG_SESSION_ID")
			}
			if sid == "" {
				return fmt.Errorf("no session id: pass --session or export AILOG_SESSION_ID")
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			n, err := s.RenameSession(ctx, sid, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed %d entries in session %s → %q\n", n, shortID(sid), args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionIDFlag, "session", "", "explicit session id (default $AILOG_SESSION_ID)")
	return cmd
}

func newSessionShowCmd() *cobra.Command {
	var sessionIDFlag string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show [session-id]",
		Short: "List every entry in a session, in turn order",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sid := sessionIDFlag
			if len(args) == 1 {
				sid = args[0]
			}
			if sid == "" {
				sid = os.Getenv("AILOG_SESSION_ID")
			}
			if sid == "" {
				return fmt.Errorf("no session id: pass as arg, --session, or export AILOG_SESSION_ID")
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			entries, err := s.SessionEntries(ctx, sid)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd, entries)
			}
			renderEntryList(cmd, entries)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionIDFlag, "session", "", "session id")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
