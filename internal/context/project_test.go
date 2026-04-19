package context_test

import (
	"testing"

	aictx "github.com/khanakia/ai-logger/internal/context"
)

func TestCanonicalRepo(t *testing.T) {
	cases := []struct {
		in                 string
		host, owner, repo  string
		wantCanonicalEmpty bool
	}{
		{"git@github.com:khanakia/ai-logger.git", "github.com", "khanakia", "ai-logger", false},
		{"git@github.com:khanakia/ai-logger", "github.com", "khanakia", "ai-logger", false},
		{"https://github.com/khanakia/ai-logger.git", "github.com", "khanakia", "ai-logger", false},
		{"https://github.com/khanakia/ai-logger", "github.com", "khanakia", "ai-logger", false},
		{"ssh://git@github.com/khanakia/ai-logger.git", "github.com", "khanakia", "ai-logger", false},
		{"https://gitlab.example.com/team/subgroup/proj.git", "gitlab.example.com", "team", "subgroup/proj", false},
		{"", "", "", "", true},
		{"not-a-url", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			h, o, r := aictx.CanonicalRepo(c.in)
			if h != c.host || o != c.owner || r != c.repo {
				t.Fatalf("got (%q,%q,%q) want (%q,%q,%q)", h, o, r, c.host, c.owner, c.repo)
			}
			got := aictx.CanonicalProject(c.in)
			if c.wantCanonicalEmpty != (got == "") {
				t.Fatalf("CanonicalProject(%q) = %q", c.in, got)
			}
		})
	}
}

func TestCanonicalProject_FormatsHostOwnerName(t *testing.T) {
	got := aictx.CanonicalProject("git@github.com:khanakia/ai-logger.git")
	want := "github.com/khanakia/ai-logger"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
