package views

// pluralS returns "s" for n != 1 — for words like "turn"/"turns".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pluralES returns "es" for n != 1 — for words like "match"/"matches".
func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
