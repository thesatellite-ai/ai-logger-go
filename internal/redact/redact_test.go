package redact_test

import (
	"strings"
	"testing"

	"github.com/khanakia/ai-logger/internal/redact"
)

func TestScrub_RedactsKnownSecrets(t *testing.T) {
	cases := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"ghp_1234567890abcdefghijklmnopqrstuvwxyz",
		"sk-1234567890abcdefghijklmnopqrstuvwxyz",
		"sk-ant-api03_verylongtokenwithmanychars_ABCDEF1234567890abcdef",
		"xoxb-1234-abcd-efgh-ijkl-mnop",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTYifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
	}
	for _, c := range cases {
		t.Run(c[:12], func(t *testing.T) {
			got := redact.Scrub("before " + c + " after")
			if strings.Contains(got, c) {
				t.Fatalf("secret leaked through scrubber: %q", got)
			}
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("no [redacted] marker: %q", got)
			}
		})
	}
}

func TestScrub_PreservesNormalText(t *testing.T) {
	in := "fix the race condition in worker.go around line 42"
	got := redact.Scrub(in)
	if got != in {
		t.Fatalf("scrubber mangled normal text: got %q want %q", got, in)
	}
}

func TestScrub_Empty(t *testing.T) {
	if redact.Scrub("") != "" {
		t.Fatal("empty input should return empty")
	}
}
