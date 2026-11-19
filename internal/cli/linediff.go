package cli

import (
	"fmt"
	"io"
	"strings"
)

// diffContext is how many unchanged lines `--dry-run` shows around a
// change.
const diffContext = 3

// writeLineDiff prints a unified-style line diff of before → after
// (`-` removed, `+` added, ` ` context). Settings files are a few
// hundred lines, so the O(n·m) LCS table is cheap.
func writeLineDiff(w io.Writer, name string, before, after []byte) {
	a := splitLines(string(before))
	b := splitLines(string(after))
	ops := diffOps(a, b)
	fmt.Fprintf(w, "--- %s\n+++ %s (after)\n", name, name)
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-diffContext, 0)
		end := i
		// Extend the hunk while changes are within 2*context of each other.
		for j := i; j < len(ops); j++ {
			if ops[j].kind != ' ' {
				end = j
			} else if j-end > 2*diffContext {
				break
			}
		}
		stop := min(end+diffContext+1, len(ops))
		fmt.Fprintln(w, "@@")
		for _, op := range ops[start:stop] {
			fmt.Fprintf(w, "%c%s\n", op.kind, op.line)
		}
		i = stop
	}
}

type diffOp struct {
	kind byte // ' ', '-', '+'
	line string
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffOps returns the edit script from a to b via longest common
// subsequence.
func diffOps(a, b []string) []diffOp {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
