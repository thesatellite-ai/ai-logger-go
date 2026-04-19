package views

import "strconv"

// intStr renders an int as a decimal string. Empty when zero so the kv
// helper hides the row.
func intStr(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// intPair renders an "in / out" tokens pair, hiding the row when both
// are zero.
func intPair(a, b int) string {
	if a == 0 && b == 0 {
		return ""
	}
	return strconv.Itoa(a) + " / " + strconv.Itoa(b)
}
