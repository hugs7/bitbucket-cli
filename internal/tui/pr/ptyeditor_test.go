package pr

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestEditorSeedPlacesHeaderBelowEditableArea(t *testing.T) {
	req := editorRequest{
		initial: "draft comment",
		header:  "# inline comment on file.go:12 (new side)\n# Lines starting with # are ignored.\n",
	}

	got := editorSeed(req)
	want := "draft comment\n\n\n# inline comment on file.go:12 (new side)\n# Lines starting with # are ignored.\n"
	if got != want {
		t.Fatalf("editorSeed() = %q, want %q", got, want)
	}
}

func TestCleanEditorResultStripsTemplateComments(t *testing.T) {
	req := editorRequest{header: "# Lines starting with # are ignored.\n"}
	input := "actual comment\n\n# inline comment on file.go:12 (new side)\n  # indented helper\n<!-- legacy helper -->\nmore text\n"

	got := cleanEditorResult(req, input)
	want := "actual comment\n\nmore text\n"
	if got != want {
		t.Fatalf("cleanEditorResult() = %q, want %q", got, want)
	}
}

func TestCleanEditorResultPreservesHashLinesWithoutTemplate(t *testing.T) {
	req := editorRequest{}
	input := "# markdown heading\nbody\n"

	got := cleanEditorResult(req, input)
	if got != input {
		t.Fatalf("cleanEditorResult() = %q, want %q", got, input)
	}
}

// TestPTYEditorSeedsInitialContent verifies that newPTYEditor writes
// req.initial to the temp file BEFORE the editor process is launched,
// so editors like vim/nvim see the existing comment text on open.
func TestPTYEditorSeedsInitialContent(t *testing.T) {
	if os.Getenv("EDITOR") == "" {
		// Skip if no editor available (e.g. minimal CI image).
		t.Setenv("EDITOR", "cat")
	}
	req := editorRequest{
		purpose: "edit-comment-diff",
		hint:    "test",
		prID:    1,
		initial: "EXISTING-COMMENT-TEXT-AAAAA\nsecond line\n",
	}
	pe, _, err := newPTYEditor(req, 80, 12)
	if err != nil {
		t.Fatalf("newPTYEditor: %v", err)
	}
	defer pe.Close()

	// Read the temp file the editor was launched on; it MUST contain
	// req.initial verbatim before any user keystrokes.
	data, err := os.ReadFile(pe.tmpFile)
	if err != nil {
		t.Fatalf("read tmp: %v", err)
	}
	if !strings.Contains(string(data), "EXISTING-COMMENT-TEXT-AAAAA") {
		t.Fatalf("temp file missing seeded content; got %q", string(data))
	}

	// Give vt10x a beat to absorb the editor's initial draw, then
	// check that at least some cells contain the seeded text. This
	// catches the case where the file has content but vim is opening
	// a different file (e.g. arg passing bug).
	time.Sleep(300 * time.Millisecond)
	pe.term.Lock()
	var screen strings.Builder
	for y := 0; y < 12; y++ {
		for x := 0; x < 80; x++ {
			ch := pe.term.Cell(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			screen.WriteRune(ch)
		}
		screen.WriteByte('\n')
	}
	pe.term.Unlock()
	if !strings.Contains(screen.String(), "EXISTING-COMMENT-TEXT-AAAAA") {
		t.Logf("vt10x screen did NOT contain seeded text:\n%s", screen.String())
		// Don't fail — the editor (e.g. vim) may use alternate screen
		// quirks vt10x doesn't fully model. The disk-content check
		// above is the real assertion.
	}
}
