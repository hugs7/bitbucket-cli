package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/hugs7/bitbucket-cli/internal/aiutil"
)

// newPRDescribeCmd implements `bb pr describe <id>` — pipes the PR's
// unified diff into the user-configured AI command (config.AICmd or
// $BB_AI_CMD) and prints the resulting description on stdout. With
// --apply, the suggestion is written back to the PR.
//
// The AI command itself is opaque to bb: it can be a local LLM
// wrapper (e.g. `llm -s "Write a PR description"`), an `ollama run`
// invocation, or a curl call to a remote API. We simply pipe the diff
// to its stdin and read its stdout.
func newPRDescribeCmd() *cobra.Command {
	var repoFlag, hostFlag, aiCmdFlag string
	var apply, yes bool
	c := &cobra.Command{
		Use:   "describe <id>",
		Short: "Generate a PR description from the diff using an AI command",
		Long: `Pipe the PR's unified diff into a configured AI command and print
its suggested description.

The AI command is read from (in order): the --ai-cmd flag, the
$BB_AI_CMD environment variable, or the ai_cmd field in your bb
config. It is invoked via 'sh -c' with the diff on stdin; whatever it
prints on stdout becomes the description.

Examples:
  bb pr describe 123
  bb pr describe 123 --apply
  BB_AI_CMD='llm -s "Write a concise PR description"' bb pr describe 123 --apply --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid PR id %q", args[0])
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			aiCmd := aiutil.Resolve(aiCmdFlag)
			if aiCmd == "" {
				return fmt.Errorf("no AI command configured — set ai_cmd in your bb config, $BB_AI_CMD, or pass --ai-cmd")
			}
			diff, err := svc.PRDiff(project, slug, id)
			if err != nil {
				return fmt.Errorf("fetch diff: %w", err)
			}
			if strings.TrimSpace(diff) == "" {
				return fmt.Errorf("PR #%d has an empty diff", id)
			}
			fmt.Fprintf(os.Stderr, "→ piping %d-byte diff to AI command…\n", len(diff))
			out, err := aiutil.Run(aiCmd, diff)
			if err != nil {
				return fmt.Errorf("AI command failed: %w", err)
			}
			out = strings.TrimSpace(out)
			if out == "" {
				return fmt.Errorf("AI command produced empty output")
			}
			fmt.Println(out)
			if !apply {
				return nil
			}
			if !yes {
				var confirm bool
				if err := huh.NewConfirm().
					Title(fmt.Sprintf("Apply this description to PR #%d?", id)).
					Value(&confirm).Run(); err != nil {
					return err
				}
				if !confirm {
					return nil
				}
			}
			if err := svc.UpdatePRDescription(project, slug, id, out); err != nil {
				return fmt.Errorf("update description: %w", err)
			}
			fmt.Fprintf(os.Stderr, "✓ Updated description for PR #%d\n", id)
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "host")
	c.Flags().StringVar(&aiCmdFlag, "ai-cmd", "", "shell command to pipe the diff into (overrides config and $BB_AI_CMD)")
	c.Flags().BoolVar(&apply, "apply", false, "update the PR description with the generated text")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt when --apply is set")
	return c
}


