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
