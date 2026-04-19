package cli

import (
	"testing"
	"time"
)

func TestParseSince_Durations(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"30m", 30 * time.Minute},
		{"7d", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1.5d", 36 * time.Hour}, // fractional days supported
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseSince(c.in)
			if err != nil {
				t.Fatal(err)
			}
			delta := time.Until(got) + c.want
			if delta < -2*time.Second || delta > 2*time.Second {
				t.Fatalf("cutoff off by %s", delta)
			}
		})
	}
}

func TestParseSince_Empty(t *testing.T) {
	got, err := parseSince("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Fatal("empty input should return zero time")
	}
}

func TestParseSince_Invalid(t *testing.T) {
	if _, err := parseSince("banana"); err == nil {
		t.Fatal("expected error on bad input")
	}
}

func TestParseCutoff_RFC3339(t *testing.T) {
	got, err := parseCutoff("2025-01-15T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got.Year() != 2025 || got.Month() != time.January || got.Day() != 15 {
		t.Fatalf("parse wrong: %v", got)
	}
}

func TestParseCutoff_DateOnly(t *testing.T) {
	got, err := parseCutoff("2024-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Year() != 2024 || got.Month() != time.June || got.Day() != 1 {
		t.Fatalf("parse wrong: %v", got)
	}
}

func TestParseCutoff_Duration(t *testing.T) {
	got, err := parseCutoff("30d")
	if err != nil {
		t.Fatal(err)
	}
	delta := time.Since(got)
	if delta < 29*24*time.Hour || delta > 31*24*time.Hour {
		t.Fatalf("expected ~30 days, got %s", delta)
	}
}

func TestParseCutoff_Garbage(t *testing.T) {
	if _, err := parseCutoff("not-a-date"); err == nil {
		t.Fatal("expected error on garbage")
	}
}

func TestShortID_13Chars(t *testing.T) {
	id := "019da607-dfac-71f7-b7a8-d05a3b4cad1f"
	if got := shortID(id); got != "019da607-dfac" {
		t.Fatalf("shortID: %q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatal("short ids pass through unchanged")
	}
}

func TestTruncate_CollapsesNewlines(t *testing.T) {
	in := "line one\nline two\nline three"
	got := truncate(in, 15)
	if got != "line one line …" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeTags_UniqueSorted(t *testing.T) {
	got := mergeTags("b, a", "c,a,d")
	if got != "a,b,c,d" {
		t.Fatalf("merge: %q", got)
	}
}
