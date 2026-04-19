package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Backfill from Claude Code transcript JSONL files (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if from == "" {
				h, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				from = filepath.Join(h, ".claude", "projects")
			}
			files, err := findJSONL(from)
			if err != nil {
				return err
			}
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()

			var imported, skipped int
			for _, f := range files {
				i, sk, err := importFile(cmd, s, f)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "skip %s: %v\n", f, err)
					continue
				}
				imported += i
				skipped += sk
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %d, skipped %d (already present)\n", imported, skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "directory to scan for *.jsonl (default ~/.claude/projects)")
	return cmd
}

func findJSONL(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(p) == ".jsonl" {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

type transcriptMsg struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Cwd       string          `json:"cwd"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type tMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
}

func importFile(cmd *cobra.Command, s *store.Store, path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	ctx := cmd.Context()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)

	var lastPromptID string
	var imported, skipped int

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		hash := hashLine(line)
		exists, err := s.RawHashExists(ctx, hash)
		if err != nil {
			return imported, skipped, err
		}
		if exists {
			skipped++
			continue
		}

		var m transcriptMsg
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		var inner tMessage
		if len(m.Message) > 0 {
			_ = json.Unmarshal(m.Message, &inner)
		}
		text := flattenContent(inner.Content)
		if text == "" {
			continue
		}

		switch m.Type {
		case "user":
			id, err := s.InsertEntry(ctx, store.InsertEntryInput{
				Tool:      "claude-code",
				CWD:       m.Cwd,
				SessionID: m.SessionID,
				Prompt:    text,
				Raw:       hash,
			})
			if err != nil {
				return imported, skipped, err
			}
			lastPromptID = id
			imported++
		case "assistant":
			if lastPromptID != "" {
				if err := s.AttachResponse(ctx, lastPromptID, text, inner.Model, 0); err != nil {
					return imported, skipped, err
				}
				lastPromptID = ""
			} else {
				_, err := s.InsertEntry(ctx, store.InsertEntryInput{
					Tool:      "claude-code",
					CWD:       m.Cwd,
					SessionID: m.SessionID,
					Response:  text,
					Model:     inner.Model,
					Raw:       hash,
				})
				if err != nil {
					return imported, skipped, err
				}
			}
			imported++
		}
	}
	return imported, skipped, scanner.Err()
}

func hashLine(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// flattenContent handles both string and array-of-blocks shapes.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// String form.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var out string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if out != "" {
					out += "\n"
				}
				out += b.Text
			}
		}
		return out
	}
	return ""
}
