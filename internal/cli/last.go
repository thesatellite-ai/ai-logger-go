package cli

import (
	"strconv"

	"github.com/spf13/cobra"
)

func newLastCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "last [N]",
		Short: "Print the most recent N entries (default 1)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			n := 1
			if len(args) == 1 {
				v, err := strconv.Atoi(args[0])
				if err != nil || v <= 0 {
					return err
				}
				n = v
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			entries, err := s.Recent(ctx, n)
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
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
