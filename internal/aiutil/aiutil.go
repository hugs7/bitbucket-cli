// Package aiutil wraps the user-configured AI command. The command is
// any shell pipeline that reads a unified diff on stdin and prints a
// generated description on stdout — bb is intentionally agnostic
// about which model or service backs it (local llm, ollama, curl to
// a remote API, …).
package aiutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// Resolve picks the AI command from (in order): the explicit override
// argument, $BB_AI_CMD, or the persisted ai_cmd config field.
// Returns "" when nothing is configured so callers can surface a
// friendly error.
func Resolve(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("BB_AI_CMD"); env != "" {
		return env
	}
	return config.Get().AICmd
}

// Run executes the given shell command via 'sh -c', piping the diff
// to its stdin and returning whatever it prints on stdout. The child's
// stderr is forwarded to the bb process's stderr so users see any
// progress / error output from the AI tool itself.
func Run(shellCmd, diff string) (string, error) {
	if strings.TrimSpace(shellCmd) == "" {
		return "", fmt.Errorf("no AI command configured (ai_cmd / $BB_AI_CMD)")
	}
	cmd := exec.Command("sh", "-c", shellCmd)
	cmd.Stdin = strings.NewReader(diff)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}
