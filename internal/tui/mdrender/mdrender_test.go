package mdrender

import (
	"strings"
	"testing"
)

func TestRenderProducesAnsi(t *testing.T) {
	// With WithAutoStyle this regressed — termenv couldn't detect
	// the terminal profile inside a child process and glamour fell
	// back to the no-color "ascii" style, so READMEs reached the
	// preview panes as literal `# headings`. Locking the style to
	// "dark" guarantees we always emit ANSI escapes; this test is
	// the regression guard.
	out := Render("# Hello\n\n**bold** body\n", 80)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escape sequences in styled markdown, got %q", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected 'Hello' content in output, got %q", out)
	}
}
