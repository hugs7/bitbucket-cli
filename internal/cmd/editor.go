package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// editText launches the user's editor on a temporary file pre-populated
// with `initial`, waits for them to save and exit, then returns the new
// content. The caller is responsible for trimming whitespace if desired.
//
// Editor is resolved from config.editor, then $VISUAL, $EDITOR, then nano.
func editText(initial, hint string) (string, error) {
	dir := os.TempDir()
	suffix := ".md"
	if hint != "" {
		suffix = "-" + hint + ".md"
	}
	f, err := os.CreateTemp(dir, "bb-edit-*"+suffix)
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	editor := config.Get().EditorCmd()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return "", fmt.Errorf("no editor configured")
	}
	args := append(parts[1:], tmp)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q exited with error: %w", filepath.Base(parts[0]), err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
