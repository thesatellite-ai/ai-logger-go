package cli

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/khanakia/ai-logger/internal/config"
	"github.com/spf13/cobra"
)

//go:embed skill_body.md
var skillBody string

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage the Claude Code skill",
	}
	cmd.AddCommand(newSkillInstallCmd(), newSkillPrintCmd())
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install SKILL.md into ~/.claude/skills/ailog/",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.ClaudeSkillsDir()
			if err != nil {
				return err
			}
			target := filepath.Join(dir, "ailog")
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			dst := filepath.Join(target, "SKILL.md")
			if _, err := os.Stat(dst); err == nil && !force {
				return fmt.Errorf("SKILL.md already exists at %s (use --force to overwrite)", dst)
			}
			if err := os.WriteFile(dst, []byte(skillBody), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed skill → %s\n", dst)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing SKILL.md")
	return cmd
}

func newSkillPrintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "print",
		Short: "Print the embedded SKILL.md to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), skillBody)
			return nil
		},
	}
}
