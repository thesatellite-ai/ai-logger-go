package views

import "strconv"

// intStr renders a non-zero int. Empty when zero so the kv helper hides the row.
func intStr(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// intStrAlways renders any int, including 0 (turn 0 is the first turn).
func intStrAlways(n int) string {
	return strconv.Itoa(n)
}

// intPair renders an "in · out" tokens pair, hiding the row when both zero.
func intPair(a, b int) string {
	if a == 0 && b == 0 {
		return ""
	}
	return strconv.Itoa(a) + "  ·  " + strconv.Itoa(b)
}
