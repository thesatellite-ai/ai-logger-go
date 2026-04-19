package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print counts per tool / project / session",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			st, err := s.ComputeStats(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd, st)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "total entries:    %d\n", st.Total)
			fmt.Fprintf(out, "starred:          %d\n", st.Starred)
			fmt.Fprintf(out, "distinct sessions: %d\n", st.BySession)
			if st.FirstEntryAt != nil {
				fmt.Fprintf(out, "first entry:      %s\n", st.FirstEntryAt.Local().Format("2006-01-02 15:04"))
			}
			if st.LastEntryAt != nil {
				fmt.Fprintf(out, "last entry:       %s\n", st.LastEntryAt.Local().Format("2006-01-02 15:04"))
			}
			fmt.Fprintln(out, "\nby tool:")
			printMap(out, st.ByTool)
			fmt.Fprintln(out, "\nby project:")
			printMap(out, st.ByProject)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func printMap(out interface{ Write([]byte) (int, error) }, m map[string]int) {
	type kv struct {
		k string
		v int
	}
	ks := make([]kv, 0, len(m))
	for k, v := range m {
		ks = append(ks, kv{k, v})
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].v > ks[j].v })
	for _, p := range ks {
		fmt.Fprintf(out, "  %6d  %s\n", p.v, p.k)
	}
}
